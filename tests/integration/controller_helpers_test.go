package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/app"
)

// Observe the bound address in the controller's startup log instead of reserving
// and releasing a port before Run binds it. All startup work still belongs to Run.
type controllerLogHandler struct {
	slog.Handler
	started chan string
}

func (*controllerLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (handler *controllerLogHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "controller started" {
		record.Attrs(func(attribute slog.Attr) bool {
			if attribute.Key == "http_address" {
				handler.started <- attribute.Value.String()
			}
			return true
		})
	}
	return nil
}

// controllerTestConfig keeps test process addresses out of the product configuration.
type controllerTestConfig struct {
	app.Config
	prometheusURL   string
	alertmanagerURL string
}

func controllerConfig(t *testing.T, prometheusURL string) controllerTestConfig {
	t.Helper()
	config := app.DefaultConfig()
	config.HTTPAddress = "127.0.0.1:0"
	config.DatabasePath = filepath.Join(t.TempDir(), "arveld.db")
	return controllerTestConfig{Config: config, prometheusURL: prometheusURL, alertmanagerURL: "http://127.0.0.1:1"}
}

func startController(t *testing.T, config controllerTestConfig) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	handler := &controllerLogHandler{Handler: slog.DiscardHandler, started: make(chan string, 1)}
	finished := make(chan struct{})
	var runErr error
	go func() {
		defer close(finished)
		runErr = app.RunWithComponents(ctx, config.Config, slog.New(handler), app.ComponentRuntime{
			PrometheusURL:   config.prometheusURL,
			AlertmanagerURL: config.alertmanagerURL,
			Run: func(ctx context.Context, ready chan<- struct{}) error {
				// Tests own their HTTP servers; retain the controller's readiness/stop lifecycle.
				close(ready)
				<-ctx.Done()
				return nil
			},
		})
	}()
	var stopOnce sync.Once
	stop := func() {
		t.Helper()
		stopOnce.Do(func() {
			cancel()
			select {
			case <-finished:
				if runErr != nil {
					t.Errorf("controller stopped: %v", runErr)
				}
			case <-time.After(10 * time.Second):
				t.Error("controller did not stop")
			}
		})
	}
	t.Cleanup(stop)
	select {
	case address := <-handler.started:
		return "http://" + address, stop
	case <-finished:
		t.Fatalf("controller failed before startup: %v", runErr)
	case <-time.After(10 * time.Second):
		t.Fatal("controller did not start")
	}
	return "", stop
}

func doControllerRequest(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("controller request: %v", err)
	}
	return response
}

func loginController(t *testing.T, url string) *http.Cookie {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/api/v1/auth/login", strings.NewReader(`{"email":"camille@example.com","password":"a long password for testing"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	if response.StatusCode != http.StatusNoContent || len(body) != 0 || response.Header.Get("Cache-Control") != "no-store" || len(cookies) != 1 || cookies[0].Name != "arveld_session" || cookies[0].Value == "" {
		t.Fatalf("controller login = %d %q", response.StatusCode, body)
	}
	return cookies[0]
}
