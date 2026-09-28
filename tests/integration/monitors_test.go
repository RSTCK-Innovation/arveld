package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

type httpMonitorResult struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	AgentInstanceUID string `json:"agent_instance_uid"`
	Endpoint         string `json:"endpoint"`
	Method           string `json:"method"`
	IntervalSeconds  int    `json:"interval_seconds"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
}

func TestHTTPMonitorAPIValidatesRequestsAndAccess(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{InstanceUID: agent.InstanceUID{1}}); err != nil {
		t.Fatal(err)
	}
	_, readKey, err := auth.NewStore(db).CreateAPIKey(ctx, auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writeKey, err := auth.NewStore(db).CreateAPIKey(ctx, auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body, token string, session *http.Cookie, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if session != nil {
			request.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	const collection = "/api/v1/monitors/http"
	const valid = `{"name":"Public homepage","agent_instance_uid":"01000000-0000-0000-0000-000000000000","endpoint":"https://example.com","method":"GET","interval_seconds":30,"timeout_seconds":5}`
	// Block writes while testing validation, so accidental persistence produces 500.
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_http_monitor_creation BEFORE INSERT ON monitors
		BEGIN SELECT RAISE(ABORT, 'test-only monitor storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"malformed JSON", `{`, http.StatusBadRequest},
		{"unknown field", strings.Replace(valid, `"name":`, `"id":"chosen","name":`, 1), http.StatusBadRequest},
		{"wrong type", strings.Replace(valid, `"GET"`, `true`, 1), http.StatusBadRequest},
		{"multiple objects", valid + `{}`, http.StatusBadRequest},
		{"oversized JSON", valid + strings.Repeat(" ", 512*1024), http.StatusRequestEntityTooLarge},
		{"invalid UTF-8", strings.Replace(valid, "Public homepage", "\xff", 1), http.StatusBadRequest},
		{"null", `null`, http.StatusUnprocessableEntity},
		{"empty object", `{}`, http.StatusUnprocessableEntity},
		{"empty name", strings.Replace(valid, "Public homepage", "  ", 1), http.StatusUnprocessableEntity},
		{"short name", strings.Replace(valid, "Public homepage", "X", 1), http.StatusUnprocessableEntity},
		{"long name", strings.Replace(valid, "Public homepage", strings.Repeat("é", 81), 1), http.StatusUnprocessableEntity},
		{"NUL name", strings.Replace(valid, "Public homepage", `ab\u0000cd`, 1), http.StatusUnprocessableEntity},
		{"invalid UID", strings.Replace(valid, "01000000-0000-0000-0000-000000000000", "invalid", 1), http.StatusUnprocessableEntity},
		{"missing Agent", strings.Replace(valid, "01000000", "02000000", 1), http.StatusUnprocessableEntity},
		{"relative URL", strings.Replace(valid, "https://example.com", "/health", 1), http.StatusUnprocessableEntity},
		{"unsupported scheme", strings.Replace(valid, "https://example.com", "ftp://example.com", 1), http.StatusUnprocessableEntity},
		{"missing host", strings.Replace(valid, "https://example.com", "https:///health", 1), http.StatusUnprocessableEntity},
		{"URL credentials", strings.Replace(valid, "https://example.com", "https://user:password@example.com", 1), http.StatusUnprocessableEntity},
		{"zero port", strings.Replace(valid, "https://example.com", "https://example.com:0", 1), http.StatusUnprocessableEntity},
		{"large port", strings.Replace(valid, "https://example.com", "https://example.com:65536", 1), http.StatusUnprocessableEntity},
		{"unsupported method", strings.Replace(valid, `"GET"`, `"TRACE"`, 1), http.StatusUnprocessableEntity},
		{"short interval", strings.Replace(valid, `"interval_seconds":30`, `"interval_seconds":9`, 1), http.StatusUnprocessableEntity},
		{"long interval", strings.Replace(valid, `"interval_seconds":30`, `"interval_seconds":3601`, 1), http.StatusUnprocessableEntity},
		{"zero timeout", strings.Replace(valid, `"timeout_seconds":5`, `"timeout_seconds":0`, 1), http.StatusUnprocessableEntity},
		{"long timeout", strings.Replace(valid, `"timeout_seconds":5`, `"timeout_seconds":61`, 1), http.StatusUnprocessableEntity},
		{"timeout exceeds interval", strings.Replace(valid, `"timeout_seconds":5`, `"timeout_seconds":31`, 1), http.StatusUnprocessableEntity},
		{"failed storage", valid, http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(http.MethodPost, collection, test.body, "", cookie, "")
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("rejected creation = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Location") != "" || response.Header().Get("Set-Cookie") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("rejected creation must not publish a Location, replace a cookie or allow caching")
			}
		})
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_http_monitor_creation"); err != nil {
		t.Fatal(err)
	}
	created := request(http.MethodPost, collection, valid, "", cookie, "https://arveld.example")
	if created.Code != http.StatusCreated {
		t.Fatalf("valid session creation = %d %q, want 201", created.Code, created.Body.String())
	}
	var first httpMonitorResult
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	// A write key can create an independent resource, including HEAD probes.
	unicodeName := strings.Repeat("é", 80)
	headBody := strings.Replace(valid, `"GET"`, `"HEAD"`, 1)
	headBody = strings.Replace(headBody, "Public homepage", "  "+unicodeName+"  ", 1)
	created = request(http.MethodPost, collection, headBody, writeKey, nil, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("write-key creation = %d %q, want 201", created.Code, created.Body.String())
	}
	var second httpMonitorResult
	if err := json.Unmarshal(created.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.ID == "" || second.ID == first.ID || second.Method != http.MethodHead || second.Name != unicodeName {
		t.Fatal("each creation must receive a distinct ID, preserve the method and accept an 80-character Unicode name")
	}
	for _, test := range []struct {
		name, token, origin string
		cookie              *http.Cookie
		status              int
	}{
		{"anonymous", "", "", nil, http.StatusUnauthorized},
		{"read-only key", readKey, "", nil, http.StatusForbidden},
		{"cross-origin session", "", "https://another.example", cookie, http.StatusForbidden},
		{"invalid key with valid cookie", "invalid", "", cookie, http.StatusUnauthorized},
		{"agent key", agentKey, "", nil, http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(http.MethodPost, collection, valid, test.token, test.cookie, test.origin)
			if response.Code != test.status || response.Header().Get("Location") != "" {
				t.Fatalf("unauthorized creation = %d, want %d without a Location", response.Code, test.status)
			}
		})
	}
	path := collection + "/" + first.ID
	read := request(http.MethodGet, path, "", readKey, nil, "")
	var got httpMonitorResult
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &got) != nil || got != first {
		t.Fatalf("read-only key must retrieve the first Monitor unchanged: %d %q", read.Code, read.Body.String())
	}
	head := request(http.MethodHead, path, "", readKey, nil, "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("HEAD = %d %q, want 200 with JSON headers and no body", head.Code, head.Body.String())
	}
	if got := request(http.MethodGet, path, "", "", nil, "").Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous read = %d, want 401", got)
	}
	if got := request(http.MethodHead, collection+"/missing", "", readKey, nil, "").Code; got != http.StatusNotFound {
		t.Fatalf("unknown Monitor HEAD = %d, want 404", got)
	}
	// Break only Monitor reads, preserving authentication and error mapping.
	if _, err := db.ExecContext(ctx, "DROP TABLE monitors"); err != nil {
		t.Fatal(err)
	}
	failed := request(http.MethodGet, path, "", readKey, nil, "")
	if failed.Code != http.StatusInternalServerError || failed.Body.String() != "Internal Server Error\n" {
		t.Fatalf("failed Monitor read = %d %q, want generic 500", failed.Code, failed.Body.String())
	}
}

func TestControllerCreatesAndReadsHTTPMonitorAfterRestart(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	url, stop := startController(t, config)
	cookie := loginController(t, url)
	request := func(method, path, body string) *http.Response {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, url+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		t.Cleanup(func() {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		})
		return response
	}
	const body = `{
		"name":"  Public homepage  ",
		"agent_instance_uid":"01000000-0000-0000-0000-000000000000",
		"endpoint":"https://example.com/health?value=$ready",
		"method":"GET","interval_seconds":30,"timeout_seconds":5
	}`
	created := request(http.MethodPost, "/api/v1/monitors/http", body)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create HTTP Monitor = %d, want 201", created.StatusCode)
	}
	var result httpMonitorResult
	decoder := json.NewDecoder(created.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || strings.ContainsAny(result.ID, "/: ") ||
		result.Name != "Public homepage" || result.AgentInstanceUID != uid.String() ||
		result.Endpoint != "https://example.com/health?value=$ready" ||
		result.Method != http.MethodGet || result.IntervalSeconds != 30 || result.TimeoutSeconds != 5 {
		t.Fatalf("created HTTP Monitor = %+v, want the submitted definition and a server-generated ID", result)
	}
	location := "/api/v1/monitors/http/" + result.ID
	if created.Header.Get("Location") != location || created.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("creation must return the resource Location and prevent caching")
	}
	if err := created.Body.Close(); err != nil {
		t.Fatal(err)
	}
	stop()
	url, _ = startController(t, config)
	read := request(http.MethodGet, location, "")
	if read.StatusCode != http.StatusOK || read.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("read HTTP Monitor after restart = %d, want uncached 200", read.StatusCode)
	}
	var got httpMonitorResult
	if err := json.NewDecoder(read.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != result {
		t.Fatalf("HTTP Monitor after restart = %+v, want %+v", got, result)
	}
	if err := read.Body.Close(); err != nil {
		t.Fatal(err)
	}
	head := request(http.MethodHead, location, "")
	content, err := io.ReadAll(head.Body)
	if err != nil || head.StatusCode != http.StatusOK || len(content) != 0 {
		t.Fatalf("HEAD HTTP Monitor = %d %q, %v; want empty 200", head.StatusCode, content, err)
	}
	if err := head.Body.Close(); err != nil {
		t.Fatal(err)
	}
	missing := request(http.MethodGet, "/api/v1/monitors/http/missing", "")
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown HTTP Monitor = %d, want 404", missing.StatusCode)
	}
	if err := missing.Body.Close(); err != nil {
		t.Fatal(err)
	}
}
