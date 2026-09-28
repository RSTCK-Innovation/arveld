package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPMonitorOptionsSurviveCreateReadAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: agent.InstanceUID{1}}); err != nil {
		t.Fatal(err)
	}
	_, key, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	request := func(method, path, body string) map[string]any {
		t.Helper()
		r := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("%s %s = %d %s", method, path, w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		delete(result, "protocol")
		return result
	}
	const body = `{"name":"API readiness","agent_instance_uid":"01000000-0000-0000-0000-000000000000","endpoint":"https://example.com/health","method":"POST","interval_seconds":30,"timeout_seconds":5,"body":"{\"ready\":true}","headers":{"Authorization":"Bearer synthetic-$token"},"skip_tls_verify":true,"validations":[{"type":"json_path","path":"ready","equals":"true"},{"type":"min_size","size":1}]}`
	created := request(http.MethodPost, "/api/v1/monitors/http", body)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatal("missing Monitor ID")
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	want["id"] = id
	if !reflect.DeepEqual(created, want) {
		t.Fatalf("created options = %#v, want %#v", created, want)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	if got := request(http.MethodGet, "/api/v1/monitors/"+id, ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored options = %#v, want %#v", got, want)
	}
	updated := request(http.MethodPut, "/api/v1/monitors/"+id, strings.Replace(body, `"POST"`, `"DELETE"`, 1))
	want["method"] = "DELETE"
	if !reflect.DeepEqual(updated, want) {
		t.Fatalf("updated options = %#v, want %#v", updated, want)
	}
	desired, err := remoteconfig.NewStore(db).Desired(t.Context(), agent.InstanceUID{1})
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{"Bearer synthetic-$$token", "auto_content_type: true", "json_path: ready", "min_size: 1", "insecure_skip_verify: true", "method: DELETE"} {
		if !strings.Contains(string(desired.Content), literal) {
			t.Fatalf("reconciled configuration lost %q", literal)
		}
	}
	// Clearing options must clear the compiled request, without touching immutable history.
	cleared := `{"name":"API readiness","agent_instance_uid":"01000000-0000-0000-0000-000000000000","endpoint":"https://example.com/health","method":"GET","interval_seconds":30,"timeout_seconds":5}`
	updated = request(http.MethodPut, "/api/v1/monitors/"+id, cleared)
	for _, field := range []string{"body", "headers", "validations", "skip_tls_verify"} {
		if _, exists := updated[field]; exists {
			t.Fatalf("cleared response retains %s", field)
		}
	}
	latest, err := remoteconfig.NewStore(db).Desired(t.Context(), agent.InstanceUID{1})
	if err != nil || latest.Number <= desired.Number || strings.Contains(string(latest.Content), "synthetic-") || strings.Contains(string(latest.Content), "validations:") || strings.Contains(string(latest.Content), "skip_verify") {
		t.Fatal("cleared options did not reconcile")
	}
	old, err := remoteconfig.NewStore(db).RevisionContent(t.Context(), agent.InstanceUID{1}, desired.Number)
	if err != nil || !reflect.DeepEqual(old, desired.Content) {
		t.Fatal("clearing options rewrote history")
	}
}
