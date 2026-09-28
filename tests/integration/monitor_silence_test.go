package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestImmediateMonitorSilenceStartsAfterLookup(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	_, token, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	type window struct {
		StartsAt time.Time `json:"startsAt"`
		EndsAt   time.Time `json:"endsAt"`
	}
	received := make(chan window, 1)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value window
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- value
		if _, err := io.WriteString(w, `{"silenceID":"native-id"}`); err != nil {
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body, writer := io.Pipe()
	defer func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/monitors/homepage/silences", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if err := body.Close(); err != nil {
			t.Error(err)
		}
		done <- response
	}()
	// A pipe write completes only when the handler reads it, after authentication.
	if _, err := io.WriteString(writer, "{"); err != nil {
		t.Fatal(err)
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Error(err)
		}
	})
	waitCount := db.Stats().WaitCount
	if _, err := io.WriteString(writer, `"duration_seconds":1,"comment":"Lookup wait"}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for db.Stats().WaitCount == waitCount {
		select {
		case <-ctx.Done():
			t.Fatal("Monitor lookup did not wait for SQLite")
		case <-ticker.C:
		}
	}
	// Keep the lookup waiting longer than the requested one-second silence.
	select {
	case <-time.After(1100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal("request expired while holding SQLite")
	}
	releasedAt := time.Now()
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.Code != http.StatusCreated {
			t.Fatalf("create silence = %d %s", response.Code, response.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("silence creation did not finish")
	}
	select {
	case value := <-received:
		if value.StartsAt.Before(releasedAt) || value.EndsAt.Sub(value.StartsAt) != time.Second {
			t.Fatalf("one-second silence lost lookup time: start %s, end %s, SQLite released %s", value.StartsAt, value.EndsAt, releasedAt)
		}
	default:
		t.Fatal("silence was not sent to Alertmanager")
	}
}

func TestMonitorSilenceCreationBoundaries(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
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
	_, writer, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	var calls atomic.Int32
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/silences" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected native silence request: %s %s", r.Method, r.URL)
		}
		if _, err := io.WriteString(w, `{"silenceID":"4d849af4-fcb5-4af9-937d-b818389dc387"}`); err != nil {
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
	const path = "/api/v1/monitors/homepage/silences"
	const valid = `{"duration_seconds":1800,"comment":"Server maintenance"}`
	for _, test := range []struct {
		name, path, body, token, origin string
		session                         *http.Cookie
		status                          int
	}{
		{name: "session", path: path, body: valid, session: cookie, status: 201},
		{name: "write key", path: path, body: valid, token: writer, status: 201},
		{name: "minimum duration", path: path, body: strings.Replace(valid, "1800", "1", 1), token: writer, status: 201},
		{name: "maximum duration", path: path, body: strings.Replace(valid, "1800", "604800", 1), token: writer, status: 201},
		{name: "null start is immediate", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":null,"comment":`, 1), token: writer, status: 201},
		{name: "past start", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":"2020-01-01T00:00:00Z","comment":`, 1), token: writer, status: 422},
		{name: "zero start", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":"0001-01-01T00:00:00Z","comment":`, 1), token: writer, status: 422},
		{name: "unencodable end", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":"9999-12-31T23:59:59Z","comment":`, 1), token: writer, status: 422},
		{name: "malformed start", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":"tomorrow","comment":`, 1), token: writer, status: 400},
		{name: "start without timezone", path: path, body: strings.Replace(valid, `"comment":`, `"starts_at":"2030-01-01T12:00:00","comment":`, 1), token: writer, status: 400},
		{name: "missing Monitor", path: "/api/v1/monitors/missing/silences", body: valid, token: writer, status: 404},
		{name: "anonymous", path: path, body: valid, status: 401},
		{name: "read key", path: path, body: valid, token: reader, status: 403},
		{name: "Agent key", path: path, body: valid, token: agentKey, status: 401},
		{name: "invalid bearer overrides session", path: path, body: valid, token: "invalid", session: cookie, status: 401},
		{name: "foreign origin", path: path, body: valid, session: cookie, origin: "https://foreign.example", status: 403},
		{name: "zero duration", path: path, body: strings.Replace(valid, "1800", "0", 1), token: writer, status: 422},
		{name: "negative duration", path: path, body: strings.Replace(valid, "1800", "-1", 1), token: writer, status: 422},
		{name: "excessive duration", path: path, body: strings.Replace(valid, "1800", "604801", 1), token: writer, status: 422},
		{name: "duration overflow", path: path, body: strings.Replace(valid, "1800", "9223372036854775807", 1), token: writer, status: 422},
		{name: "fractional duration", path: path, body: strings.Replace(valid, "1800", "1.5", 1), token: writer, status: 400},
		{name: "empty comment", path: path, body: strings.Replace(valid, "Server maintenance", "  ", 1), token: writer, status: 422},
		{name: "long comment", path: path, body: strings.Replace(valid, "Server maintenance", strings.Repeat("x", 1025), 1), token: writer, status: 422},
		{name: "NUL comment", path: path, body: strings.Replace(valid, "Server maintenance", `maintenance\u0000`, 1), token: writer, status: 422},
		{name: "caller-owned matchers", path: path, body: strings.Replace(valid, `"comment":`, `"matchers":[],"comment":`, 1), token: writer, status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := calls.Load()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://arveld.example"+test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
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
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("create silence = %d %s, want uncached %d", response.Code, response.Body.String(), test.status)
			}
			if test.status == http.StatusCreated {
				if calls.Load() != before+1 || strings.TrimSpace(response.Body.String()) != `{"id":"4d849af4-fcb5-4af9-937d-b818389dc387","monitor_id":"homepage"}` {
					t.Fatalf("unexpected silence receipt: %s", response.Body.String())
				}
			} else if calls.Load() != before || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatal("rejected request reached Alertmanager or exposed details")
			}
		})
	}
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"engine failure", "sensitive upstream details", 500},
		{"redirect", "", 307},
		{"invalid JSON", "sensitive upstream details", 200},
		{"missing identity", `{}`, 200},
		{"empty identity", `{"silenceID":" "}`, 200},
		{"oversized response", strings.Repeat("x", 4097), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := calls.Load()
			broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", engine.URL)
				w.WriteHeader(test.status)
				if _, err := io.WriteString(w, test.body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(broken.Close)
			deps.Silences, err = notification.NewSilenceClient(broken.URL)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(valid))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps).ServeHTTP(response, request)
			if response.Code != http.StatusBadGateway || response.Body.String() != "Bad Gateway\n" || response.Header().Get("Cache-Control") != "no-store" || calls.Load() != before+1 {
				t.Fatalf("engine failure must return generic 502 without retry/redirect, got %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMonitorSilenceReadBoundaries(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
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
	agentKey := createAgentKey(t, db)
	var calls atomic.Int32
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/silences" || (r.URL.RawQuery != "" && r.URL.Query().Get("filter") != `arveld_monitor_id="homepage"`) {
			t.Errorf("unexpected native silence read: %s %s", r.Method, r.URL)
		}
		if _, err := io.WriteString(w, `[]`); err != nil {
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
	const path = "/api/v1/monitors/homepage/silences"
	for _, test := range []struct {
		name, path, token, origin string
		session                   *http.Cookie
		status                    int
	}{
		{name: "session", path: path, session: cookie, status: 200},
		{name: "read key", path: path, token: reader, status: 200},
		{name: "missing Monitor", path: "/api/v1/monitors/missing/silences", token: reader, status: 404},
		{name: "anonymous", path: path, status: 401},
		{name: "Agent key", path: path, token: agentKey, status: 401},
		{name: "invalid bearer overrides session", path: path, token: "invalid", session: cookie, status: 401},
		{name: "authenticated safe method", path: path, session: cookie, origin: "https://foreign.example", status: 200},
		{name: "global session", path: "/api/v1/silences", session: cookie, status: 200},
		{name: "global read key", path: "/api/v1/silences", token: reader, status: 200},
		{name: "global anonymous", path: "/api/v1/silences", status: 401},
		{name: "global Agent key", path: "/api/v1/silences", token: agentKey, status: 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				before := calls.Load()
				request := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+test.path, nil)
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
				want := "{\"silences\":[]}\n"
				if method == http.MethodHead {
					want = ""
				}
				if calls.Load() != before+1 || response.Body.String() != want || response.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("unexpected silence read: %s", response.Body.String())
				}
			}
		})
	}
	const valid = `[{"id":"native-id","matchers":[{"name":"arveld_monitor_id","value":"homepage","isRegex":false,"isEqual":true}],"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T00:30:00Z","createdBy":"Arveld","comment":"Maintenance","status":{"state":"active"}}]`
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"native state beats controller clock", valid, `"state":"active"`, 200},
		{"different Monitor", strings.Replace(valid, `"value":"homepage"`, `"value":"other"`, 1), `"silences":[]`, 200},
		{"empty Monitor", strings.Replace(valid, `"value":"homepage"`, `"value":""`, 1), `"silences":[]`, 200},
		{"rule scope", strings.Replace(valid, `"matchers":[`, `"matchers":[{"name":"arveld_rule_id","value":"rule","isRegex":false,"isEqual":true},`, 1), `"silences":[]`, 200},
		{"regex scope", strings.Replace(valid, `"isRegex":false`, `"isRegex":true`, 1), `"silences":[]`, 200},
		{"negative scope", strings.Replace(valid, `"isEqual":true`, `"isEqual":false`, 1), `"silences":[]`, 200},
		{"engine failure", "private engine details", "", 503},
		{"redirect", "", "", 307},
		{"invalid JSON", "private engine details", "", 200},
		{"null list", "null", "", 200},
		{"partial list", `[{"id":"native-id"}]`, "", 200},
		{"missing date", strings.Replace(valid, `"startsAt":"2026-01-01T00:00:00Z",`, "", 1), "", 200},
		{"unknown state", strings.Replace(valid, `"state":"active"`, `"state":"unknown"`, 1), "", 200},
		{"missing state", strings.Replace(valid, `"state":"active"`, "", 1), "", 200},
		{"oversized response", strings.Repeat("x", 4*1024*1024+1), "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := calls.Load()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", engine.URL)
				w.WriteHeader(test.status)
				if _, err := io.WriteString(w, test.body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(upstream.Close)
			deps.Silences, err = notification.NewSilenceClient(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{path, "/api/v1/silences"} {
				want := test.want
				if scope == "/api/v1/silences" && test.name == "different Monitor" {
					want = `"monitor_id":"other"`
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					request := httptest.NewRequestWithContext(t.Context(), method, scope, nil)
					request.Header.Set("Authorization", "Bearer "+reader)
					response := httptest.NewRecorder()
					httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps).ServeHTTP(response, request)
					if want == "" {
						if response.Code != http.StatusBadGateway || response.Body.String() != "Bad Gateway\n" {
							t.Fatalf("invalid engine response must return generic 502, got %d %s", response.Code, response.Body.String())
						}
					} else if response.Code != http.StatusOK || (method == http.MethodGet && !strings.Contains(response.Body.String(), want)) || (method == http.MethodHead && response.Body.Len() != 0) {
						t.Fatalf("unexpected native silence projection: %d %s", response.Code, response.Body.String())
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("native state must not be cached")
					}
				}
			}
			if calls.Load() != before+4 {
				t.Fatal("unexpected retry or redirect")
			}
		})
	}
}

func TestMonitorSilenceCancellationBoundaries(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
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
	_, writer, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	const id = "4d849af4-fcb5-4af9-937d-b818389dc387"
	const path = "/api/v1/monitors/homepage/silences/" + id
	const valid = `[{"id":"` + id + `","matchers":[{"name":"arveld_monitor_id","value":"homepage","isRegex":false,"isEqual":true}],"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T00:30:00Z","status":{"state":"active"}}]`
	for _, test := range []struct {
		name, path, token, origin, list, body string
		session                               *http.Cookie
		upstreamStatus, want, reads, deletes  int
	}{
		{name: "write key", token: writer, list: valid, upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "session", session: cookie, list: valid, upstreamStatus: 200, want: 204, reads: 1, deletes: 1},
		{name: "anonymous", want: 401},
		{name: "read key", token: reader, want: 403},
		{name: "Agent key", token: agentKey, want: 401},
		{name: "invalid bearer overrides session", token: "invalid", session: cookie, want: 401},
		{name: "foreign origin", session: cookie, origin: "https://foreign.example", want: 403},
		{name: "missing Monitor", path: "/api/v1/monitors/missing/silences/" + id, token: writer, want: 404},
		{name: "missing silence", token: writer, list: `[]`, want: 404, reads: 1},
		{name: "different silence", token: writer, list: strings.Replace(valid, id, "another-id", 1), want: 404, reads: 1},
		{name: "different Monitor", token: writer, list: strings.Replace(valid, `"value":"homepage"`, `"value":"other"`, 1), want: 404, reads: 1},
		{name: "regex scope", token: writer, list: strings.Replace(valid, `"isRegex":false`, `"isRegex":true`, 1), want: 404, reads: 1},
		{name: "rule scope", token: writer, list: strings.Replace(valid, `"matchers":[`, `"matchers":[{"name":"arveld_rule_id","value":"rule","isRegex":false,"isEqual":true},`, 1), want: 404, reads: 1},
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
					if r.URL.Path != "/api/v2/silences" || r.URL.Query().Get("filter") != `arveld_monitor_id="homepage"` {
						t.Errorf("unexpected native read: %s", r.URL)
					}
					if _, err := io.WriteString(w, test.list); err != nil {
						t.Error(err)
					}
				case http.MethodDelete:
					deletes.Add(1)
					if r.URL.Path != "/api/v2/silence/"+id {
						t.Errorf("wrong silence cancelled: %s", r.URL)
					}
					w.Header().Set("Location", "/unexpected")
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
			requestPath := test.path
			if requestPath == "" {
				requestPath = path
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "https://arveld.example"+requestPath, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.session != nil {
				request.AddCookie(test.session)
			}
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps).ServeHTTP(response, request)
			body := ""
			if test.want != http.StatusNoContent {
				body = http.StatusText(test.want) + "\n"
			}
			if response.Code != test.want || response.Body.String() != body || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("cancel = %d %s, want uncached %d %q", response.Code, response.Body.String(), test.want, body)
			}
			if int(reads.Load()) != test.reads || int(deletes.Load()) != test.deletes {
				t.Fatalf("native calls = %d reads/%d deletes, want %d/%d", reads.Load(), deletes.Load(), test.reads, test.deletes)
			}
		})
	}
}

func TestControllerManagesNativeMonitorSilences(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable path to run native silences")
	}
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	initial, err := notification.NewStore(db).AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(filepath.Dir(config.DatabasePath), initial); err != nil {
		t.Fatal(err)
	}
	config.alertmanagerURL = startNotificationAlertmanager(t, filepath.Dir(config.DatabasePath))
	address, stop := startController(t, config)
	cookie := loginController(t, address)
	owner := createControllerHTTPMonitor(t, address, cookie, uid)
	type listedSilence struct {
		ID        string    `json:"id"`
		MonitorID string    `json:"monitor_id"`
		StartsAt  time.Time `json:"starts_at"`
		EndsAt    time.Time `json:"ends_at"`
		CreatedBy string    `json:"created_by"`
		Comment   string    `json:"comment"`
		State     string    `json:"state"`
	}
	read := func() []listedSilence {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address+"/api/v1/monitors/"+owner.ID+"/silences", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		var result struct {
			Silences []listedSilence `json:"silences"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&result)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || decodeErr != nil || closeErr != nil || result.Silences == nil {
			t.Fatalf("list Monitor silences = HTTP %d, %+v, %v, %v", response.StatusCode, result, decodeErr, closeErr)
		}
		return result.Silences
	}
	if silences := read(); len(silences) != 0 {
		t.Fatalf("expected an empty native collection, got %+v", silences)
	}
	before := time.Now().Truncate(time.Millisecond)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		address+"/api/v1/monitors/"+owner.ID+"/silences",
		strings.NewReader(`{"duration_seconds":1800,"comment":"  Server maintenance  "}`))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	response := doControllerRequest(t, request)
	var created struct {
		ID        string `json:"id"`
		MonitorID string `json:"monitor_id"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&created)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || created.ID == "" || created.MonitorID != owner.ID {
		t.Fatalf("create Monitor silence = HTTP %d, %+v, %v, %v", response.StatusCode, created, decodeErr, closeErr)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("silence creation must not be cached")
	}
	// Native reads provide independent evidence for the product response.
	native := func(method, path, body string, result any) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, config.alertmanagerURL+path, strings.NewReader(body))
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
	native(http.MethodGet, "/api/v2/silence/"+created.ID, "", &saved)
	if len(saved.Matchers) != 1 || saved.Matchers[0].Name != "arveld_monitor_id" || saved.Matchers[0].Value != owner.ID || saved.Matchers[0].IsRegex || !saved.Matchers[0].IsEqual {
		t.Fatalf("silence must match only this Monitor, regardless of its rules: %+v", saved)
	}
	if saved.StartsAt.Before(before) || saved.StartsAt.After(time.Now()) || saved.EndsAt.Before(before.Add(30*time.Minute)) || saved.EndsAt.After(time.Now().Add(30*time.Minute)) || saved.CreatedBy != "Arveld" || saved.Comment != "Server maintenance" {
		t.Fatalf("unexpected native silence: %+v", saved)
	}
	assertRead := func() {
		t.Helper()
		listed := read()
		if len(listed) != 1 || listed[0].ID != created.ID || listed[0].MonitorID != owner.ID ||
			!listed[0].StartsAt.Equal(saved.StartsAt) || !listed[0].EndsAt.Equal(saved.EndsAt) ||
			listed[0].CreatedBy != "Arveld" || listed[0].Comment != "Server maintenance" || listed[0].State != "active" {
			t.Fatalf("product read differs from the native silence: %+v", listed)
		}
	}
	assertRead()
	stop()
	address, stop = startController(t, config)
	assertRead()
	native(http.MethodPost, "/api/v2/alerts", `[
		{"labels":{"alertname":"Failed","arveld_monitor_id":"`+owner.ID+`"}},
		{"labels":{"alertname":"NoData","arveld_monitor_id":"`+owner.ID+`"}},
		{"labels":{"alertname":"Failed","arveld_monitor_id":"other-monitor"}}
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
		if len(alerts) == 3 {
			for _, alert := range alerts {
				if alert.Labels["arveld_monitor_id"] == owner.ID {
					if alert.Status.State != "suppressed" || len(alert.Status.SilencedBy) != 1 || alert.Status.SilencedBy[0] != created.ID {
						t.Fatalf("Monitor alert was not silenced: %+v", alert)
					}
				} else if alert.Status.State != "active" || len(alert.Status.SilencedBy) != 0 {
					t.Fatalf("another Monitor was silenced: %+v", alert)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native alerts not observed: %+v", alerts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Schedule through Arveld, then read the exact window after a controller restart.
	startsAt := time.Now().Add(time.Hour).Truncate(time.Second)
	scheduledBody, err := json.Marshal(map[string]any{
		"starts_at":        startsAt.In(time.FixedZone("UTC+2", 2*60*60)),
		"duration_seconds": 604800, "comment": "Scheduled maintenance",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err = http.NewRequestWithContext(t.Context(), http.MethodPost,
		address+"/api/v1/monitors/"+owner.ID+"/silences", strings.NewReader(string(scheduledBody)))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	response = doControllerRequest(t, request)
	var scheduled struct {
		ID string `json:"id"`
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&scheduled)
	closeErr = response.Body.Close()
	if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || scheduled.ID == "" {
		t.Fatalf("schedule Monitor silence = HTTP %d, %+v, %v, %v", response.StatusCode, scheduled, decodeErr, closeErr)
	}
	stop()
	address, _ = startController(t, config)
	foundScheduled := false
	for _, value := range read() {
		if value.ID == scheduled.ID {
			foundScheduled = true
			if value.State != "pending" || !value.StartsAt.Equal(startsAt) || !value.EndsAt.Equal(startsAt.Add(7*24*time.Hour)) || value.Comment != "Scheduled maintenance" {
				t.Fatalf("scheduled window was not preserved: %+v", value)
			}
		}
	}
	if !foundScheduled {
		t.Fatal("scheduled silence missing after controller restart")
	}
	// Seed other native states/scopes; the pending silence was created through Arveld.
	wantStates := map[string]string{created.ID: "active", scheduled.ID: "pending"}
	for _, fixture := range []struct {
		monitorID, state string
		ruleOnly         bool
	}{
		{monitorID: owner.ID, state: "expired"},
		{monitorID: "other-monitor", state: "active"},
		{monitorID: owner.ID, state: "active", ruleOnly: true},
	} {
		startsAt := time.Now().UTC()
		matchers := []map[string]any{{"name": "arveld_monitor_id", "value": fixture.monitorID, "isRegex": false, "isEqual": true}}
		if fixture.ruleOnly {
			matchers = append(matchers, map[string]any{"name": "alertname", "value": "Failed", "isRegex": false, "isEqual": true})
		}
		body, err := json.Marshal(map[string]any{
			"matchers": matchers, "startsAt": startsAt, "endsAt": startsAt.Add(time.Hour),
			"createdBy": "Native fixture", "comment": fixture.state,
		})
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			ID string `json:"silenceID"`
		}
		native(http.MethodPost, "/api/v2/silences", string(body), &value)
		if fixture.state == "expired" {
			native(http.MethodDelete, "/api/v2/silence/"+value.ID, "", nil)
		}
		if fixture.monitorID == owner.ID && !fixture.ruleOnly {
			wantStates[value.ID] = fixture.state
		}
	}
	listed := read()
	if len(listed) != len(wantStates) {
		t.Fatalf("expected only whole-Monitor silences, got %+v", listed)
	}
	for _, value := range listed {
		if value.MonitorID != owner.ID || value.State != wantStates[value.ID] {
			t.Fatalf("native scope/state was not preserved: %+v", value)
		}
	}
	for id := range wantStates {
		for range 2 {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete,
				address+"/api/v1/monitors/"+owner.ID+"/silences/"+id, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(cookie)
			response := doControllerRequest(t, request)
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != http.StatusNoContent || len(body) != 0 || readErr != nil || closeErr != nil || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("cancel Monitor silence = HTTP %d %s, %v, %v", response.StatusCode, body, readErr, closeErr)
			}
		}
	}
	for _, value := range read() {
		if value.State != "expired" {
			t.Fatalf("cancelled silence remains active: %+v", value)
		}
	}
}
