package integration

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerAuthorizesAPIKeysByMethod(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	configPath := "/api/v1/agents/" + uid.String() + "/config"
	tokens := make(map[string]string)
	for _, permission := range []string{"read", "write"} {
		body := `{"name":"Automation","permission":"` + permission + `","expiresDays":null}`
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s key status = %d, want 201", permission, response.Code)
		}
		var result struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		tokens[permission] = result.Token
	}
	send := func(method, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/x-yaml")
		request.Header.Set("Authorization", "Bearer "+token)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	check := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("API key response = %d %q, want %d", response.Code, response.Body.String(), want)
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
			t.Error("API key requests must not be cached or issue a session cookie")
		}
		if want == http.StatusForbidden && response.Body.String() != "Forbidden\n" {
			t.Fatal("read-only keys must receive a generic 403 for non-read methods")
		}
	}
	for _, body := range []string{"receivers: {}", "receivers:\n  otlp: {}"} {
		check(send(http.MethodPut, configPath, body, tokens["write"], nil), http.StatusOK)
	}
	for _, token := range tokens {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			check(send(method, "/api/v1/agents", "", token, nil), http.StatusOK)
		}
	}
	for _, session := range []*http.Cookie{nil, cookie} {
		check(send(http.MethodPut, configPath, "receivers: {forbidden: {}}", tokens["read"], session), http.StatusForbidden)
		check(send(http.MethodPost, configPath+"/rollback", "", tokens["read"], session), http.StatusForbidden)
		for _, route := range []struct {
			path, allow string
			status      int
		}{
			{"/api/v1/agents", "GET, HEAD", http.StatusMethodNotAllowed},
			{"/api/v1/%61gents", "", http.StatusNotFound},
		} {
			for _, method := range []string{http.MethodPatch, http.MethodDelete, http.MethodOptions} {
				response := send(method, route.path, "", tokens["read"], session)
				check(response, http.StatusForbidden)
				if response.Header().Get("Allow") != "" {
					t.Error("permission rejection leaked method fallback headers")
				}
				// A write key reaches the router; encoded literals do not alias routes.
				response = send(method, route.path, "", tokens["write"], session)
				check(response, route.status)
				allowed := response.Header().Values("Allow")
				slices.Sort(allowed)
				if strings.Join(allowed, ", ") != route.allow {
					t.Error("authorized method fallback must advertise the available methods")
				}
			}
		}
	}
	response := send(http.MethodGet, configPath+"/status", "", tokens["read"], nil)
	check(response, http.StatusOK)
	if !strings.Contains(response.Body.String(), `"desired":{"revision":2,`) {
		t.Fatal("rejected writes must leave the desired configuration unchanged")
	}
	response = send(http.MethodPost, configPath+"/rollback", "", tokens["write"], nil)
	check(response, http.StatusOK)
	if response.Body.String() != "{\"revision\":1}\n" {
		t.Fatal("a write key must be able to roll back the configuration")
	}
	response = send(http.MethodGet, configPath+"/status", "", tokens["read"], nil)
	check(response, http.StatusOK)
	if !strings.Contains(response.Body.String(), `"desired":{"revision":1,`) {
		t.Fatal("the rollback must be visible through the read API")
	}
}

func TestHTTPServerAuthenticatesManagementReadsWithAPIKeys(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	send := func(method, path, body string, authorization []string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(path, "/api/v1/metrics/") {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if authorization != nil {
			request.Header["Authorization"] = authorization
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	check := func(t *testing.T, response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("management response = %d %q, want %d", response.Code, response.Body.String(), want)
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
			t.Error("management authentication must not be cached or issue a session cookie")
		}
		if want == http.StatusUnauthorized || want == http.StatusForbidden || want == http.StatusInternalServerError {
			if response.Body.String() != http.StatusText(want)+"\n" {
				t.Fatal("authentication failures must return generic errors")
			}
			if response.Header().Get("Allow") != "" {
				t.Error("authentication failure leaked method fallback headers")
			}
		}
	}
	var credentials []struct {
		Key   auth.APIKey `json:"key"`
		Token string      `json:"token"`
	}
	for _, body := range []string{
		`{"name":"Temporary access","permission":"read","expiresDays":1}`,
		`{"name":"Automation","permission":"write","expiresDays":null}`,
	} {
		response := send(http.MethodPost, "/api/v1/apikeys", body, nil, cookie)
		check(t, response, http.StatusCreated)
		var credential struct {
			Key   auth.APIKey `json:"key"`
			Token string      `json:"token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &credential); err != nil {
			t.Fatalf("decode created API key: %v", err)
		}
		credentials = append(credentials, credential)
	}
	readHeader := []string{"Bearer " + credentials[0].Token}
	writeHeader := []string{"Bearer " + credentials[1].Token}
	response := send(http.MethodGet, "/api/v1/agents", "", readHeader, nil)
	check(t, response, http.StatusOK)
	if response.Body.String() != "{\"agents\":[]}\n" {
		t.Fatal("API key authentication must reach the real agent inventory handler")
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", writeHeader, nil), http.StatusOK)
	check(t, send(http.MethodGet, "/api/v1/agents", "", nil, cookie), http.StatusOK)
	check(t, send(http.MethodGet, "/api/v1/agents", "", nil, nil), http.StatusUnauthorized)
	check(t, send(http.MethodGet, "/api/v1/agents", "", []string{"bEaReR " + credentials[0].Token}, nil), http.StatusOK)
	invalidCookie := &http.Cookie{Name: cookie.Name, Value: "unknown-session"} //nolint:gosec // Request cookie fixture; security attributes belong to Set-Cookie responses.
	check(t, send(http.MethodGet, "/api/v1/agents", "", readHeader, invalidCookie), http.StatusOK)

	// Explicit bearer credentials must not load an unrelated browser session.
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET data = ?", []byte("{")); err != nil {
		t.Fatalf("make browser session unreadable: %v", err)
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", readHeader, cookie), http.StatusOK)
	check(t, send(http.MethodGet, "/api/v1/auth/session", "", nil, cookie), http.StatusInternalServerError)
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET data = ?", []byte(`{"administrator_id":1}`)); err != nil {
		t.Fatalf("restore browser session: %v", err)
	}

	const configPath = "/api/v1/agents/0198f1ad-6f2a-7a4b-8c3d-123456789abc/config"
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{http.MethodHead, "/api/v1/agents", http.StatusOK},
		{http.MethodGet, "/api/v1/metrics/query", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/metrics/query_range", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/metrics/query", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/metrics/query_range", http.StatusBadRequest},
		{http.MethodGet, configPath + "/status", http.StatusNotFound},
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound},
	} {
		check(t, send(route.method, route.path, "", readHeader, nil), route.status)
	}
	for _, test := range []struct {
		name          string
		authorization []string
	}{
		{"empty header", []string{""}},
		{"missing token", []string{"Bearer"}},
		{"wrong scheme", []string{"Basic " + credentials[0].Token}},
		{"unknown token", []string{"Bearer arv_unknown_secret"}},
		{"wrong secret", []string{"Bearer " + credentials[0].Token + "changed"}},
		{"public prefix only", []string{"Bearer " + credentials[0].Key.Prefix}},
		{"collector token", []string{"Bearer secret-token"}},
		{"extra field", []string{readHeader[0] + " extra"}},
		{"duplicate headers", []string{readHeader[0], writeHeader[0]}},
		{"combined headers", []string{readHeader[0] + ", " + writeHeader[0]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// An explicit credential never falls back to a browser session.
			for _, session := range []*http.Cookie{nil, cookie} {
				response := send(http.MethodGet, "/api/v1/agents", "", test.authorization, session)
				check(t, response, http.StatusUnauthorized)
				if response.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Error("rejected bearer credentials must advertise the authentication scheme")
				}
			}
		})
	}
	// These requests exercise OTLP admission; controller tests cover forwarding.
	deps := accountDependencies(db)
	deps.OpAMP = http.NotFoundHandler()
	deps.OTLPMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server = httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	for _, authorization := range [][]string{readHeader, writeHeader} {
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/apikeys"},
			{http.MethodPost, "/api/v1/apikeys"},
			{http.MethodDelete, "/api/v1/apikeys/" + credentials[0].Key.ID},
			{http.MethodPost, "/api/v1/agentkeys"},
			{http.MethodGet, "/api/v1/agentkeys"},
			{http.MethodHead, "/api/v1/agentkeys"},
			{http.MethodDelete, "/api/v1/agentkeys/unknown"},
			{http.MethodPatch, "/api/v1/%61gentkeys"},
			{http.MethodGet, "/api/v1/agentkeys/unknown"},
			{http.MethodGet, "/api/v1/account%2Fprofile"},
			{http.MethodGet, "/api/v1/apikeys%2Funknown"},
			{http.MethodPut, "/api/v1/account/profile"},
			{http.MethodPut, "/api/v1/account/password"},
			{http.MethodPut, "/api/v1/%61ccount/profile"},
			{http.MethodGet, "/api/v1/%61ccount/unknown"},
			{http.MethodPatch, "/api/v1/%61pikeys"},
			{http.MethodGet, "/api/v1/auth/session"},
		} {
			check(t, send(route.method, route.path, "{}", authorization, nil), http.StatusUnauthorized)
		}
		response := send(http.MethodPost, "/v1/otlp/v1/metrics", "", authorization, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatal("a management API key must not authenticate collector ingestion")
		}
	}

	// Move expiration into the past without waiting for the one-day validity period.
	now := time.Now()
	if _, err := db.ExecContext(ctx, "UPDATE api_keys SET created_at_ns = ?, expires_at_ns = ? WHERE id = ?",
		now.Add(-2*time.Hour).UnixNano(), now.Add(-time.Hour).UnixNano(), credentials[0].Key.ID); err != nil {
		t.Fatal(err)
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", readHeader, cookie), http.StatusUnauthorized)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	check(t, send(http.MethodGet, "/api/v1/agents", "", readHeader, nil), http.StatusUnauthorized)
	check(t, send(http.MethodGet, "/api/v1/agents", "", writeHeader, nil), http.StatusOK)
	check(t, send(http.MethodDelete, "/api/v1/apikeys/"+credentials[1].Key.ID, "", nil, cookie), http.StatusNoContent)
	check(t, send(http.MethodGet, "/api/v1/agents", "", writeHeader, cookie), http.StatusUnauthorized)

	// Keep one active key to exercise the storage boundaries independently.
	response = send(http.MethodPost, "/api/v1/apikeys",
		`{"name":"Diagnostics","permission":"read","expiresDays":null}`, nil, cookie)
	check(t, response, http.StatusCreated)
	var diagnostic struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &diagnostic); err != nil {
		t.Fatal(err)
	}
	diagnosticHeader := []string{"Bearer " + diagnostic.Token}
	if _, err := db.ExecContext(ctx, "DROP TABLE sessions"); err != nil {
		t.Fatal(err)
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", diagnosticHeader, cookie), http.StatusOK)
	if _, err := db.ExecContext(ctx, "DELETE FROM users"); err != nil {
		t.Fatal(err)
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", diagnosticHeader, nil), http.StatusUnauthorized)
	if _, err := db.ExecContext(ctx, "DROP TABLE api_keys"); err != nil {
		t.Fatal(err)
	}
	check(t, send(http.MethodGet, "/api/v1/agents", "", diagnosticHeader, nil), http.StatusInternalServerError)
}

func TestHTTPServerRequiresAdministratorForManagement(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	server := newAccountHandler(db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	configPath := "/api/v1/agents/" + uid.String() + "/config"
	send := func(method, path, body string, cookie *http.Cookie, headers http.Header) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/x-yaml")
		maps.Copy(request.Header, headers)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	check := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("management response = %d %q, want %d", response.Code, response.Body.String(), want)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Error("management responses must not be cached")
		}
		if response.Header().Get("Set-Cookie") != "" {
			t.Error("management requests must not issue or renew a session cookie")
		}
	}
	checkDenied := func(cookie *http.Cookie, headers http.Header, want int) {
		t.Helper()
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/agents"},
			{http.MethodHead, "/api/v1/agents"},
			{http.MethodGet, "/api/v1/metrics/query"},
			{http.MethodGet, "/api/v1/metrics/query_range"},
			{http.MethodPost, "/api/v1/metrics/query"},
			{http.MethodPost, "/api/v1/metrics/query_range"},
			{http.MethodPut, configPath},
			{http.MethodPost, configPath + "/rollback"},
			{http.MethodGet, configPath + "/status"},
			{http.MethodGet, "/api/v1/unknown"},
		} {
			check(send(route.method, route.path, "receivers: {}", cookie, headers), want)
		}
	}

	checkDenied(nil, nil, http.StatusUnauthorized)
	checkDenied(nil, http.Header{"Authorization": {"Bearer secret-token"}}, http.StatusUnauthorized)
	checkDenied(sessionCookie(t, db, 1, time.Hour), nil, http.StatusUnauthorized)
	for path, want := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusServiceUnavailable, "/api/v1/auth/setup": http.StatusOK} {
		if response := send(http.MethodGet, path, "", nil, nil); response.Code != want {
			t.Fatalf("public %s status = %d, want %d", path, response.Code, want)
		}
	}

	jsonHeaders := http.Header{"Content-Type": {"application/json"}}
	response := send(http.MethodPost, "/api/v1/auth/setup",
		`{"name":"Camille","email":"camille@example.com","password":"a long password for testing"}`, nil, jsonHeaders)
	if response.Code != http.StatusCreated {
		t.Fatalf("public setup status = %d, want 201", response.Code)
	}
	cookie := loginCookie(t, send(http.MethodPost, "/api/v1/auth/login",
		`{"email":"camille@example.com","password":"a long password for testing"}`, nil, jsonHeaders))
	checkDenied(&http.Cookie{Name: cookie.Name, Value: "unknown-token"}, nil, http.StatusUnauthorized) //nolint:gosec // Request cookie fixture; security attributes belong to Set-Cookie responses.
	checkDenied(sessionCookie(t, db, 1, -time.Hour), nil, http.StatusUnauthorized)
	checkDenied(sessionCookie(t, db, 0, time.Hour), nil, http.StatusUnauthorized)
	checkDenied(sessionCookie(t, db, 2, time.Hour), nil, http.StatusUnauthorized)
	if _, err := remoteconfig.NewStore(db).Desired(ctx, uid); !errors.Is(err, remoteconfig.ErrNoDesiredRevision) {
		t.Fatalf("unauthenticated requests changed agent configuration: %v", err)
	}

	check(send(http.MethodGet, "/api/v1/agents", "", cookie, nil), http.StatusOK)
	// Missing query parameters reach validation only after authentication.
	check(send(http.MethodGet, "/api/v1/metrics/query", "", cookie, nil), http.StatusBadRequest)
	check(send(http.MethodGet, "/api/v1/metrics/query_range", "", cookie, nil), http.StatusBadRequest)
	check(send(http.MethodGet, "/api/v1/unknown", "", cookie, nil), http.StatusNotFound)
	check(send(http.MethodPut, configPath, "receivers: {}", cookie, nil), http.StatusOK)
	sameOrigin := http.Header{"Origin": {"https://arveld.example"}}
	encodedConfigPath := "/api/v1/%61gents/%30" + uid.String()[1:] + "/%63onfig"
	check(send(http.MethodPut, encodedConfigPath, "receivers:\n  otlp: {}", cookie, sameOrigin), http.StatusNotFound)
	check(send(http.MethodPut, configPath, "receivers:\n  otlp: {}", cookie, sameOrigin), http.StatusOK)
	for _, headers := range []http.Header{
		{"Origin": {"https://another.example"}},
		{"Origin": {"null"}},
		{"Sec-Fetch-Site": {"cross-site"}},
		{"Sec-Fetch-Site": {"same-site"}},
	} {
		check(send(http.MethodPut, configPath, "receivers: {forbidden: {}}", cookie, headers), http.StatusForbidden)
		check(send(http.MethodPost, configPath+"/rollback", "", cookie, headers), http.StatusForbidden)
	}
	response = send(http.MethodGet, configPath+"/status", "", cookie, nil)
	check(response, http.StatusOK)
	if !strings.Contains(response.Body.String(), `"desired":{"revision":2,`) {
		t.Fatalf("cross-origin requests changed configuration: %s", response.Body.String())
	}
	response = send(http.MethodPost, configPath+"/rollback", "", cookie, sameOrigin)
	check(response, http.StatusOK)
	if response.Body.String() != "{\"revision\":1}\n" {
		t.Fatalf("authenticated rollback response = %q", response.Body.String())
	}

	// A browser session must not replace the collector's bearer token.
	deps := accountDependencies(db)
	deps.OpAMP = http.NotFoundHandler()
	deps.OTLPMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server = httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	if response := send(http.MethodPost, "/v1/otlp/v1/metrics", "", cookie, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("OTLP with browser cookie status = %d, want 401", response.Code)
	}
	if response := send(http.MethodPost, "/api/v1/auth/logout", "", cookie, sameOrigin); response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", response.Code)
	}
	checkDenied(cookie, nil, http.StatusUnauthorized)

	cookie = login(t, newAccountHandler(db), "a long password for testing")
	if _, err := db.ExecContext(ctx, "DELETE FROM users"); err != nil {
		t.Fatalf("remove administrator: %v", err)
	}
	checkDenied(cookie, nil, http.StatusUnauthorized)
	if _, err := db.ExecContext(ctx, "DROP TABLE users"); err != nil {
		t.Fatalf("make account storage unavailable: %v", err)
	}
	checkDenied(cookie, nil, http.StatusInternalServerError)
	if err := db.Close(); err != nil {
		t.Fatalf("make session storage unavailable: %v", err)
	}
	checkDenied(cookie, nil, http.StatusInternalServerError)
}
