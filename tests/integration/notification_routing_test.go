package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMonitorRuleNotificationRoutingLifecycle(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{ID: "home", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid, Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5}); err != nil {
		t.Fatal(err)
	}
	channels := notification.NewStore(db)
	for _, id := range []string{"alpha", "beta"} {
		if _, err := channels.Create(t.Context(), notification.Channel{ID: id, Name: id, Type: "webhook", Config: json.RawMessage(`{"url":"https://hooks.example.com/` + id + `"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s = %d %s, want %d", method, path, res.Code, res.Body.String(), want)
		}
		return res
	}
	collection := "/api/v1/monitors/home/alert-rules"
	body := `{"condition":"failed","for_seconds":2,"severity":"critical","notification_ids":["alpha","beta"]}`
	created := request(http.MethodPost, collection, body, http.StatusCreated)
	path := created.Header().Get("Location")
	var rule struct {
		ID              string   `json:"id"`
		NotificationIDs []string `json:"notification_ids"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	if len(rule.NotificationIDs) != 2 || rule.NotificationIDs[0] != "alpha" || rule.NotificationIDs[1] != "beta" {
		t.Fatalf("missing channel assignments: %s", created.Body.String())
	}
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct{ method, path string }{{http.MethodPost, collection}, {http.MethodPut, path}, {http.MethodDelete, path}} {
		for _, credential := range []string{"reader", "foreign origin", "anonymous"} {
			req := httptest.NewRequestWithContext(t.Context(), operation.method, "https://arveld.example"+operation.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			want := http.StatusForbidden
			switch credential {
			case "reader":
				req.Header.Set("Authorization", "Bearer "+reader)
			case "foreign origin":
				req.AddCookie(cookie)
				req.Header.Set("Origin", "https://other.example")
			case "anonymous":
				want = http.StatusUnauthorized
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != want {
				t.Fatalf("%s as %s = %d, want %d", operation.method, credential, response.Code, want)
			}
		}
	}

	listed := request(http.MethodGet, collection, "", http.StatusOK)
	if !strings.Contains(listed.Body.String(), rule.ID) {
		t.Fatal("created rule is missing from list")
	}
	request(http.MethodHead, collection, "", http.StatusOK)
	request(http.MethodDelete, "/api/v1/notifications/alpha", "", http.StatusConflict)
	for _, ids := range []string{`["missing"]`, `["alpha","alpha"]`, `[""]`} {
		request(http.MethodPut, path, `{"condition":"no_data","for_seconds":3,"severity":"warning","notification_ids":`+ids+`}`, http.StatusUnprocessableEntity)
	}
	if got := request(http.MethodGet, path, "", http.StatusOK).Body.String(); !strings.Contains(got, `"condition":"failed"`) || !strings.Contains(got, `"notification_ids":["alpha","beta"]`) {
		t.Fatal("rejected update changed the rule")
	}
	request(http.MethodPut, path, `{"condition":"no_data","for_seconds":3,"severity":"warning","notification_ids":["beta"]}`, http.StatusOK)
	request(http.MethodDelete, "/api/v1/notifications/alpha", "", http.StatusNoContent)
	request(http.MethodDelete, path, "", http.StatusNoContent)
	request(http.MethodDelete, path, "", http.StatusNotFound)
	request(http.MethodPut, path, body, http.StatusNotFound)
	request(http.MethodDelete, "/api/v1/notifications/beta", "", http.StatusNoContent)
	request(http.MethodGet, "/api/v1/monitors/missing/alert-rules", "", http.StatusNotFound)
}
