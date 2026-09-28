package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestIncidentHistoryPermissionsAndFilters(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	// Historical rows have no foreign keys; seed episodes whose resources were removed.
	for _, entry := range []struct {
		kind, owner, severity string
		closed                any
		reason                string
	}{
		{"monitor", "checkout", "critical", nil, ""},
		{"agent", "agent", "warning", 2000, "rule_removed"},
	} {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO incidents
   (rule_id,rule_revision,owner_kind,owner_id,owner_name,condition,for_seconds,severity,opened_at,last_evaluated_at,closed_at,close_reason)
   VALUES (?,?,?,?,?,'failed',60,?,1000,1000,?,?)`, entry.owner, "revision", entry.kind, entry.owner, entry.owner, entry.severity, entry.closed, entry.reason); err != nil {
			t.Fatal(err)
		}
	}
	tokens := map[string]string{"anonymous": "", "agent": createAgentKey(t, db)}
	for _, permission := range []string{"read", "write"} {
		_, token, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: permission, Permission: permission})
		if err != nil {
			t.Fatal(err)
		}
		tokens[permission] = token
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, identity, origin string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, nil)
		if identity == "session" {
			req.AddCookie(cookie)
		} else if tokens[identity] != "" {
			req.Header.Set("Authorization", "Bearer "+tokens[identity])
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, test := range []struct {
		method, path, identity, origin string
		status                         int
	}{
		{"GET", "/api/v1/incidents", "anonymous", "", 401},
		{"GET", "/api/v1/incidents/1", "agent", "", 401},
		{"GET", "/api/v1/incidents", "read", "", 200},
		{"HEAD", "/api/v1/incidents", "read", "", 200},
		{"HEAD", "/api/v1/incidents/1", "read", "", 200},
		{"GET", "/api/v1/incidents/999", "read", "", 404},
		{"GET", "/api/v1/incidents/nope", "read", "", 404},
		{"POST", "/api/v1/incidents/1/acknowledgment", "read", "", 403},
		{"POST", "/api/v1/incidents/1/acknowledgment", "session", "https://other.example", 403},
		{"POST", "/api/v1/incidents/2/acknowledgment", "write", "", 409},
		{"POST", "/api/v1/incidents/999/acknowledgment", "write", "", 404},
	} {
		response := request(test.method, test.path, test.identity, test.origin)
		if response.Code != test.status {
			t.Fatalf("%s %s (%s) = %d %s, want %d", test.method, test.path, test.identity, response.Code, response.Body.String(), test.status)
		}
		if test.method == http.MethodHead && response.Body.Len() != 0 {
			t.Fatal("HEAD wrote a body")
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=abc", "before=-1", "before=0", "before=1.5", "status=resolved", "severity=urgent", "owner_kind=host"} {
		response := request("GET", "/api/v1/incidents?"+query, "read", "")
		if response.Code != 422 {
			t.Fatalf("invalid %s = %d", query, response.Code)
		}
	}
	for query, want := range map[string]int{"status=open": 1, "status=closed": 1, "severity=critical&owner_kind=agent": 0, "owner_kind=monitor&owner_id=checkout": 1, "owner_id=missing": 0, "limit=1&before=2": 1} {
		response := request("GET", "/api/v1/incidents?"+query, "read", "")
		var page alert.IncidentPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || response.Code != 200 || len(page.Incidents) != want {
			t.Fatalf("filter %s = %d %s %v", query, response.Code, response.Body.String(), err)
		}
		if want == 0 && page.Incidents == nil {
			t.Fatal("empty history must serialize as an array")
		}
	}
	first := request("POST", "/api/v1/incidents/1/acknowledgment", "session", "https://arveld.example")
	repeated := request("POST", "/api/v1/incidents/1/acknowledgment", "write", "")
	var incident alert.Incident
	if err := json.Unmarshal(first.Body.Bytes(), &incident); err != nil || first.Code != 200 || repeated.Code != 200 || first.Body.String() != repeated.Body.String() || incident.AcknowledgedBy == "" || incident.AcknowledgedBy == "Management API key" || incident.Status != "open" {
		t.Fatalf("acknowledgment changed actor/state or was not idempotent: %d %s / %d %s (%v)", first.Code, first.Body.String(), repeated.Code, repeated.Body.String(), err)
	}
}
