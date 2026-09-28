package notification_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestPublicationRequiresAppliedConfigAndRetries(t *testing.T) {
	directory := t.TempDir()
	store := notification.NewStore(testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db")))
	var mode, reloads atomic.Int32
	var loaded atomic.Bool
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /-/reload":
			reloads.Add(1)
			switch mode.Load() {
			case 1:
				http.Error(w, "test-only-secret", http.StatusServiceUnavailable)
			case 2:
				loaded.Store(true)
			}
		case "GET /metrics":
			var hash int64
			if loaded.Load() {
				// Fixed fingerprint of the generated empty configuration; no engine code is embedded.
				hash = 160781325551633
			}
			if _, err := fmt.Fprintf(w, "# TYPE alertmanager_config_hash gauge\nalertmanager_config_hash %d\n# TYPE alertmanager_config_last_reload_successful gauge\nalertmanager_config_last_reload_successful 1\n", hash); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected engine request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(engine.Close)
	publisher, err := notification.NewPublisher(store, engine.URL, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Sync(t.Context()); err == nil {
		t.Fatal("HTTP 200 without the desired configuration was accepted")
	}
	mode.Store(1)
	if err := publisher.Sync(t.Context()); err == nil || strings.Contains(err.Error(), "test-only-secret") {
		t.Fatalf("expected a reload error without the response body, got %v", err)
	}
	mode.Store(2)
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatalf("retry already-written configuration: %v", err)
	}
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if reloads.Load() != 3 {
		t.Fatalf("reload count = %d, want three attempts and no reload when unchanged", reloads.Load())
	}
}
