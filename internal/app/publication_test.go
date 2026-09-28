package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunWaitsForManagedReadinessBeforePublishingConfiguration(t *testing.T) {
	requests := make(chan string, 16)
	newEngine := func(name string) *httptest.Server {
		engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			select {
			case requests <- name:
			default:
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(engine.Close)
		return engine
	}
	prometheus := newEngine("Prometheus")
	alertmanager := newEngine("Alertmanager")
	directory := t.TempDir()
	ruleDirectory := filepath.Join(directory, "config", "alert-rules")
	if err := os.MkdirAll(ruleDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	// An existing rule file makes Prometheus publication contact the engine even
	// with no saved rules, as it does when the controller restarts after deletion.
	if err := os.WriteFile(filepath.Join(ruleDirectory, "monitors.yml"), []byte("groups: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	managedStarted := make(chan struct{})
	allowReady := make(chan struct{})
	runErrors := make(chan error, 1)
	go func() {
		runErrors <- RunWithComponents(ctx, Config{
			PrometheusQueryTimeout:   30 * time.Second,
			OTLPMetricsTimeout:       30 * time.Second,
			ComponentDownloadTimeout: 2 * time.Minute,
			HTTPAddress:              "127.0.0.1:0",
			DatabasePath:             filepath.Join(directory, "arveld.db"),
			PrometheusRetentionTime:  "15d",
		}, testLogger(), ComponentRuntime{
			PrometheusURL:   prometheus.URL,
			AlertmanagerURL: alertmanager.URL,
			Run: func(ctx context.Context, ready chan<- struct{}) error {
				close(managedStarted)
				select {
				case <-allowReady:
					close(ready)
				case <-ctx.Done():
					return nil
				}
				<-ctx.Done()
				return nil
			},
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-runErrors:
			if err != nil {
				t.Errorf("RunWithComponents() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("controller did not stop after cancellation")
		}
	})

	select {
	case <-managedStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("managed components did not start")
	}
	if _, err := os.Stat(filepath.Join(directory, "config", "alertmanager.yml")); err != nil {
		t.Fatalf("notification configuration was not prepared before component startup: %v", err)
	}
	select {
	case engine := <-requests:
		t.Fatalf("%s publication contacted the engine before managed readiness", engine)
	case <-time.After(5500 * time.Millisecond):
	}

	close(allowReady)
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	seen := make(map[string]bool)
	for len(seen) < 2 {
		select {
		case engine := <-requests:
			seen[engine] = true
		case <-deadline.C:
			t.Fatalf("publication did not resume after managed readiness; contacted engines: %v", seen)
		}
	}
}
