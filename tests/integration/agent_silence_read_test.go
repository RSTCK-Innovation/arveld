package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAgentSilencesAppearInScopedAndGlobalReads(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{0xab}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	// Return a mixed native collection even for filtered requests to exercise scope checks.
	const entry = `{"id":"ID","matchers":[MATCHERS],"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T00:30:00Z","createdBy":"Arveld","comment":"Maintenance","status":{"state":"STATE"}}`
	agentMatcher := `{"name":"arveld_agent_id","value":"` + uid.String() + `","isRegex":false,"isEqual":true}`
	var entries []string
	for _, fixture := range []struct{ id, matcher, state string }{
		{"agent-active", agentMatcher, "active"},
		{"monitor", strings.ReplaceAll(strings.ReplaceAll(agentMatcher, "arveld_agent_id", "arveld_monitor_id"), uid.String(), "homepage"), "active"},
		{"agent-pending", agentMatcher, "pending"},
		{"agent-expired", agentMatcher, "expired"},
		{"deleted-agent", strings.ReplaceAll(agentMatcher, uid.String(), (agent.InstanceUID{2}).String()), "active"},
		{"rule-only", agentMatcher + `,{"name":"arveld_rule_id","value":"rule","isRegex":false,"isEqual":true}`, "active"},
		{"regex", strings.ReplaceAll(agentMatcher, `"isRegex":false`, `"isRegex":true`), "active"},
		{"negative", strings.ReplaceAll(agentMatcher, `"isEqual":true`, `"isEqual":false`), "active"},
		{"empty", strings.ReplaceAll(agentMatcher, uid.String(), ""), "active"},
		{"unrelated", strings.ReplaceAll(agentMatcher, "arveld_agent_id", "service"), "active"},
	} {
		entries = append(entries, strings.NewReplacer("ID", fixture.id, "MATCHERS", fixture.matcher, "STATE", fixture.state).Replace(entry))
	}
	var calls atomic.Int32
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		filter := r.URL.Query().Get("filter")
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/silences" ||
			(filter != "" && filter != `arveld_agent_id="`+uid.String()+`"` && filter != `arveld_monitor_id="homepage"`) {
			t.Errorf("unexpected native read: %s %s", r.Method, r.URL)
		}
		if _, err := io.WriteString(w, "["+strings.Join(entries, ",")+"]"); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(engine.Close)
	deps := accountDependencies(db)
	deps.Silences, err = notification.NewSilenceClient(engine.URL)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	cookie := login(t, handler, "a long password for testing")
	path := "/api/v1/agents/" + uid.String() + "/silences"
	for _, test := range []struct {
		name, path, token string
		session           *http.Cookie
		status            int
		ids               []string
	}{
		{"Agent session", path, "", cookie, 200, []string{"agent-active", "agent-pending", "agent-expired"}},
		{"canonical UID", strings.ReplaceAll(path, uid.String(), strings.ToUpper(uid.String())), reader, nil, 200, []string{"agent-active", "agent-pending", "agent-expired"}},
		{"global", "/api/v1/silences", reader, nil, 200, []string{"agent-active", "monitor", "agent-pending", "agent-expired", "deleted-agent"}},
		{"Monitor stays scoped", "/api/v1/monitors/homepage/silences", reader, nil, 200, []string{"monitor"}},
		{"missing Agent", strings.ReplaceAll(path, uid.String(), (agent.InstanceUID{2}).String()), reader, nil, 404, nil},
		{"invalid UID", "/api/v1/agents/invalid/silences", reader, nil, 404, nil},
		{"anonymous", path, "", nil, 401, nil},
		{"Agent credential", path, createAgentKey(t, db), nil, 401, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				before := calls.Load()
				request := httptest.NewRequestWithContext(t.Context(), method, test.path, nil)
				if test.token != "" {
					request.Header.Set("Authorization", "Bearer "+test.token)
				}
				if test.session != nil {
					request.AddCookie(test.session)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s silences = %d %s, want uncached %d", method, response.Code, response.Body.String(), test.status)
				}
				if test.status != http.StatusOK {
					if calls.Load() != before {
						t.Fatal("rejected read reached Alertmanager")
					}
					continue
				}
				if calls.Load() != before+1 || response.Header().Get("Content-Type") != "application/json" {
					t.Fatal("read must use native state and return JSON")
				}
				if method == http.MethodHead {
					if response.Body.Len() != 0 {
						t.Fatal("HEAD must have no body")
					}
					continue
				}
				var result struct {
					Silences []struct {
						ID               string `json:"id"`
						State            string `json:"state"`
						Comment          string `json:"comment"`
						MonitorID        string `json:"monitor_id"`
						AgentInstanceUID string `json:"agent_instance_uid"`
						StartsAt         string `json:"starts_at"`
						EndsAt           string `json:"ends_at"`
					} `json:"silences"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				var ids []string
				for _, value := range result.Silences {
					ids = append(ids, value.ID)
					wantState, wantAgent, wantMonitor := "active", uid.String(), ""
					switch value.ID {
					case "monitor":
						wantAgent, wantMonitor = "", "homepage"
					case "deleted-agent":
						wantAgent = (agent.InstanceUID{2}).String()
					case "agent-pending":
						wantState = "pending"
					case "agent-expired":
						wantState = "expired"
					}
					if value.AgentInstanceUID != wantAgent || value.MonitorID != wantMonitor || value.State != wantState || value.Comment != "Maintenance" || value.StartsAt != "2026-01-01T00:00:00Z" || value.EndsAt != "2026-01-01T00:30:00Z" {
						t.Fatalf("native owner/window/state not preserved: %+v", value)
					}
				}
				if !reflect.DeepEqual(ids, test.ids) {
					t.Fatalf("silence IDs = %v, want %v in native order", ids, test.ids)
				}
			}
		})
	}
}
