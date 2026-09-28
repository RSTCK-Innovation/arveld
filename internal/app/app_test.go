package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRejectsEmptyHTTPAddress(t *testing.T) {
	err := Run(context.Background(), Config{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want a configuration error")
	}

	if !strings.Contains(err.Error(), "HTTP address") {
		t.Errorf("Run() error = %q, want it to mention the HTTP address", err)
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := Config{
		PrometheusQueryTimeout:   30 * time.Second,
		OTLPMetricsTimeout:       30 * time.Second,
		ComponentDownloadTimeout: 2 * time.Minute,
		HTTPAddress:              "127.0.0.1:0",
		DatabasePath:             filepath.Join(t.TempDir(), "arveld.db"),
		PrometheusRetentionTime:  "15d",
	}

	runErrors := make(chan error, 1)
	go func() {
		runErrors <- Run(
			ctx,
			config,
			testLogger(),
		)
	}()

	select {
	case err := <-runErrors:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after its context was canceled")
	}
}

func TestRunKeepsHTTPServerRunningWhenManagedComponentsFail(t *testing.T) {
	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve HTTP address: %v", err)
	}
	httpAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release HTTP address: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	databasePath := filepath.Join(t.TempDir(), "arveld.db")
	managedFailed := make(chan struct{})
	var managedFailedOnce sync.Once
	runErrors := make(chan error, 1)
	go func() {
		runErrors <- RunWithComponents(
			ctx,
			Config{
				PrometheusQueryTimeout:   30 * time.Second,
				OTLPMetricsTimeout:       30 * time.Second,
				ComponentDownloadTimeout: 2 * time.Minute,
				HTTPAddress:              httpAddress,
				DatabasePath:             databasePath,
				PrometheusRetentionTime:  "15d",
			},
			testLogger(),
			ComponentRuntime{
				PrometheusURL:   "http://127.0.0.1:1",
				AlertmanagerURL: "http://127.0.0.1:1",
				Run: func(context.Context, chan<- struct{}) error {
					managedFailedOnce.Do(func() {
						close(managedFailed)
					})
					return errors.New("managed components failed")
				},
			},
		)
	}()

	select {
	case <-managedFailed:
	case <-time.After(time.Second):
		t.Fatal("managed components did not start")
	}
	select {
	case err := <-runErrors:
		t.Fatalf("Run() stopped after managed components failure: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	response := getResponse(t, "http://"+httpAddress+"/healthz", nil)
	if response.StatusCode != http.StatusOK {
		t.Errorf("health status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close health response: %v", err)
	}

	cancel()
	select {
	case err := <-runErrors:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after its context was canceled")
	}
}

func TestRunDoesNotStartComponentsWhenHTTPListenFails(t *testing.T) {
	runnerCalled := false
	config := Config{
		PrometheusQueryTimeout:   30 * time.Second,
		OTLPMetricsTimeout:       30 * time.Second,
		ComponentDownloadTimeout: 2 * time.Minute,
		HTTPAddress:              "invalid HTTP address",
		DatabasePath:             filepath.Join(t.TempDir(), "arveld.db"),
		PrometheusRetentionTime:  "15d",
	}

	err := RunWithComponents(
		t.Context(),
		config,
		testLogger(),
		ComponentRuntime{
			PrometheusURL:   "http://127.0.0.1:1",
			AlertmanagerURL: "http://127.0.0.1:1",
			Run: func(context.Context, chan<- struct{}) error {
				runnerCalled = true
				return errors.New("components started before HTTP listener")
			},
		},
	)
	if runnerCalled {
		t.Error("components started despite an HTTP listener failure")
	}
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Errorf("Run() error = %v, want HTTP listener error", err)
	}
}

func TestManagedReadinessRequiresLocalSupervisorReadiness(t *testing.T) {
	managedState := &managedComponentsState{}
	check := newReadinessChecker(
		ComponentRuntime{},
		managedState,
	)

	got := check(t.Context())

	if got.Prometheus {
		t.Error("Prometheus readiness = true before local Supervisor readiness")
	}
	if got.Alertmanager {
		t.Error("Alertmanager readiness = true before local Supervisor readiness")
	}
}

func TestStartManagedComponentsStopWaitsForRunner(t *testing.T) {
	runnerStarted := make(chan struct{})
	allowRunnerToStop := make(chan struct{})
	stop := startManagedComponents(
		t.Context(),
		testLogger(),
		func(ctx context.Context, _ chan<- struct{}) error {
			close(runnerStarted)
			<-ctx.Done()
			<-allowRunnerToStop

			return nil
		},
		&managedComponentsState{},
	)
	<-runnerStarted
	stopReturned := make(chan struct{})
	go func() {
		stop()
		close(stopReturned)
	}()

	returnedBeforeRunner := false
	select {
	case <-stopReturned:
		returnedBeforeRunner = true
	case <-time.After(50 * time.Millisecond):
	}
	close(allowRunnerToStop)
	if returnedBeforeRunner {
		t.Fatal("managed stop returned before runner stopped")
	}
	select {
	case <-stopReturned:
	case <-time.After(time.Second):
		t.Fatal("managed stop did not return after runner stopped")
	}
}

func TestStartManagedComponentsRetriesFailuresWithBackoff(t *testing.T) {
	attempts := make(chan struct{}, 3)
	stop := startManagedComponents(
		t.Context(),
		testLogger(),
		func(context.Context, chan<- struct{}) error {
			attempts <- struct{}{}

			return errors.New("managed startup failed")
		},
		&managedComponentsState{},
	)
	defer stop()

	for attempt := 1; attempt <= 2; attempt++ {
		select {
		case <-attempts:
		case <-time.After(time.Second):
			t.Fatalf("managed startup attempt %d did not happen", attempt)
		}
	}
	select {
	case <-attempts:
		t.Fatal("managed startup retried without the expected backoff")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReadinessReportsComponentProbeResults(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/-/ready" {
				t.Errorf("Prometheus readiness path = %q, want %q", request.URL.Path, "/-/ready")
			}
			response.WriteHeader(http.StatusServiceUnavailable)
		},
	))
	t.Cleanup(prometheus.Close)

	alertmanager := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/-/ready" {
				t.Errorf("Alertmanager readiness path = %q, want %q", request.URL.Path, "/-/ready")
			}
			response.WriteHeader(http.StatusOK)
		},
	))
	t.Cleanup(alertmanager.Close)

	state := &managedComponentsState{}
	state.markReady()
	check := newReadinessChecker(ComponentRuntime{
		PrometheusURL:   prometheus.URL,
		AlertmanagerURL: alertmanager.URL,
	}, state)
	got := check(t.Context())

	if got.Prometheus {
		t.Error("Prometheus readiness = true for a 503 response, want false")
	}
	if !got.Alertmanager {
		t.Error("Alertmanager readiness = false for a 200 response, want true")
	}
}

func TestReadinessChecksComponentEndpointsConcurrently(t *testing.T) {
	var requestsStarted atomic.Int32
	bothStarted := make(chan struct{})
	handler := http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if requestsStarted.Add(1) == 2 {
				close(bothStarted)
			}

			select {
			case <-bothStarted:
				response.WriteHeader(http.StatusOK)
			case <-request.Context().Done():
			}
		},
	)
	prometheus := httptest.NewServer(handler)
	t.Cleanup(prometheus.Close)
	alertmanager := httptest.NewServer(handler)
	t.Cleanup(alertmanager.Close)

	state := &managedComponentsState{}
	state.markReady()
	check := newReadinessChecker(ComponentRuntime{
		PrometheusURL:   prometheus.URL,
		AlertmanagerURL: alertmanager.URL,
	}, state)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	got := check(ctx)

	if !got.Prometheus || !got.Alertmanager {
		t.Errorf("readiness = %+v, want both components ready", got)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func getResponse(t *testing.T, url string, cookie *http.Cookie) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		t.Fatalf("create GET request: %v", err)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform GET request: %v", err)
	}

	return response
}
