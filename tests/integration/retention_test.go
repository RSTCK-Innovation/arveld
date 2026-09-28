package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestInstanceRetentionReadsEnginePolicy(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	_, token, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Retention reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body, policy string
		status, wantStatus int
	}{
		{"time", `{"status":"success","data":{"storageRetention":"15d","CWD":"private"}}`, "15d", 200, 200},
		{"time and size", `{"status":"success","data":{"storageRetention":"2w or 1GiB"}}`, "2w or 1GiB", 200, 200},
		{"size only", `{"status":"success","data":{"storageRetention":"1GiB"}}`, "1GiB", 200, 200},
		{"disabled", `{"status":"success","data":{"storageRetention":""}}`, "", 200, 200},
		{"unavailable", "private failure details", "", 503, 502},
		{"unsuccessful", `{"status":"error","data":{"storageRetention":"15d"}}`, "", 200, 502},
		{"missing", `{"status":"success","data":{}}`, "", 200, 502},
		{"null", `{"status":"success","data":{"storageRetention":null}}`, "", 200, 502},
		{"wrong type", `{"status":"success","data":{"storageRetention":15}}`, "", 200, 502},
		{"partial", `{"status":"success","warnings":["partial"],"data":{"storageRetention":"15d"}}`, "", 200, 502},
		{"malformed", "not JSON", "", 200, 502},
		{"oversized", strings.Repeat(" ", 64*1024+1), "", 200, 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/status/runtimeinfo" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected native request: %s %s (authorization must not be forwarded)", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body)) //nolint:errcheck // Fixture response; assertions are made at the Arveld boundary.
			}))
			defer upstream.Close()
			deps := accountDependencies(db)
			deps.Prometheus, err = prometheus.NewClient(upstream.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/settings/retention", nil)
			denied := httptest.NewRecorder()
			handler.ServeHTTP(denied, request)
			if denied.Code != http.StatusUnauthorized || calls.Load() != 0 {
				t.Fatalf("anonymous read = %d, upstream calls = %d", denied.Code, calls.Load())
			}
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || response.Header().Get("Cache-Control") != "no-store" || calls.Load() != 1 {
				t.Fatalf("retention read = %d %q (calls %d), want %d without caching", response.Code, response.Body.String(), calls.Load(), test.wantStatus)
			}
			if test.wantStatus != http.StatusOK {
				if response.Body.String() != "Bad Gateway\n" {
					t.Fatal("upstream failures must remain generic")
				}
				return
			}
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			policy, present := result["storage_retention"]
			if !present || len(result) != 1 || policy != test.policy {
				t.Fatalf("retention projection = %v, want only storage_retention=%q", result, test.policy)
			}
		})
	}
}
