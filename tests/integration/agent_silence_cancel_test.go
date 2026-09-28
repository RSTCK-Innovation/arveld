package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAgentSilenceCancellationBoundaries(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{0xab}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	_, writer, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	const id = "4d849af4-fcb5-4af9-937d-b818389dc387"
	path := "/api/v1/agents/" + uid.String() + "/silences/" + id
	valid := `[{"id":"` + id + `","matchers":[{"name":"arveld_agent_id","value":"` + uid.String() + `","isRegex":false,"isEqual":true}],"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T00:30:00Z","status":{"state":"active"}}]`
	for _, test := range []struct {
		name, path, token, origin, list, body string
		session                               *http.Cookie
		upstreamStatus, want, reads, deletes  int
	}{
		{name: "session", session: cookie, list: valid, upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "write key", token: writer, list: valid, upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "canonical UID", path: strings.ReplaceAll(path, uid.String(), strings.ToUpper(uid.String())), token: writer, list: valid, upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "pending window", token: writer, list: strings.ReplaceAll(valid, "active", "pending"), upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "expired window", token: writer, list: strings.ReplaceAll(valid, "active", "expired"), upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "anonymous", want: 401},
		{name: "read key", token: reader, want: 403},
		{name: "Agent key", token: createAgentKey(t, db), want: 401},
		{name: "invalid bearer overrides session", token: "invalid", session: cookie, want: 401},
		{name: "foreign origin", session: cookie, origin: "https://foreign.example", want: 403},
		{name: "missing Agent", path: strings.ReplaceAll(path, uid.String(), (agent.InstanceUID{2}).String()), token: writer, want: 404},
		{name: "invalid UID", path: "/api/v1/agents/invalid/silences/" + id, token: writer, want: 404},
		{name: "missing silence", token: writer, list: `[]`, want: 404, reads: 1},
		{name: "different silence", token: writer, list: strings.ReplaceAll(valid, id, "another-id"), want: 404, reads: 1},
		{name: "another Agent", token: writer, list: strings.ReplaceAll(valid, uid.String(), (agent.InstanceUID{2}).String()), want: 404, reads: 1},
		{name: "Monitor with same identity", token: writer, list: strings.ReplaceAll(valid, "arveld_agent_id", "arveld_monitor_id"), want: 404, reads: 1},
		{name: "rule scope", token: writer, list: strings.ReplaceAll(valid, `"matchers":[`, `"matchers":[{"name":"arveld_rule_id","value":"rule","isRegex":false,"isEqual":true},`), want: 404, reads: 1},
		{name: "regex scope", token: writer, list: strings.ReplaceAll(valid, `"isRegex":false`, `"isRegex":true`), want: 404, reads: 1},
		{name: "negative scope", token: writer, list: strings.ReplaceAll(valid, `"isEqual":true`, `"isEqual":false`), want: 404, reads: 1},
		{name: "invalid native collection", token: writer, list: `null`, want: 502, reads: 1},
		{name: "native disappeared", token: writer, list: valid, upstreamStatus: 404, want: 404, reads: 1, deletes: 1},
		{name: "native rejection", token: writer, list: valid, upstreamStatus: 503, body: "private details", want: 502, reads: 1, deletes: 1},
		{name: "native redirect", token: writer, list: valid, upstreamStatus: 307, want: 502, reads: 1, deletes: 1},
		{name: "oversized response", token: writer, list: valid, upstreamStatus: 200, body: strings.Repeat("x", 4097), want: 502, reads: 1, deletes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reads, deletes atomic.Int32
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					reads.Add(1)
					if r.URL.Path != "/api/v2/silences" || r.URL.Query().Get("filter") != `arveld_agent_id="`+uid.String()+`"` {
						t.Errorf("unexpected native scope read: %s", r.URL)
					}
					if _, err := io.WriteString(w, test.list); err != nil {
						t.Error(err)
					}
				case http.MethodDelete:
					deletes.Add(1)
					if r.URL.Path != "/api/v2/silence/"+id {
						t.Errorf("unexpected native cancellation: %s", r.URL)
					}
					w.Header().Set("Location", "/unexpected-redirect")
					w.WriteHeader(test.upstreamStatus)
					if _, err := io.WriteString(w, test.body); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected native method: %s", r.Method)
				}
			}))
			t.Cleanup(engine.Close)
			deps := accountDependencies(db)
			deps.Silences, err = notification.NewSilenceClient(engine.URL)
			if err != nil {
				t.Fatal(err)
			}
			target := path
			if test.path != "" {
				target = test.path
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "https://arveld.example"+target, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.session != nil {
				request.AddCookie(test.session)
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps).ServeHTTP(response, request)
			if response.Code != test.want || response.Header().Get("Cache-Control") != "no-store" || int(reads.Load()) != test.reads || int(deletes.Load()) != test.deletes {
				t.Fatalf("cancel Agent silence = %d %s, native reads/deletes %d/%d; want %d, %d/%d", response.Code, response.Body.String(), reads.Load(), deletes.Load(), test.want, test.reads, test.deletes)
			}
			if test.want == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Fatal("successful cancellation must have no body")
				}
			} else if response.Body.String() != http.StatusText(test.want)+"\n" {
				t.Fatal("failure exposed native details")
			}
		})
	}
}
