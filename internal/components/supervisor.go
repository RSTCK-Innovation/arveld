package components

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

type componentEnsurer func(context.Context, componentName) (string, error)

type processSupervisor func(context.Context, processSpec) error

type readinessWaiter func(context.Context, string) error

type managedAddressChecker func(context.Context) error

// SupervisorConfig configures installation and storage of managed components.
type SupervisorConfig struct {
	DataDirectory   string
	DownloadTimeout time.Duration
	Prometheus      PrometheusConfig
}

// Supervisor installs and runs managed upstream components.
type Supervisor struct {
	dataDirectory    string
	prometheusConfig PrometheusConfig
	ensure           componentEnsurer
	supervise        processSupervisor
	waitReady        readinessWaiter
	checkAddresses   managedAddressChecker
}

// NewSupervisor assembles the installer, readiness probe and process supervision.
// config.DownloadTimeout must be positive and bounds each archive transfer.
func NewSupervisor(
	config SupervisorConfig,
	logger *slog.Logger,
) *Supervisor {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	installer := newInstaller(config.DataDirectory, nil, logger, config.DownloadTimeout)
	probe := NewReadinessProbe()
	return &Supervisor{
		dataDirectory:    config.DataDirectory,
		prometheusConfig: config.Prometheus,
		ensure:           installer.ensure,
		supervise: func(ctx context.Context, spec processSpec) error {
			return superviseProcess(ctx, spec, runProcessOnce, waitForRestart, logger)
		},
		waitReady: probe.waitReady,
		checkAddresses: func(ctx context.Context) error {
			return checkManagedAddressesAvailable(
				ctx,
				alertmanagerListenAddress,
				prometheusListenAddress,
			)
		},
	}
}

// RunManaged supervises Alertmanager and starts Prometheus once Alertmanager is ready.
// The caller must prepare the Alertmanager configuration before this call.
// It closes ready once both components are initially ready; ready must be non-nil
// and owned by this call. The signal does not track later readiness changes.
// Before returning, RunManaged cancels and waits for all processes and readiness tasks.
func (supervisor *Supervisor) RunManaged(
	ctx context.Context,
	ready chan<- struct{},
) error {
	if supervisor.checkAddresses != nil {
		if err := supervisor.checkAddresses(ctx); err != nil {
			return fmt.Errorf("check managed component addresses: %w", err)
		}
	}

	managedCtx, cancel := context.WithCancel(ctx)
	var managedTasks sync.WaitGroup
	defer func() {
		cancel()
		managedTasks.Wait()
	}()

	alertmanagerErrors := make(chan error, 1)
	managedTasks.Go(func() {
		alertmanagerErrors <- supervisor.runAlertmanager(managedCtx)
	})

	alertmanagerReadiness := make(chan error, 1)
	managedTasks.Go(func() {
		alertmanagerReadiness <- supervisor.waitReady(
			managedCtx,
			ManagedAlertmanagerReadinessEndpoint,
		)
	})

	select {
	case err := <-alertmanagerErrors:
		if err != nil {
			return fmt.Errorf("run Alertmanager while starting managed components: %w", err)
		}

		return errors.New("alertmanager stopped before becoming ready")
	case err := <-alertmanagerReadiness:
		if err != nil {
			return fmt.Errorf("wait for managed Alertmanager readiness: %w", err)
		}
	}

	prometheusErrors := make(chan error, 1)
	managedTasks.Go(func() {
		prometheusErrors <- supervisor.runPrometheus(managedCtx)
	})

	prometheusReadiness := make(chan error, 1)
	managedTasks.Go(func() {
		prometheusReadiness <- supervisor.waitReady(
			managedCtx,
			ManagedPrometheusReadinessEndpoint,
		)
	})

	select {
	case err := <-alertmanagerErrors:
		return alertmanagerExitAfterReadinessError(managedCtx, err)
	case err := <-prometheusErrors:
		if managedCtx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("run Prometheus before readiness: %w", err)
		}

		return errors.New("prometheus stopped before becoming ready")
	case err := <-prometheusReadiness:
		if err != nil {
			if managedCtx.Err() != nil {
				return nil
			}

			return fmt.Errorf("wait for managed Prometheus readiness: %w", err)
		}
	}
	close(ready)

	select {
	case <-managedCtx.Done():
		return nil
	case err := <-alertmanagerErrors:
		return alertmanagerExitAfterReadinessError(managedCtx, err)
	case err := <-prometheusErrors:
		if err != nil {
			return fmt.Errorf("run Prometheus after readiness: %w", err)
		}

		return nil
	}
}

func checkManagedAddressesAvailable(
	ctx context.Context,
	addresses ...string,
) error {
	listeners := make([]net.Listener, 0, len(addresses))
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close() //nolint:errcheck // listeners only reserve addresses during this check
		}
	}()

	listenerConfig := net.ListenConfig{}
	for _, address := range addresses {
		listener, err := listenerConfig.Listen(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("managed address %q is unavailable: %w", address, err)
		}
		listeners = append(listeners, listener)
	}

	return nil
}

func alertmanagerExitAfterReadinessError(
	ctx context.Context,
	runErr error,
) error {
	if ctx.Err() != nil {
		return nil //nolint:nilerr // child termination is expected during shutdown
	}
	if runErr != nil {
		return fmt.Errorf("run Alertmanager after readiness: %w", runErr)
	}

	return errors.New("alertmanager stopped after readiness")
}

// runComponent ensures that name is installed, then supervises its process.
func (supervisor *Supervisor) runComponent(
	ctx context.Context,
	name componentName,
	arguments ...string,
) error {
	executable, err := supervisor.ensure(ctx, name)
	if err != nil {
		return fmt.Errorf("ensure component %q: %w", name, err)
	}

	spec := processSpec{
		component:  name,
		executable: executable,
		arguments:  arguments,
	}
	if err := supervisor.supervise(ctx, spec); err != nil {
		return fmt.Errorf("supervise component %q: %w", name, err)
	}

	return nil
}
