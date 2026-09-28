package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerRevokesAPIKey(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	var created []auth.APIKey
	for _, body := range []string{
		`{"name":"Deployment","permission":"write","expiresDays":1}`,
		`{"name":"Supervision","permission":"read","expiresDays":null}`,
	} {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create API key status = %d, want 201", response.Code)
		}
		var result struct {
			Key auth.APIKey `json:"key"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode created API key: %v", err)
		}
		created = append(created, result.Key)
	}
	list := func() []auth.APIKey {
		t.Helper()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/apikeys", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("list API keys status = %d, want 200", response.Code)
		}
		var result struct {
			Keys []auth.APIKey `json:"keys"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode API key list: %v", err)
		}
		return result.Keys
	}
	revoke := func(id string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodDelete, "https://arveld.example/api/v1/apikeys/"+id, nil)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}

	before := time.Now()
	response := revoke(created[0].ID, cookie, "https://arveld.example")
	after := time.Now()
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("revoke API key response = %d %q, want an empty 204", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("revoking an API key must not be cached or replace the session cookie")
	}
	keys := list()
	if len(keys) != 2 || keys[1].ID != created[0].ID || keys[1].RevokedAt == nil {
		t.Fatal("the revoked key must remain in the list with a revocation date")
	}
	if keys[1].RevokedAt.Before(before) || keys[1].RevokedAt.After(after) {
		t.Fatal("the revocation date must reflect the successful request")
	}
	created[0].RevokedAt = keys[1].RevokedAt
	want := []auth.APIKey{created[1], created[0]}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal("revocation must preserve all other metadata and leave other keys unchanged")
	}

	// Reopening SQLite also proves that revocation survives a controller restart.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	response = revoke(created[0].ID, cookie, "")
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatal("revoking an already revoked key must still return an empty 204")
	}
	if !reflect.DeepEqual(list(), want) {
		t.Fatal("repeated revocation must preserve the original revocation date after a restart")
	}

	for _, test := range []struct {
		name, id, origin string
		cookie           *http.Cookie
		status           int
	}{
		{"anonymous", created[1].ID, "", nil, http.StatusUnauthorized},
		{"cross origin", created[1].ID, "https://another.example", cookie, http.StatusForbidden},
		{"unknown key", "missing", "", cookie, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := revoke(test.id, test.cookie, test.origin)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("rejected revocation response = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Error("rejected revocation must not be cached or issue a cookie")
			}
		})
	}
	if !reflect.DeepEqual(list(), want) {
		t.Fatal("rejected revocations must leave every key unchanged")
	}

	// Simulate a storage failure without replacing the production store.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_api_key_revocation BEFORE UPDATE ON api_keys
		BEGIN SELECT RAISE(ABORT, 'simulated API key revocation failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	response = revoke(created[1].ID, cookie, "")
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed API key revocation must return a generic 500")
	}
	if !reflect.DeepEqual(list(), want) {
		t.Fatal("failed revocation must leave every key unchanged")
	}
}

func TestHTTPServerListsAPIKeyMetadata(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	list := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/apikeys", nil)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	response := list(cookie)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"keys":[]}` {
		t.Fatalf("empty API key list = %d %q, want 200 with an empty array", response.Code, response.Body.String())
	}

	// Fixed timestamps cover ordering and optional expiration and revocation metadata.
	for _, fixture := range []struct {
		id, name, permission, createdAt, expiresAt, revokedAt string
	}{
		{"revoked", "Previous deployment", "write", "2026-01-03", "2026-02-02", "2026-01-04"},
		{"expired", "Temporary access", "read", "2026-01-01", "2026-01-02", ""},
		{"perpetual", "Supervision", "read", "2026-01-03", "", ""},
	} {
		hash := sha256.Sum256([]byte("fixture-secret-" + fixture.id))
		if _, err := db.ExecContext(ctx, `
			INSERT INTO api_keys (id, name, prefix, permission, token_hash, created_at_ns, expires_at_ns, revoked_at_ns)
			VALUES (?, ?, ?, ?, ?, unixepoch(?) * 1000000000, unixepoch(?) * 1000000000, unixepoch(?) * 1000000000)
		`, fixture.id, fixture.name, "arv_"+fixture.id, fixture.permission, hash[:],
			fixture.createdAt, fixture.expiresAt, fixture.revokedAt); err != nil {
			t.Fatalf("insert API key fixture: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)

	response = list(cookie)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("API key list status = %d, want 200 with JSON", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("listing API keys must not be cached or replace the session cookie")
	}
	// Compare the complete public payload: extra token or hash fields must fail this test.
	const expected = `{"keys":[
		{"id":"perpetual","name":"Supervision","prefix":"arv_perpetual","permission":"read",
		 "createdAt":"2026-01-03T00:00:00Z","expiresAt":null,"revokedAt":null},
		{"id":"revoked","name":"Previous deployment","prefix":"arv_revoked","permission":"write",
		 "createdAt":"2026-01-03T00:00:00Z","expiresAt":"2026-02-02T00:00:00Z","revokedAt":"2026-01-04T00:00:00Z"},
		{"id":"expired","name":"Temporary access","prefix":"arv_expired","permission":"read",
		 "createdAt":"2026-01-01T00:00:00Z","expiresAt":"2026-01-02T00:00:00Z","revokedAt":null}
	]}`
	var got, want any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode API key list: %v", err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("API key list must contain only persisted public metadata, newest first with ID as the tie-breaker")
	}

	response = list(nil)
	if response.Code != http.StatusUnauthorized || response.Body.String() != "Unauthorized\n" {
		t.Fatal("listing API keys without a session must return a generic 401")
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE api_keys"); err != nil {
		t.Fatal(err)
	}
	response = list(cookie)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed API key reads must return a generic 500")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Error("API key storage errors must not be cached")
	}
}

func TestHTTPServerCreatesAPIKeyWithoutPersistingSecret(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")
	create := func(body, contentType, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost,
			"https://arveld.example/api/v1/apikeys", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	var created struct {
		Key struct {
			ID         string     `json:"id"`
			Name       string     `json:"name"`
			Prefix     string     `json:"prefix"`
			Permission string     `json:"permission"`
			CreatedAt  time.Time  `json:"createdAt"`
			ExpiresAt  time.Time  `json:"expiresAt"`
			RevokedAt  *time.Time `json:"revokedAt"`
		} `json:"key"`
		Token string `json:"token"`
	}
	const body = `{"name":"  Deployment 🔧  ","permission":"write","expiresDays":30}`
	before := time.Now()
	response := create(body, "application/json; charset=utf-8", "https://arveld.example", cookie)
	after := time.Now()
	if response.Code != http.StatusCreated {
		t.Fatalf("API key creation status = %d, want 201", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("API key creation must not be cached or replace the session cookie")
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Error("API key creation must return JSON")
	}
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&created); err != nil {
		t.Fatalf("decode API key response: %v", err)
	}
	key := created.Key
	if key.ID == "" || key.Name != "Deployment 🔧" || key.Permission != "write" || key.RevokedAt != nil {
		t.Fatal("API key creation returned incorrect metadata")
	}
	if key.Prefix != "arv_"+key.ID || !strings.HasPrefix(created.Token, key.Prefix+"_") || len(created.Token) < len(key.Prefix)+27 {
		t.Fatal("API key must contain a public prefix and a separate random secret")
	}
	if key.CreatedAt.Before(before) || key.CreatedAt.After(after) || key.ExpiresAt.Sub(key.CreatedAt) != 30*24*time.Hour {
		t.Fatal("API key must expire exactly 30 days after creation")
	}

	// Inspect the persistence boundary after reopening SQLite: metadata and a digest only.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	var name, prefix, permission string
	var createdAt, expiresAt int64
	var hash []byte
	if err := db.QueryRowContext(ctx, `
		SELECT name, prefix, permission, created_at_ns, expires_at_ns, token_hash
		FROM api_keys WHERE id = ?
	`, key.ID).Scan(&name, &prefix, &permission, &createdAt, &expiresAt, &hash); err != nil {
		t.Fatalf("read persisted API key: %v", err)
	}
	if name != key.Name || prefix != key.Prefix || permission != key.Permission || createdAt != key.CreatedAt.UnixNano() || expiresAt != key.ExpiresAt.UnixNano() {
		t.Fatal("persisted API key metadata does not match the creation response")
	}
	wantHash := sha256.Sum256([]byte(created.Token))
	if !bytes.Equal(hash, wantHash[:]) {
		t.Fatal("SQLite must store the SHA-256 digest of the full token")
	}
	for _, test := range []struct {
		name, body, contentType, origin string
		cookie                          *http.Cookie
		status                          int
	}{
		{"anonymous", body, "application/json", "", nil, http.StatusUnauthorized},
		{"cross origin", body, "application/json", "https://another.example", cookie, http.StatusForbidden},
		{"malformed JSON", `{`, "application/json", "", cookie, http.StatusBadRequest},
		{"supplied secret", `{"name":"Automation","permission":"read","expiresDays":7,"token":"chosen-secret"}`, "application/json", "", cookie, http.StatusBadRequest},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", cookie, http.StatusRequestEntityTooLarge},
		{"missing name", `{"permission":"read","expiresDays":7}`, "application/json", "", cookie, http.StatusUnprocessableEntity},
		{"missing expiry", `{"name":"Automation","permission":"read"}`, "application/json", "", cookie, http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := create(test.body, test.contentType, test.origin, test.cookie)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("rejected API key response = %d, want generic %d", response.Code, test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Error("rejected creation must not be cached or issue a cookie")
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM api_keys").Scan(&count); err != nil || count != 1 {
				t.Fatalf("rejected request changed stored keys: count = %d, error = %v", count, err)
			}
		})
	}

	response = create(`{"name":"`+strings.Repeat("🔑", 80)+`","permission":"read","expiresDays":7}`, "application/json", "", cookie)
	var second struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Token string `json:"token"`
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("second API key creation status = %d, want 201", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.Key.ID == "" || second.Key.ID == key.ID || !strings.HasPrefix(second.Token, "arv_"+second.Key.ID+"_") {
		t.Error("each creation must produce a distinct identifier and matching token prefix")
	}
	if strings.TrimPrefix(second.Token, "arv_"+second.Key.ID+"_") == strings.TrimPrefix(created.Token, key.Prefix+"_") {
		t.Error("each creation must produce a distinct identifier and secret")
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_api_key_insert BEFORE INSERT ON api_keys
		BEGIN SELECT RAISE(ABORT, 'test API key write failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	response = create(body, "application/json", "", cookie)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatal("failed API key persistence must return a generic 500 without a token")
	}
}

func TestHTTPServerCreatesOneDayAndNonExpiringAPIKeys(t *testing.T) {
	for _, duration := range []string{"1", "null"} {
		t.Run(duration, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arveld.db")
			db := testutil.OpenDatabase(t, path)
			createAdministrator(t, db)
			server := newAccountHandler(db)
			cookie := login(t, server, "a long password for testing")
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/apikeys",
				strings.NewReader(`{"name":"Automation","permission":"read","expiresDays":`+duration+`}`))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("API key creation status = %d, want 201", response.Code)
			}
			var created struct {
				Key struct {
					ID        string     `json:"id"`
					Prefix    string     `json:"prefix"`
					CreatedAt time.Time  `json:"createdAt"`
					ExpiresAt *time.Time `json:"expiresAt"`
				} `json:"key"`
				Token string `json:"token"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			if created.Key.Prefix != "arv_"+created.Key.ID || !strings.HasPrefix(created.Token, created.Key.Prefix+"_") {
				t.Fatal("API key must use the arv_ prefix")
			}
			if duration == "null" {
				if created.Key.ExpiresAt != nil || !strings.Contains(response.Body.String(), `"expiresAt":null`) {
					t.Fatal("non-expiring API key must return expiresAt: null")
				}
			} else if created.Key.ExpiresAt == nil || created.Key.ExpiresAt.Sub(created.Key.CreatedAt) != 24*time.Hour {
				t.Fatal("one-day API key must expire exactly 24 hours after creation")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = testutil.OpenDatabase(t, path)
			var noExpiry bool
			var expiryNS int64
			if err := db.QueryRowContext(t.Context(), `
				SELECT expires_at_ns IS NULL, COALESCE(expires_at_ns, 0) FROM api_keys WHERE id = ?
			`, created.Key.ID).Scan(&noExpiry, &expiryNS); err != nil {
				t.Fatal(err)
			}
			if noExpiry != (duration == "null") || (!noExpiry && expiryNS != created.Key.ExpiresAt.UnixNano()) {
				t.Fatal("API key expiration was not preserved after reopening SQLite")
			}
		})
	}
}
