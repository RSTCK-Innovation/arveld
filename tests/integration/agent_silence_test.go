package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAgentSilenceCreationBoundaries(t *testing.T) {
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
	var calls atomic.Int32
	var upstreamStatus atomic.Int32
	upstreamStatus.Store(http.StatusOK)
	var saved struct {
		Matchers []struct {
			Name    string `json:"name"`
			Value   string `json:"value"`
			IsRegex bool   `json:"isRegex"`
			IsEqual bool   `json:"isEqual"`
		} `json:"matchers"`
		StartsAt  time.Time `json:"startsAt"`
		EndsAt    time.Time `json:"endsAt"`
		CreatedBy string    `json:"createdBy"`
		Comment   string    `json:"comment"`
	}
	// The channel transfers the decoded wire request safely from the HTTP server.
	received := make(chan []byte, 1)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/silences" {
			t.Errorf("unexpected native request: %s %s", r.Method, r.URL)
		}
		var content json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
			t.Error(err)
		}
		select {
		case received <- content:
		default:
			t.Error("unexpected repeated native request")
		}
		w.WriteHeader(int(upstreamStatus.Load()))
		if err := json.NewEncoder(w).Encode(map[string]string{"silenceID": "native-agent-silence"}); err != nil {
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
	const valid = `{"duration_seconds":1800,"comment":"  Machine maintenance  "}`
	future := time.Now().Add(time.Hour).Truncate(time.Second)
	for _, test := range []struct {
		name, owner, body, token string
		session                  *http.Cookie
		status                   int
	}{
		{name: "session immediate", owner: uid.String(), body: valid, session: cookie, status: 201},
		{name: "canonical identity", owner: strings.ToUpper(uid.String()), body: valid, token: writer, status: 201},
		{name: "scheduled", owner: uid.String(), body: strings.Replace(valid, `"comment":`, `"starts_at":"`+future.Format(time.RFC3339)+`","comment":`, 1), token: writer, status: 201},
		{name: "unknown Agent", owner: (agent.InstanceUID{2}).String(), body: valid, token: writer, status: 404},
		{name: "invalid identity", owner: "invalid", body: valid, token: writer, status: 404},
		{name: "anonymous", owner: uid.String(), body: valid, status: 401},
		{name: "read key", owner: uid.String(), body: valid, token: reader, status: 403},
		{name: "Agent key", owner: uid.String(), body: valid, token: createAgentKey(t, db), status: 401},
		{name: "invalid duration", owner: uid.String(), body: strings.Replace(valid, "1800", "0", 1), token: writer, status: 422},
		{name: "past start", owner: uid.String(), body: strings.Replace(valid, `"comment":`, `"starts_at":"2020-01-01T00:00:00Z","comment":`, 1), token: writer, status: 422},
		{name: "caller-owned matcher", owner: uid.String(), body: strings.Replace(valid, `"comment":`, `"matchers":[],"comment":`, 1), token: writer, status: 400},
		{name: "engine failure", owner: uid.String(), body: valid, token: writer, status: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.status == http.StatusBadGateway {
				upstreamStatus.Store(http.StatusServiceUnavailable)
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://arveld.example/api/v1/agents/"+test.owner+"/silences", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.session != nil {
				request.AddCookie(test.session)
			}
			beforeCalls, before := calls.Load(), time.Now()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("create Agent silence = %d %s, want uncached %d", response.Code, response.Body.String(), test.status)
			}
			if test.status != http.StatusCreated && test.status != http.StatusBadGateway {
				if calls.Load() != beforeCalls {
					t.Fatal("rejected request reached Alertmanager")
				}
				return
			}
			if calls.Load() != beforeCalls+1 {
				t.Fatal("creation must send exactly one native request, with no automatic retry")
			}
			if err := json.Unmarshal(<-received, &saved); err != nil {
				t.Fatal(err)
			}
			if test.status == http.StatusBadGateway {
				if response.Body.String() != "Bad Gateway\n" {
					t.Fatal("native failure exposed upstream details")
				}
				return
			}
			if strings.TrimSpace(response.Body.String()) != `{"id":"native-agent-silence","agent_instance_uid":"`+uid.String()+`"}` {
				t.Fatalf("unexpected Agent silence receipt: %s", response.Body.String())
			}
			if len(saved.Matchers) != 1 || saved.Matchers[0].Name != "arveld_agent_id" || saved.Matchers[0].Value != uid.String() || saved.Matchers[0].IsRegex || !saved.Matchers[0].IsEqual {
				t.Fatalf("silence must cover only this Agent and its Monitors: %+v", saved.Matchers)
			}
			if saved.EndsAt.Sub(saved.StartsAt) != 30*time.Minute || saved.CreatedBy != "Arveld" || saved.Comment != "Machine maintenance" {
				t.Fatalf("unexpected native window/author/reason: %+v", saved)
			}
			if test.name == "scheduled" {
				if !saved.StartsAt.Equal(future) {
					t.Fatalf("scheduled start = %s, want %s", saved.StartsAt, future)
				}
			} else if saved.StartsAt.Before(before) || saved.StartsAt.After(time.Now()) {
				t.Fatalf("immediate start outside request: %s", saved.StartsAt)
			}
		})
	}
}

func TestAgentSilenceSuppressesNativeAgentAndMonitorAlerts(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable path to run native silences")
	}
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	initial, err := notification.NewStore(db).AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(directory, initial); err != nil {
		t.Fatal(err)
	}
	engineURL := startNotificationAlertmanager(t, directory)
	deps := accountDependencies(db)
	deps.Silences, err = notification.NewSilenceClient(engineURL)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	cookie := login(t, handler, "a long password for testing")
	native := func(method, path, body string, result any) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, engineURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response := doControllerRequest(t, request)
		content, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || closeErr != nil {
			t.Fatalf("native %s %s = %d %s, %v, %v", method, path, response.StatusCode, content, readErr, closeErr)
		}
		if result != nil {
			if err := json.Unmarshal(content, result); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Both windows use the product API; native reads independently verify acceptance.
	future := time.Now().Add(time.Hour).Truncate(time.Second)
	var immediateID string
	var silenceIDs []string
	for _, scheduled := range []bool{false, true} {
		body := `{"duration_seconds":1800,"comment":"Machine maintenance"}`
		if scheduled {
			body = strings.Replace(body, `"comment":`, `"starts_at":"`+future.Format(time.RFC3339)+`","comment":`, 1)
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/agents/"+uid.String()+"/silences", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || response.Code != http.StatusCreated || created.ID == "" {
			t.Fatalf("create Agent silence = %d %s: %v", response.Code, response.Body.String(), err)
		}
		silenceIDs = append(silenceIDs, created.ID)
		var saved struct {
			StartsAt time.Time `json:"startsAt"`
			EndsAt   time.Time `json:"endsAt"`
			Status   struct {
				State string `json:"state"`
			} `json:"status"`
		}
		native(http.MethodGet, "/api/v2/silence/"+created.ID, "", &saved)
		if scheduled {
			if saved.Status.State != "pending" || !saved.StartsAt.Equal(future) || !saved.EndsAt.Equal(future.Add(30*time.Minute)) {
				t.Fatalf("scheduled Agent silence was not preserved: %+v", saved)
			}
		} else {
			if saved.Status.State != "active" {
				t.Fatalf("immediate Agent silence is not active: %+v", saved)
			}
			immediateID = created.ID
		}
		for _, path := range []string{"/api/v1/agents/" + uid.String() + "/silences", "/api/v1/silences"} {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var listed struct {
				Silences []struct {
					ID               string    `json:"id"`
					AgentInstanceUID string    `json:"agent_instance_uid"`
					StartsAt         time.Time `json:"starts_at"`
					EndsAt           time.Time `json:"ends_at"`
					State            string    `json:"state"`
				} `json:"silences"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil || response.Code != http.StatusOK {
				t.Fatalf("read %s = %d %s: %v", path, response.Code, response.Body.String(), err)
			}
			found := false
			for _, value := range listed.Silences {
				if value.ID != created.ID {
					continue
				}
				found = true
				if value.AgentInstanceUID != uid.String() || value.State != saved.Status.State || !value.StartsAt.Equal(saved.StartsAt) || !value.EndsAt.Equal(saved.EndsAt) {
					t.Fatalf("product read differs from the native Agent silence: %+v", value)
				}
			}
			if !found {
				t.Fatalf("created Agent silence missing from %s", path)
			}
		}
	}
	native(http.MethodPost, "/api/v2/alerts", `[
		{"labels":{"alertname":"CPU","arveld_agent_id":"`+uid.String()+`"}},
		{"labels":{"alertname":"Failed","arveld_agent_id":"`+uid.String()+`","arveld_monitor_id":"homepage"}},
		{"labels":{"alertname":"NoData","arveld_agent_id":"`+uid.String()+`","arveld_monitor_id":"dns"}},
		{"labels":{"alertname":"CPU","arveld_agent_id":"other-agent"}},
		{"labels":{"alertname":"Failed","arveld_agent_id":"other-agent","arveld_monitor_id":"other-monitor"}}
	]`, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var alerts []struct {
			Labels map[string]string `json:"labels"`
			Status struct {
				State      string   `json:"state"`
				SilencedBy []string `json:"silencedBy"`
			} `json:"status"`
		}
		native(http.MethodGet, "/api/v2/alerts", "", &alerts)
		if len(alerts) == 5 {
			for _, value := range alerts {
				if value.Labels["arveld_agent_id"] == uid.String() {
					if value.Status.State != "suppressed" || len(value.Status.SilencedBy) != 1 || value.Status.SilencedBy[0] != immediateID {
						t.Fatalf("Agent or assigned Monitor alert was not silenced: %+v", value)
					}
				} else if value.Status.State != "active" || len(value.Status.SilencedBy) != 0 {
					t.Fatalf("another Agent or its Monitor was silenced: %+v", value)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native alerts not observed: %+v", alerts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, id := range silenceIDs {
		// Repeating cancellation also succeeds for an expired entry retained by the engine.
		for range 2 {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/agents/"+uid.String()+"/silences/"+id, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("cancel native Agent silence = %d %s", response.Code, response.Body.String())
			}
		}
		var saved struct {
			Status struct {
				State string `json:"state"`
			} `json:"status"`
		}
		native(http.MethodGet, "/api/v2/silence/"+id, "", &saved)
		if saved.Status.State != "expired" {
			t.Fatalf("cancelled Agent silence is not expired: %+v", saved)
		}
	}
	for _, path := range []string{"/api/v1/agents/" + uid.String() + "/silences", "/api/v1/silences"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var listed struct {
			Silences []notification.Silence `json:"silences"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil || response.Code != http.StatusOK || len(listed.Silences) != 2 {
			t.Fatalf("read cancelled Agent silences = %d %s: %v", response.Code, response.Body.String(), err)
		}
		for _, value := range listed.Silences {
			if value.State != "expired" || value.AgentInstanceUID != uid.String() {
				t.Fatalf("product list did not preserve native expiration: %+v", value)
			}
		}
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		var alerts []struct {
			Status struct {
				State      string   `json:"state"`
				SilencedBy []string `json:"silencedBy"`
			} `json:"status"`
		}
		native(http.MethodGet, "/api/v2/alerts", "", &alerts)
		active := len(alerts) == 5
		for _, value := range alerts {
			active = active && value.Status.State == "active" && len(value.Status.SilencedBy) == 0
		}
		if active {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("alerts remain silenced after cancellation: %+v", alerts)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
