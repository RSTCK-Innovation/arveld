package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAgentThresholdRuleLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	_, token, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	request := func(method, url, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	collection := "/api/v1/agents/" + uid.String() + "/alert-rules"
	for _, condition := range []string{"cpu", "memory", "disk"} {
		body := fmt.Sprintf(`{"condition":%q,"threshold":85.5,"for_seconds":120,"severity":"warning"}`, condition)
		created := request(http.MethodPost, collection, body)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s rule = %d %s", condition, created.Code, created.Body.String())
		}
		var value struct {
			ID        string  `json:"id"`
			AgentID   string  `json:"agent_instance_uid"`
			MonitorID string  `json:"monitor_id"`
			Threshold float64 `json:"threshold"`
		}
		if err := json.Unmarshal(created.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.ID == "" || value.AgentID != uid.String() || value.MonitorID != "" || value.Threshold != 85.5 {
			t.Fatalf("wrong Agent ownership or threshold: %+v", value)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db = testutil.OpenDatabase(t, path)
		handler = newAccountHandler(db)
		location := created.Header().Get("Location")
		read := request(http.MethodGet, location, "")
		if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
			t.Fatalf("rule changed after reopen: %d %s", read.Code, read.Body.String())
		}
		listed := request(http.MethodGet, collection, "")
		if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), value.ID) {
			t.Fatalf("Agent rule not listed: %d %s", listed.Code, listed.Body.String())
		}
		for _, invalid := range []string{strings.Replace(body, "85.5", "0", 1), strings.Replace(body, "85.5", "100.1", 1), strings.Replace(body, condition, "latency", 1)} {
			if got := request(http.MethodPut, location, invalid); got.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid Agent rule = %d %s", got.Code, got.Body.String())
			}
		}
		updated := request(http.MethodPut, location, strings.Replace(body, "85.5", "90", 1))
		if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"threshold":90`) {
			t.Fatalf("update Agent rule = %d %s", updated.Code, updated.Body.String())
		}
		if got := request(http.MethodDelete, location, ""); got.Code != http.StatusNoContent {
			t.Fatalf("delete Agent rule = %d", got.Code)
		}
		if got := request(http.MethodGet, location, ""); got.Code != http.StatusNotFound {
			t.Fatalf("deleted rule = %d", got.Code)
		}
	}
	if got := request(http.MethodPost, "/api/v1/agents/02000000-0000-0000-0000-000000000000/alert-rules", `{"condition":"cpu","threshold":80,"for_seconds":60,"severity":"warning"}`); got.Code != http.StatusNotFound {
		t.Fatalf("missing Agent = %d %s", got.Code, got.Body.String())
	}
}
