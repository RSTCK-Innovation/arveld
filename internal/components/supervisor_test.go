package components

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSupervisorRunManagedRejectsOccupiedComponentAddress(t *testing.T) {
	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy managed component address: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close occupied managed component address: %v", err)
		}
	})

	componentStarted := make(chan struct{}, 1)
	supervisor := &Supervisor{
		checkAddresses: func(ctx context.Context) error {
			return checkManagedAddressesAvailable(ctx, listener.Addr().String())
		},
		ensure: func(context.Context, componentName) (string, error) {
			componentStarted <- struct{}{}
			return "component", nil
		},
		supervise: func(context.Context, processSpec) error {
			return nil
		},
		waitReady: func(context.Context, string) error {
			return nil
		},
	}
	ready := make(chan struct{})

	err = supervisor.RunManaged(t.Context(), ready)
	if err == nil || !strings.Contains(err.Error(), "managed component addresses") {
		t.Fatalf("RunManaged() error = %v, want occupied address error", err)
	}
	select {
	case <-componentStarted:
		t.Error("managed component started despite occupied address")
	default:
	}
	select {
	case <-ready:
		t.Error("managed components reported ready despite occupied address")
	default:
	}
}

func TestSupervisorRunManagedWaitsForEachComponentReadinessInOrder(t *testing.T) {
	const (
		alertmanagerReadinessEndpoint = "http://127.0.0.1:19093/-/ready"
		prometheusReadinessEndpoint   = "http://127.0.0.1:19090/-/ready"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	alertmanagerStarted := make(chan struct{})
	alertmanagerReady := make(chan struct{})
	prometheusStarted := make(chan struct{})
	prometheusReady := make(chan struct{})
	readinessEndpoints := make(chan string, 2)

	supervisor := &Supervisor{
		dataDirectory: t.TempDir(),
		ensure: func(_ context.Context, name componentName) (string, error) {
			return string(name), nil
		},
		supervise: func(ctx context.Context, spec processSpec) error {
			switch spec.executable {
			case string(alertmanagerComponent):
				close(alertmanagerStarted)
				<-ctx.Done()
				return nil
			case string(prometheusComponent):
				select {
				case <-alertmanagerReady:
					close(prometheusStarted)
				default:
					return errors.New("Prometheus started before Alertmanager was ready")
				}
				<-ctx.Done()
				return nil
			default:
				return errors.New("unexpected managed component")
			}
		},
		waitReady: func(ctx context.Context, endpoint string) error {
			readinessEndpoints <- endpoint
			switch endpoint {
			case alertmanagerReadinessEndpoint:
				select {
				case <-alertmanagerStarted:
				case <-ctx.Done():
					return ctx.Err()
				}
				close(alertmanagerReady)
				return nil
			case prometheusReadinessEndpoint:
				select {
				case <-prometheusStarted:
				case <-ctx.Done():
					return ctx.Err()
				}
				close(prometheusReady)
				cancel()
				return nil
			default:
				return errors.New("unexpected readiness endpoint")
			}
		},
	}

	if err := supervisor.RunManaged(ctx, make(chan struct{})); err != nil {
		t.Fatalf("run managed components: %v", err)
	}

	select {
	case <-prometheusStarted:
		// Prometheus started after Alertmanager became ready.
	default:
		t.Error("Prometheus did not start")
	}
	select {
	case <-prometheusReady:
		// Prometheus reported readiness before the managed run stopped.
	default:
		t.Error("Prometheus readiness was not checked")
	}

	var gotEndpoints []string
	for len(readinessEndpoints) > 0 {
		gotEndpoints = append(gotEndpoints, <-readinessEndpoints)
	}
	wantEndpoints := []string{
		alertmanagerReadinessEndpoint,
		prometheusReadinessEndpoint,
	}
	if !slices.Equal(gotEndpoints, wantEndpoints) {
		t.Errorf("readiness endpoints = %q, want %q", gotEndpoints, wantEndpoints)
	}
}

func TestSupervisorRunManagedSignalsWhenComponentsAreReady(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	alertmanagerStarted := make(chan struct{})
	prometheusStarted := make(chan struct{})
	supervisor := &Supervisor{
		dataDirectory: t.TempDir(),
		ensure: func(_ context.Context, name componentName) (string, error) {
			return string(name), nil
		},
		supervise: func(ctx context.Context, spec processSpec) error {
			switch spec.executable {
			case string(alertmanagerComponent):
				close(alertmanagerStarted)
			case string(prometheusComponent):
				close(prometheusStarted)
			default:
				return errors.New("unexpected managed component")
			}

			<-ctx.Done()
			return nil
		},
		waitReady: func(ctx context.Context, endpoint string) error {
			var started <-chan struct{}
			switch endpoint {
			case "http://127.0.0.1:19093/-/ready":
				started = alertmanagerStarted
			case "http://127.0.0.1:19090/-/ready":
				started = prometheusStarted
			default:
				return errors.New("unexpected readiness endpoint")
			}

			select {
			case <-started:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}

	ready := make(chan struct{})
	runErrors := make(chan error, 1)
	go func() {
		runErrors <- supervisor.RunManaged(ctx, ready)
	}()

	select {
	case <-ready:
		// Both managed components reported readiness.
	case err := <-runErrors:
		t.Fatalf("managed components stopped before readiness: %v", err)
	case <-time.After(time.Second):
		t.Fatal("managed components did not report readiness")
	}

	cancel()
	if err := <-runErrors; err != nil {
		t.Fatalf("run managed components: %v", err)
	}
}

func TestSupervisorRunManagedWaitsForProcessesDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	alertmanagerStarted := make(chan struct{})
	prometheusStarted := make(chan struct{})
	allowProcessesToStop := make(chan struct{})
	supervisor := &Supervisor{
		dataDirectory: t.TempDir(),
		ensure: func(_ context.Context, name componentName) (string, error) {
			return string(name), nil
		},
		supervise: func(ctx context.Context, spec processSpec) error {
			switch spec.component {
			case alertmanagerComponent:
				close(alertmanagerStarted)
			case prometheusComponent:
				close(prometheusStarted)
			default:
				return errors.New("unexpected managed component")
			}
			<-ctx.Done()
			<-allowProcessesToStop

			return nil
		},
		waitReady: func(ctx context.Context, endpoint string) error {
			var started <-chan struct{}
			switch endpoint {
			case ManagedAlertmanagerReadinessEndpoint:
				started = alertmanagerStarted
			case ManagedPrometheusReadinessEndpoint:
				started = prometheusStarted
			default:
				return errors.New("unexpected readiness endpoint")
			}
			select {
			case <-started:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	ready := make(chan struct{})
	runErrors := make(chan error, 1)
	go func() {
		runErrors <- supervisor.RunManaged(ctx, ready)
	}()

	select {
	case <-ready:
	case err := <-runErrors:
		t.Fatalf("managed components stopped before readiness: %v", err)
	case <-time.After(time.Second):
		t.Fatal("managed components did not become ready")
	}
	cancel()

	select {
	case err := <-runErrors:
		t.Fatalf("RunManaged() returned before child processes stopped: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(allowProcessesToStop)
	if err := <-runErrors; err != nil {
		t.Fatalf("RunManaged() error = %v, want nil", err)
	}
}

func TestSupervisorRunManagedWaitsForReadinessTasksDuringStartupShutdown(t *testing.T) {
	for _, component := range []componentName{alertmanagerComponent, prometheusComponent} {
		for _, failure := range []bool{false, true} {
			name := string(component) + "/cancellation"
			if failure {
				name = string(component) + "/process_failure"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					processFailure := errors.New("component failed during startup")
					failProcess := make(chan struct{})
					probeStarted := make(chan struct{})
					probeCanceled := make(chan struct{})
					allowProbeToStop := make(chan struct{})
					probeStopped := make(chan struct{})
					endpoint := ManagedAlertmanagerReadinessEndpoint
					if component == prometheusComponent {
						endpoint = ManagedPrometheusReadinessEndpoint
					}
					supervisor := &Supervisor{
						dataDirectory: t.TempDir(),
						ensure: func(_ context.Context, name componentName) (string, error) {
							return string(name), nil
						},
						supervise: func(ctx context.Context, spec processSpec) error {
							if spec.component == component {
								select {
								case <-failProcess:
									return processFailure
								case <-ctx.Done():
									return nil
								}
							}
							<-ctx.Done()
							return nil
						},
						waitReady: func(ctx context.Context, currentEndpoint string) error {
							if currentEndpoint != endpoint {
								return nil
							}
							close(probeStarted)
							<-ctx.Done()
							close(probeCanceled)
							<-allowProbeToStop
							close(probeStopped)
							return ctx.Err()
						},
					}
					ready := make(chan struct{})
					runErrors := make(chan error, 1)
					go func() {
						runErrors <- supervisor.RunManaged(ctx, ready)
					}()
					<-probeStarted
					synctest.Wait()
					if failure {
						close(failProcess)
					} else {
						cancel()
					}
					<-probeCanceled
					synctest.Wait()
					if len(runErrors) != 0 {
						t.Error("RunManaged returned before the readiness task stopped")
					}
					close(allowProbeToStop)
					err := <-runErrors
					if failure && !errors.Is(err, processFailure) {
						t.Errorf("RunManaged() error = %v, want process failure", err)
					}
					<-probeStopped
					select {
					case <-ready:
						t.Error("RunManaged signaled readiness during incomplete startup")
					default:
					}
				})
			})
		}
	}
}

func TestSupervisorRunManagedReportsAlertmanagerFailureAfterReadiness(t *testing.T) {
	alertmanagerFailure := errors.New("alertmanager crashed")
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	alertmanagerStarted := make(chan struct{})
	prometheusStarted := make(chan struct{})
	componentsReady := make(chan struct{})

	supervisor := &Supervisor{
		dataDirectory: t.TempDir(),
		ensure: func(_ context.Context, name componentName) (string, error) {
			return string(name), nil
		},
		supervise: func(ctx context.Context, spec processSpec) error {
			switch spec.executable {
			case string(alertmanagerComponent):
				close(alertmanagerStarted)
				<-componentsReady
				return alertmanagerFailure
			case string(prometheusComponent):
				close(prometheusStarted)
				<-ctx.Done()
				return nil
			default:
				return errors.New("unexpected managed component")
			}
		},
		waitReady: func(ctx context.Context, endpoint string) error {
			switch endpoint {
			case "http://127.0.0.1:19093/-/ready":
				select {
				case <-alertmanagerStarted:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			case "http://127.0.0.1:19090/-/ready":
				select {
				case <-prometheusStarted:
					close(componentsReady)
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			default:
				return errors.New("unexpected readiness endpoint")
			}
		},
	}

	err := supervisor.RunManaged(ctx, make(chan struct{}))
	if !errors.Is(err, alertmanagerFailure) {
		t.Errorf("run managed components error = %v, want Alertmanager failure", err)
	}
}

func TestSupervisorRunUsesInstalledExecutable(t *testing.T) {
	const (
		executable = "/data/components/prometheus/prometheus"
		argument   = "--web.listen-address=127.0.0.1:19090"
	)

	var ensuredComponent componentName
	var supervisedSpec processSpec
	supervisor := &Supervisor{
		ensure: func(_ context.Context, name componentName) (string, error) {
			ensuredComponent = name
			return executable, nil
		},
		supervise: func(_ context.Context, spec processSpec) error {
			supervisedSpec = spec
			return nil
		},
	}

	if err := supervisor.runComponent(context.Background(), prometheusComponent, argument); err != nil {
		t.Fatalf("run supervisor: %v", err)
	}

	if ensuredComponent != prometheusComponent {
		t.Errorf("ensured component = %q, want %q", ensuredComponent, prometheusComponent)
	}
	if supervisedSpec.executable != executable {
		t.Errorf("supervised executable = %q, want %q", supervisedSpec.executable, executable)
	}
	if len(supervisedSpec.arguments) != 1 || supervisedSpec.arguments[0] != argument {
		t.Errorf("supervised arguments = %q, want [%q]", supervisedSpec.arguments, argument)
	}
}

func TestNewSupervisorUsesInstaller(t *testing.T) {
	release, err := releaseForCurrentPlatform(prometheusComponent)
	if err != nil {
		t.Fatalf("resolve Prometheus release: %v", err)
	}
	dataDirectory := t.TempDir()
	_, installedPath, err := prepareComponentDirectory(
		dataDirectory,
		prometheusComponent,
		release,
	)
	if err != nil {
		t.Fatalf("prepare component directory: %v", err)
	}
	executable := []byte("prometheus executable")
	if err := os.WriteFile( //nolint:gosec // test fixture must represent a runnable executable
		installedPath,
		executable,
		0o755,
	); err != nil {
		t.Fatalf("write installed executable: %v", err)
	}
	checksum := sha256.Sum256(executable)
	if err := os.WriteFile(
		installedPath+executableChecksumSuffix,
		[]byte(hex.EncodeToString(checksum[:])),
		0o600,
	); err != nil {
		t.Fatalf("write installed executable checksum: %v", err)
	}

	supervisor := NewSupervisor(
		SupervisorConfig{
			DataDirectory:   dataDirectory,
			DownloadTimeout: 2 * time.Minute,
			Prometheus:      PrometheusConfig{RetentionTime: "15d"},
		},
		discardLogger(),
	)
	var supervisedSpec processSpec
	supervisor.supervise = func(_ context.Context, spec processSpec) error {
		supervisedSpec = spec
		return nil
	}

	if err := supervisor.runComponent(context.Background(), prometheusComponent); err != nil {
		t.Fatalf("run supervisor: %v", err)
	}

	if supervisedSpec.executable != installedPath {
		t.Errorf("supervised executable = %q, want %q", supervisedSpec.executable, installedPath)
	}
}

func TestSupervisorRunPrometheusUsesManagedPaths(t *testing.T) {
	const executable = "/components/prometheus/prometheus"

	dataDirectory := t.TempDir()
	supervisor := NewSupervisor(
		SupervisorConfig{
			DataDirectory:   dataDirectory,
			DownloadTimeout: 2 * time.Minute,
			Prometheus: PrometheusConfig{
				RetentionTime: "30d",
				RetentionSize: "8GB",
			},
		},
		discardLogger(),
	)
	var ensuredComponent componentName
	var supervisedSpec processSpec
	supervisor.ensure = func(_ context.Context, name componentName) (string, error) {
		ensuredComponent = name
		return executable, nil
	}
	supervisor.supervise = func(_ context.Context, spec processSpec) error {
		supervisedSpec = spec
		return nil
	}

	if err := supervisor.runPrometheus(context.Background()); err != nil {
		t.Fatalf("run Prometheus: %v", err)
	}

	if ensuredComponent != prometheusComponent {
		t.Errorf("ensured component = %q, want %q", ensuredComponent, prometheusComponent)
	}
	if supervisedSpec.executable != executable {
		t.Errorf("supervised executable = %q, want %q", supervisedSpec.executable, executable)
	}
	expectedArguments := []string{
		"--config.file=" + filepath.Join(dataDirectory, "config", "prometheus.yml"),
		"--storage.tsdb.path=" + filepath.Join(dataDirectory, "prometheus"),
		"--web.listen-address=127.0.0.1:19090",
		"--web.enable-otlp-receiver",
		"--web.enable-lifecycle",
		"--query.lookback-delta=2h1m",
		"--log.format=json",
	}
	if !slices.Equal(supervisedSpec.arguments, expectedArguments) {
		t.Errorf(
			"supervised arguments = %q, want %q",
			supervisedSpec.arguments,
			expectedArguments,
		)
	}

	content, err := os.ReadFile(filepath.Join(dataDirectory, "config", "prometheus.yml")) //nolint:gosec // fixed path inside the test directory
	if err != nil {
		t.Fatalf("read Prometheus config: %v", err)
	}
	if !strings.Contains(string(content), "time: 30d\n      size: 8GB") {
		t.Errorf("Prometheus config = %q, want configured retention limits", content)
	}
}

func TestSupervisorRunAlertmanagerUsesManagedPaths(t *testing.T) {
	const executable = "/components/alertmanager/alertmanager"

	dataDirectory := t.TempDir()
	supervisor := NewSupervisor(
		SupervisorConfig{
			DataDirectory:   dataDirectory,
			DownloadTimeout: 2 * time.Minute,
			Prometheus:      PrometheusConfig{RetentionTime: "15d"},
		},
		discardLogger(),
	)
	var ensuredComponent componentName
	var supervisedSpec processSpec
	supervisor.ensure = func(_ context.Context, name componentName) (string, error) {
		ensuredComponent = name
		return executable, nil
	}
	supervisor.supervise = func(_ context.Context, spec processSpec) error {
		supervisedSpec = spec
		return nil
	}

	if err := supervisor.runAlertmanager(context.Background()); err != nil {
		t.Fatalf("run Alertmanager: %v", err)
	}

	if ensuredComponent != alertmanagerComponent {
		t.Errorf("ensured component = %q, want %q", ensuredComponent, alertmanagerComponent)
	}
	if supervisedSpec.executable != executable {
		t.Errorf("supervised executable = %q, want %q", supervisedSpec.executable, executable)
	}
	expectedArguments := []string{
		"--config.file=" + filepath.Join(dataDirectory, "config", "alertmanager.yml"),
		"--storage.path=" + filepath.Join(dataDirectory, "alertmanager"),
		"--web.listen-address=127.0.0.1:19093",
		"--cluster.listen-address=",
		"--log.format=json",
	}
	if !slices.Equal(supervisedSpec.arguments, expectedArguments) {
		t.Errorf(
			"supervised arguments = %q, want %q",
			supervisedSpec.arguments,
			expectedArguments,
		)
	}
}
