package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPMonitorListReturnsPersistedDefinitions(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/monitors/http", nil)
		if authenticated {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	empty := request(http.MethodGet, true)
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"monitors":[]}` {
		t.Fatalf("empty list = %d %s", empty.Code, empty.Body.String())
	}
	for _, id := range []byte{2, 1} {
		uid := agent.InstanceUID{id}
		if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
			t.Fatal(err)
		}
		value := monitor.Monitor{
			Protocol: "http",
			ID:       uid.String(), Name: "Homepage", AgentInstanceUID: uid,
			Endpoint: "https://example.com", Method: "HEAD", IntervalSeconds: 30, TimeoutSeconds: 5,
		}
		if err := monitor.NewStore(db).Create(t.Context(), value); err != nil {
			t.Fatal(err)
		}
	}
	response := request(http.MethodGet, true)
	var result struct {
		Monitors []httpMonitorResult `json:"monitors"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Monitors) != 2 {
		t.Fatalf("list = %+v", result)
	}
	for i, value := range result.Monitors {
		uid := agent.InstanceUID{byte(i + 1)}.String()
		want := httpMonitorResult{
			ID: uid, Name: "Homepage", AgentInstanceUID: uid,
			Endpoint: "https://example.com", Method: "HEAD", IntervalSeconds: 30, TimeoutSeconds: 5,
		}
		if value != want {
			t.Fatalf("Monitor = %+v, want %+v", value, want)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("list must not be cached")
	}
	if head := request(http.MethodHead, true); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %s", head.Code, head.Body.String())
	}
	if anonymous := request(http.MethodGet, false); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", anonymous.Code)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if failed := request(http.MethodGet, true); failed.Code != http.StatusInternalServerError {
		t.Fatalf("closed database = %d", failed.Code)
	}
}
