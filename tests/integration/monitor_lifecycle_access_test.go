package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMonitorLifecycleRejectsInvalidOrUnauthorizedWrites(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	_, readKey, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writeKey, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	value := monitor.Monitor{ID: "homepage", Protocol: "http", Name: "Homepage", AgentInstanceUID: uid, Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5}
	store := monitor.NewStore(db)
	if err := store.Create(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	const body = `{"name":"Changed homepage","agent_instance_uid":"01000000-0000-0000-0000-000000000000","endpoint":"https://example.com","method":"HEAD","interval_seconds":60,"timeout_seconds":10}`
	request := func(method, id, input, token, origin string, session *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example/api/v1/monitors/"+id, strings.NewReader(input))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if session != nil {
			req.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, access := range []struct {
			name, token, origin string
			cookie              *http.Cookie
			status              int
		}{
			{"anonymous", "", "", nil, http.StatusUnauthorized},
			{"reader", readKey, "", nil, http.StatusForbidden},
			{"Agent credential", agentKey, "", nil, http.StatusUnauthorized},
			{"invalid bearer overrides session", "invalid", "", cookie, http.StatusUnauthorized},
			{"foreign origin", "", "https://foreign.example", cookie, http.StatusForbidden},
		} {
			t.Run(method+"/"+access.name, func(t *testing.T) {
				response := request(method, value.ID, body, access.token, access.origin, access.cookie)
				if response.Code != access.status || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("access = %d %s, want %d without caching", response.Code, response.Body.String(), access.status)
				}
			})
		}
		if response := request(method, "missing", body, "", "", cookie); response.Code != http.StatusNotFound {
			t.Fatalf("missing Monitor = %d", response.Code)
		}
	}
	for _, invalid := range []struct {
		name, input string
		status      int
	}{
		{"protocol is immutable", strings.Replace(body, `"name":`, `"protocol":"tcp","name":`, 1), 400},
		{"ID is immutable", strings.Replace(body, `"name":`, `"id":"other","name":`, 1), 400},
		{"cross-protocol fields", strings.Replace(body, `"name":`, `"ping_count":2,"name":`, 1), 400},
		{"malformed JSON", "{", 400},
		{"multiple documents", body + "{}", 400},
		{"oversized payload", body + strings.Repeat(" ", 512*1024), 413},
		{"missing Agent", strings.Replace(body, "01000000", "02000000", 1), 422},
		{"invalid timing", strings.Replace(body, `"timeout_seconds":10`, `"timeout_seconds":61`, 1), 422},
		{"invalid name", strings.Replace(body, "Changed homepage", " ", 1), 422},
		{"invalid endpoint", strings.Replace(body, "https://example.com", "ftp://example.com", 1), 422},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			response := request(http.MethodPut, value.ID, invalid.input, "", "", cookie)
			if response.Code != invalid.status {
				t.Fatalf("invalid update = %d %s, want %d", response.Code, response.Body.String(), invalid.status)
			}
		})
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		operation := "UPDATE"
		if method == http.MethodDelete {
			operation = "DELETE"
		}
		if _, err := db.ExecContext(t.Context(), "CREATE TRIGGER reject_monitor_change BEFORE "+operation+" ON monitors BEGIN SELECT RAISE(ABORT, 'test-only storage failure'); END"); err != nil {
			t.Fatal(err)
		}
		response := request(method, value.ID, body, "", "", cookie)
		if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
			t.Fatalf("failed storage = %d %s", response.Code, response.Body.String())
		}
		if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_monitor_change"); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := store.Get(t.Context(), value.ID)
	if err != nil || !reflect.DeepEqual(stored, value) {
		t.Fatalf("rejected mutations changed the Monitor: %+v, %v", stored, err)
	}
	if response := request(http.MethodPut, value.ID, body, writeKey, "", nil); response.Code != http.StatusOK {
		t.Fatalf("write-key update = %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, value.ID, "", writeKey, "", nil); response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("write-key delete = %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, value.ID, "", writeKey, "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("repeated deletion = %d", response.Code)
	}
}
