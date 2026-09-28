package integration

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerRevokesAgentKey(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	store := agentauth.NewStore(db)
	first, token, err := store.CreateKey(ctx, agentauth.CreateKeyParams{Name: "Agent Paris"})
	if err != nil {
		t.Fatal(err)
	}
	second, otherToken, err := store.CreateKey(ctx, agentauth.CreateKeyParams{Name: "Agent Lyon"})
	if err != nil {
		t.Fatal(err)
	}
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	list := func() []agentauth.AgentKey {
		t.Helper()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/agentkeys", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("list agent keys status = %d, want 200", response.Code)
		}
		var result struct {
			Keys []agentauth.AgentKey `json:"keys"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode agent key list: %v", err)
		}
		return result.Keys
	}
	revoke := func(id string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodDelete, "https://arveld.example/api/v1/agentkeys/"+id, nil)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	checkRevoked := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("revoke agent key response = %d %q, want an empty 204", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
			t.Error("revoking an agent key must not be cached or replace the session cookie")
		}
	}

	before := time.Now()
	response := revoke(first.ID, cookie, "https://arveld.example")
	after := time.Now()
	checkRevoked(response)
	keys := list()
	if len(keys) != 2 || keys[1].ID != first.ID || keys[1].RevokedAt == nil {
		t.Fatal("the revoked key must remain in the list with a revocation date")
	}
	if keys[1].RevokedAt.Before(before) || keys[1].RevokedAt.After(after) {
		t.Fatal("the revocation date must reflect the successful request")
	}
	first.RevokedAt = keys[1].RevokedAt
	want := []agentauth.AgentKey{second, first}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal("revocation must preserve all other metadata and leave other keys unchanged")
	}

	// Reopen the real store to verify persistence and repeated revocation.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	store = agentauth.NewStore(db)
	checkRevoked(revoke(first.ID, cookie, ""))
	if !reflect.DeepEqual(list(), want) {
		t.Fatal("repeated revocation must preserve the original date after reopening SQLite")
	}
	if _, err := store.AuthenticateKey(ctx, token); !errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("revoked token authentication = %v, want ErrInvalidKey", err)
	}

	for _, test := range []struct {
		name, id, origin string
		cookie           *http.Cookie
		status           int
	}{
		{"anonymous", second.ID, "", nil, http.StatusUnauthorized},
		{"cross origin", second.ID, "https://another.example", cookie, http.StatusForbidden},
		{"unknown key", "missing", "", cookie, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := revoke(test.id, test.cookie, test.origin)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("rejected revocation = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Error("rejected revocation must not be cached or issue a session cookie")
			}
			if !reflect.DeepEqual(list(), want) {
				t.Fatal("rejected revocation changed agent keys")
			}
		})
	}

	// Fail only the revocation write, leaving authentication and listing available.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_agent_key_revocation BEFORE UPDATE ON agent_keys
		BEGIN SELECT RAISE(ABORT, 'test agent key revocation failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	response = revoke(second.ID, cookie, "")
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed revocation must return a generic 500")
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("failed revocation must not be cached or issue a session cookie")
	}
	if !reflect.DeepEqual(list(), want) {
		t.Fatal("failed revocation changed agent keys")
	}
	if id, err := store.AuthenticateKey(ctx, otherToken); err != nil || id != second.ID {
		t.Fatalf("other token authentication = %q, %v, want the unaffected key ID", id, err)
	}
}

func TestHTTPServerListsAgentKeyMetadata(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	list := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/agentkeys", nil)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	response := list(cookie)
	if response.Code != http.StatusOK || response.Body.String() != "{\"keys\":[]}\n" {
		t.Fatalf("empty agent key list = %d %q, want 200 with an empty array", response.Code, response.Body.String())
	}

	// Fixed dates make the expected public payload independent of the clock.
	for _, fixture := range []struct {
		id, name, createdAt, revokedAt string
	}{
		{"revoked", "Previous agent", "2026-01-01", "2026-01-02"},
		{"active", "Current agent", "2026-01-03", ""},
	} {
		hash := sha256.Sum256([]byte("test-only-secret-" + fixture.id))
		if _, err := db.ExecContext(ctx, `
			INSERT INTO agent_keys (id, name, prefix, token_hash, created_at_ns, revoked_at_ns)
			VALUES (?, ?, ?, ?, unixepoch(?) * 1000000000, unixepoch(?) * 1000000000)
		`, fixture.id, fixture.name, "arv_agent_"+fixture.id, hash[:], fixture.createdAt, fixture.revokedAt); err != nil {
			t.Fatalf("insert agent key fixture: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)

	response = list(cookie)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("agent key list status = %d, want 200 with JSON", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("listing agent keys must not be cached or replace the session cookie")
	}
	// Compare the entire JSON value: extra secret, digest or expiration fields fail.
	const expected = `{"keys":[
		{"id":"active","name":"Current agent","prefix":"arv_agent_active",
		 "createdAt":"2026-01-03T00:00:00Z","revokedAt":null},
		{"id":"revoked","name":"Previous agent","prefix":"arv_agent_revoked",
		 "createdAt":"2026-01-01T00:00:00Z","revokedAt":"2026-01-02T00:00:00Z"}
	]}`
	var got, want any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode agent key list: %v", err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("agent key list must contain only persisted public metadata, newest first, including revoked keys")
	}

	response = list(nil)
	if response.Code != http.StatusUnauthorized || response.Body.String() != "Unauthorized\n" {
		t.Fatal("listing agent keys without a session must return a generic 401")
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("rejected listing must not be cached or issue a session cookie")
	}
	// Keep authentication available while the agent-key store cannot be read.
	if _, err := db.ExecContext(ctx, "DROP TABLE agent_keys"); err != nil {
		t.Fatal(err)
	}
	response = list(cookie)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed agent key reads must return a generic 500")
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("storage errors must not be cached or replace the session cookie")
	}
}

func TestHTTPServerCreatesAgentKey(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	create := func(body, contentType, origin, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost,
			"https://arveld.example/api/v1/agentkeys", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}

	const body = `{"name":"  Agent Paris 🔧  "}`
	before := time.Now()
	response := create(body, "application/json; charset=utf-8", "https://arveld.example", "", cookie)
	after := time.Now()
	if response.Code != http.StatusCreated {
		t.Fatalf("agent key creation status = %d, want 201", response.Code)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Fatal("creation must return uncached JSON without replacing the session cookie")
	}
	var created struct {
		Key struct {
			ID        string     `json:"id"`
			Name      string     `json:"name"`
			Prefix    string     `json:"prefix"`
			CreatedAt time.Time  `json:"createdAt"`
			RevokedAt *time.Time `json:"revokedAt"`
		} `json:"key"`
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(strings.NewReader(response.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&created); err != nil {
		t.Fatalf("decode agent key response: %v", err)
	}
	for _, field := range []string{"key", "token", "id", "name", "prefix", "createdAt", "revokedAt"} {
		if !strings.Contains(response.Body.String(), `"`+field+`":`) {
			t.Errorf("response is missing JSON field %q", field)
		}
	}
	key := created.Key
	if key.ID == "" || key.Name != "Agent Paris 🔧" || key.RevokedAt != nil || !strings.Contains(response.Body.String(), `"revokedAt":null`) {
		t.Fatal("creation must return the normalized name and active key metadata")
	}
	if key.CreatedAt.Before(before) || key.CreatedAt.After(after) {
		t.Fatal("creation timestamp must reflect the successful request")
	}
	if created.Token == "" || created.Token == key.Prefix {
		t.Fatal("creation must return a complete token separately from public metadata")
	}

	// The returned credential must remain usable after reopening the real store.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	store := agentauth.NewStore(db)
	if id, err := store.AuthenticateKey(ctx, created.Token); err != nil || id != key.ID {
		t.Fatalf("created key authentication = %q, %v, want the returned key ID", id, err)
	}
	want := []agentauth.AgentKey{{ID: key.ID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt}}
	if keys, err := store.ListKeys(ctx); err != nil || !reflect.DeepEqual(keys, want) {
		t.Fatalf("persisted key metadata differs from the creation response: %v", err)
	}
	_, managementToken, err := auth.NewStore(db).CreateAPIKey(ctx, auth.CreateAPIKeyParams{Name: "Automation", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body, contentType, origin, token string
		cookie                                 *http.Cookie
		status                                 int
	}{
		{"anonymous", body, "application/json", "", "", nil, http.StatusUnauthorized},
		{"management key only", body, "application/json", "", managementToken, nil, http.StatusUnauthorized},
		{"agent key only", body, "application/json", "", created.Token, nil, http.StatusUnauthorized},
		{"cross origin", body, "application/json", "https://another.example", "", cookie, http.StatusForbidden},
		{"malformed JSON", `{`, "application/json", "", "", cookie, http.StatusBadRequest},
		{"supplied secret", `{"name":"Agent Paris","token":"chosen-secret"}`, "application/json", "", "", cookie, http.StatusBadRequest},
		{"supplied expiration", `{"name":"Agent Paris","expiresDays":null}`, "application/json", "", "", cookie, http.StatusBadRequest},
		{"missing name", `{}`, "application/json", "", "", cookie, http.StatusUnprocessableEntity},
		{"blank name", `{"name":"   "}`, "application/json", "", "", cookie, http.StatusUnprocessableEntity},
		{"short Unicode name", `{"name":"🔧"}`, "application/json", "", "", cookie, http.StatusUnprocessableEntity},
		{"long name", `{"name":"` + strings.Repeat("🔧", 81) + `"}`, "application/json", "", "", cookie, http.StatusUnprocessableEntity},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", "", cookie, http.StatusRequestEntityTooLarge},
		{"unsupported media type", body, "text/plain", "", "", cookie, http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := create(test.body, test.contentType, test.origin, test.token, test.cookie)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("rejected creation = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Error("rejected creation must not be cached or issue a session cookie")
			}
			if keys, err := store.ListKeys(ctx); err != nil || !reflect.DeepEqual(keys, want) {
				t.Fatalf("rejected creation changed agent keys: %v", err)
			}
		})
	}

	// Inject a failed insert without disabling session authentication.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_agent_key_insert BEFORE INSERT ON agent_keys
		BEGIN SELECT RAISE(ABORT, 'test agent key write failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	response = create(body, "application/json", "", "", cookie)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed persistence must return a generic 500 without a token")
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("failed persistence must not be cached or issue a session cookie")
	}
	if keys, err := store.ListKeys(ctx); err != nil || !reflect.DeepEqual(keys, want) {
		t.Fatalf("failed persistence changed agent keys: %v", err)
	}
}
