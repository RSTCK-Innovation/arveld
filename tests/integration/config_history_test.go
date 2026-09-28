package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestConfigurationHistoryKeepsCreationDatesWhenReusingRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	uid := agent.InstanceUID{9}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	before := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	content := []byte("receivers:\n  nop: {}\n")
	if _, err := store.Save(t.Context(), uid, content); err != nil {
		t.Fatal(err)
	}
	type item struct {
		Revision  int64      `json:"revision"`
		CreatedAt *time.Time `json:"created_at"`
	}
	read := func() []item {
		t.Helper()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/api/v1/agents/"+uid.String()+"/config/revisions", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("history status = %d, body %s", response.Code, response.Body.String())
		}
		var body struct {
			Revisions []item `json:"revisions"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Revisions
	}
	history := read()
	if len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 1 {
		t.Fatalf("history = %+v, want newest revision first", history)
	}
	for _, revision := range history {
		if revision.CreatedAt == nil || revision.CreatedAt.Before(before) || revision.CreatedAt.After(time.Now()) {
			t.Fatalf("missing or invalid creation time: %+v", revision)
		}
	}
	if _, err := store.Rollback(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), uid, content); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got, history) {
		t.Fatalf("reusing a revision changed history: %+v, want %+v", got, history)
	}
}

func TestConfigurationRevisionReadsExactHistoricalYAML(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	uid := agent.InstanceUID{9}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	content := []byte("# Historical YAML\nexporters:\n  otlphttp:\n    endpoint: '${env:ARVELD_URL}'\n")
	if _, err := store.Save(t.Context(), uid, content); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), uid, []byte("receivers: {}\n")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, method string
		anonymous          bool
		status             int
	}{
		{"historical YAML", uid.String() + "/config/revisions/1", http.MethodGet, false, http.StatusOK},
		{"HEAD", uid.String() + "/config/revisions/1", http.MethodHead, false, http.StatusOK},
		{"absent revision", uid.String() + "/config/revisions/3", http.MethodGet, false, http.StatusNotFound},
		{"another Agent", (agent.InstanceUID{10}).String() + "/config/revisions/1", http.MethodGet, false, http.StatusNotFound},
		{"invalid revision", uid.String() + "/config/revisions/0", http.MethodGet, false, http.StatusBadRequest},
		{"requires authentication", uid.String() + "/config/revisions/1", http.MethodGet, true, http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), test.method, "/api/v1/agents/"+test.path, nil)
			if !test.anonymous {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.status == http.StatusOK {
				if response.Header().Get("Content-Type") != "application/x-yaml" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("YAML response headers = %v", response.Header())
				}
				if test.method == http.MethodGet && response.Body.String() != string(content) {
					t.Fatalf("historical YAML changed: %q", response.Body.String())
				}
				if test.method == http.MethodHead && response.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
			}
		})
	}
}
