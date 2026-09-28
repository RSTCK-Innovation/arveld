package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAlertRuleStateAccessAndEngineErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if err := alert.NewStore(db).Create(t.Context(), alert.Rule{
		ID: "homepage-failed", MonitorID: "homepage", Condition: "failed", ForSeconds: 120, Severity: "critical",
	}); err != nil {
		t.Fatal(err)
	}
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	var mu sync.Mutex
	body, status, calls := `{"status":"success","data":{"groups":[]}}`, http.StatusOK, 0
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/rules" || r.URL.Query().Get("rule_group[]") != "arveld-monitor-alerts" {
			t.Errorf("expected scoped native rule read, got %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(status)
		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(engine.Close)
	client, err := prometheus.NewClient(engine.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deps := accountDependencies(db)
	deps.Prometheus = client
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, token string, session *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequestWithContext(t.Context(), method, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if session != nil {
			r.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	const path = "/api/v1/alert-rules/homepage-failed/state"
	for _, credentials := range []struct {
		token   string
		session *http.Cookie
	}{{}, {token: agentKey}, {token: "invalid", session: cookie}} {
		if response := request(http.MethodGet, path, credentials.token, credentials.session); response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized state read = %d, want 401", response.Code)
		}
	}
	if response := request(http.MethodHead, "/api/v1/alert-rules/missing/state", reader, nil); response.Code != http.StatusNotFound {
		t.Fatalf("missing rule = %d, want 404", response.Code)
	}
	mu.Lock()
	count := calls
	mu.Unlock()
	if count != 0 {
		t.Fatal("unauthorized or missing-rule requests reached Prometheus")
	}
	response := request(http.MethodGet, path, reader, nil)
	var state alert.State
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != http.StatusOK ||
		state.SyncStatus != "pending" || state.State != "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unloaded rule must report uncached pending configuration: %d %s (%v)", response.Code, response.Body, err)
	}
	response = request(http.MethodHead, path, "", cookie)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("state HEAD = %d %s, want JSON headers and no body", response.Code, response.Body)
	}
	if response := request(http.MethodGet, "/api/v1/alert-rules/homepage-failed/observation", reader, nil); response.Code != http.StatusNotFound {
		t.Fatal("the obsolete observation endpoint still exists")
	}
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"unavailable", "sensitive engine detail", 503},
		{"malformed", "{", 200},
		{"multiple documents", "{} {}", 200},
		{"engine error", `{"status":"error","data":{"groups":[]}}`, 200},
		{"partial", `{"status":"success","warnings":["partial"],"data":{"groups":[]}}`, 200},
		{"paginated", `{"status":"success","data":{"groups":[],"groupNextToken":"more"}}`, 200},
		{"missing groups", `{"status":"success","data":{}}`, 200},
		{"null data", `{"status":"success","data":null}`, 200},
		{"foreign group", `{"status":"success","data":{"groups":[{"name":"other","rules":[]}]}}`, 200},
		{"invalid rule", `{"status":"success","data":{"groups":[{"name":"arveld-monitor-alerts","rules":[{}]}]}}`, 200},
		{"oversized", strings.Repeat(" ", 4*1024*1024+1), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			mu.Lock()
			body, status = test.body, test.status
			mu.Unlock()
			response := request(http.MethodGet, path, "", cookie)
			if response.Code != http.StatusBadGateway || response.Body.String() != "Bad Gateway\n" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("invalid engine response must produce generic 502: %d %s", response.Code, response.Body)
			}
		})
	}
}
