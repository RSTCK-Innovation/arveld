package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPPasswordChangeRevokesPendingLoginSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "current password for testing",
	}); err != nil {
		t.Fatal(err)
	}
	existing := login(t, newAccountHandler(db), "current password for testing")
	store := &pausedSessionStore{
		SessionStore: auth.NewSessionStore(db),
		committing:   make(chan struct{}),
		resume:       make(chan struct{}),
	}
	deps := accountDependencies(db)
	deps.Sessions = store
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)

	send := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	waitResponse := func(done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		t.Helper()
		select {
		case response := <-done:
			return response
		case <-ctx.Done():
			t.Fatal("authentication request did not finish")
			return nil
		}
	}
	loginDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loginDone <- send(http.MethodPost, "/api/v1/auth/login",
			`{"email":"camille@example.com","password":"current password for testing"}`, nil)
	}()
	select {
	case <-store.committing:
		// The old password has been verified, but the session is not persisted yet.
	case <-ctx.Done():
		t.Fatal("login did not reach session persistence")
	}

	changeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		changeDone <- send(http.MethodPut, "/api/v1/account/password",
			`{"currentPassword":"current password for testing","password":"replacement password for testing"}`, existing)
	}()
	// The reset must finish even while a verified login is paused before its write.
	changed := waitResponse(changeDone)
	close(store.resume)
	rejected := waitResponse(loginDone)
	if rejected.Code != http.StatusUnauthorized || rejected.Header().Get("Set-Cookie") != "" {
		t.Fatalf("outdated login = %d, cookie %q, want 401 without cookie", rejected.Code, rejected.Header().Get("Set-Cookie"))
	}

	if changed.Code != http.StatusNoContent {
		t.Fatalf("password change status = %d, want 204", changed.Code)
	}
	for _, cookie := range []*http.Cookie{existing} {
		response := send(http.MethodGet, "/api/v1/auth/session", "", cookie)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("session status after password change = %d, want 401", response.Code)
		}
	}
}

// pausedSessionStore delays the actual SQLite write after password verification.
type pausedSessionStore struct {
	*auth.SessionStore
	committing chan struct{}
	resume     chan struct{}
}

func (store *pausedSessionStore) Rotate(ctx context.Context, previous string, verified auth.VerifiedCredentials) (auth.Session, error) {
	close(store.committing)
	select {
	case <-store.resume:
	case <-ctx.Done():
		return auth.Session{}, fmt.Errorf("wait to persist login session: %w", ctx.Err())
	}
	session, err := store.SessionStore.Rotate(ctx, previous, verified)
	if err != nil {
		return auth.Session{}, fmt.Errorf("persist delayed login session: %w", err)
	}
	return session, nil
}

func TestHTTPServerLoginCreatesPersistentSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	store := auth.NewStore(db)
	server := newAccountHandler(db)
	const body = `{"email":"  CAMILLE@EXAMPLE.COM  ","password":"  a very long password 🔑  "}`
	post := func(body, contentType, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost,
			"https://arveld.example/api/v1/auth/login", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	checkRejected := func(t *testing.T, response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("login status = %d, want %d", response.Code, want)
		}
		if response.Header().Get("Set-Cookie") != "" {
			t.Error("rejected login issued a cookie")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Error("rejected login must not be cached")
		}
	}

	// Hold SQLite's sole connection to keep the first login in progress.
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close reserved connection: %v", err)
		}
	})
	waitCount := db.Stats().WaitCount
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- post(body, "application/json", "", nil) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for db.Stats().WaitCount == waitCount {
		select {
		case <-ctx.Done():
			t.Fatal("first login did not reach SQLite")
		case <-ticker.C:
		}
	}
	checkRejected(t, post(body, "application/json", "", nil), http.StatusTooManyRequests)
	if err := connection.Close(); err != nil {
		t.Fatalf("release database connection: %v", err)
	}
	missing := <-first
	checkRejected(t, missing, http.StatusUnauthorized)
	if err := store.CreateAdministrator(ctx, auth.CreateAdministratorParams{ //nolint:gosec // Public test password exercises preservation of surrounding spaces.
		Name: "Camille", Email: "camille@example.com", Password: "  a very long password 🔑  ",
	}); err != nil {
		t.Fatalf("create administrator: %v", err)
	}

	for _, test := range []struct {
		name, body, contentType, origin string
		status                          int
	}{
		{"malformed JSON", `{`, "application/json", "", http.StatusBadRequest},
		{"unknown field", strings.Replace(body, `"email":`, `"role":"admin","email":`, 1), "application/json", "", http.StatusBadRequest},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", http.StatusRequestEntityTooLarge},
		{"cross origin", body, "application/json", "https://another.example", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkRejected(t, post(test.body, test.contentType, test.origin, nil), test.status)
		})
	}

	// These sleeps exercise the real instance-wide one-second login interval.
	time.Sleep(time.Second)
	wrongEmail := post(strings.Replace(body, "CAMILLE", "OTHER", 1), "application/json", "", nil)
	checkRejected(t, wrongEmail, http.StatusUnauthorized)
	time.Sleep(time.Second)
	wrongPassword := post(strings.Replace(body, "  a very long password 🔑  ", "incorrect", 1), "application/json", "", nil)
	checkRejected(t, wrongPassword, http.StatusUnauthorized)
	if missing.Body.String() != wrongEmail.Body.String() || missing.Body.String() != wrongPassword.Body.String() {
		t.Error("invalid credentials must return the same public error")
	}
	busy := post(body, "application/json", "", nil)
	checkRejected(t, busy, http.StatusTooManyRequests)
	if busy.Header().Get("Retry-After") != "1" {
		t.Error("rate-limited login must advertise Retry-After: 1")
	}

	time.Sleep(time.Second)
	loginStarted := time.Now()
	response := post(body, "application/json; charset=utf-8", "https://arveld.example", nil)
	loginFinished := time.Now()
	cookie := loginCookie(t, response)
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Error("login cookie must be Secure, HttpOnly, SameSite=Lax, host-only, and use path /")
	}
	if cookie.MaxAge < 86390 || cookie.MaxAge > 86401 {
		t.Errorf("cookie MaxAge = %d, want approximately 24 hours", cookie.MaxAge)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	readSession := func(cookie *http.Cookie, status int) {
		t.Helper()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/session", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("session status = %d, want %d", response.Code, status)
		}
	}
	readSession(cookie, http.StatusOK)
	var persisted []byte
	var persistedToken string
	var expiryNS int64
	if err := db.QueryRowContext(ctx, "SELECT token, data, expiry_ns FROM sessions").Scan(&persistedToken, &persisted, &expiryNS); err != nil {
		t.Fatal(err)
	}
	if string(persisted) != `{"administrator_id":1}` {
		t.Fatalf("unexpected persisted session fields: %q", persisted)
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	if persistedToken != hex.EncodeToString(digest[:]) {
		t.Fatal("session storage must contain the cookie digest")
	}
	expiry := time.Unix(0, expiryNS)
	if expiry.Before(loginStarted.Add(24*time.Hour)) || expiry.After(loginFinished.Add(24*time.Hour)) {
		t.Fatalf("persisted session expiry = %s, want 24 hours after login", expiry)
	}
	if _, found, err := auth.NewSessionStore(db).Find(ctx, cookie.Value); err != nil || !found {
		t.Fatalf("raw cookie lookup = (_, %t, %v), want successful lookup through the session module", found, err)
	}
	rotated := loginCookie(t, post(body, "application/json", "", cookie))
	if rotated.Value == cookie.Value {
		t.Error("successful login must rotate the session token")
	}
	readSession(cookie, http.StatusUnauthorized)
	readSession(rotated, http.StatusOK)

	// Fault injection at the database boundary: session persistence must fail closed.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_session_insert BEFORE INSERT ON sessions
		BEGIN SELECT RAISE(ABORT, 'test session write failure'); END;
	`); err != nil {
		t.Fatalf("install session write failure: %v", err)
	}
	time.Sleep(time.Second)
	checkRejected(t, post(body, "application/json", "", nil), http.StatusInternalServerError)
	if err := db.Close(); err != nil {
		t.Fatalf("close database before error check: %v", err)
	}
	time.Sleep(time.Second)
	checkRejected(t, post(body, "application/json", "", nil), http.StatusInternalServerError)
}

func TestInvalidJSONDoesNotConsumeLoginAdmission(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "admission.db"))
	createAdministrator(t, db)
	for _, test := range []struct {
		name, body, media string
		status            int
	}{
		{"syntax", "{", "application/json", http.StatusBadRequest},
		{"unknown field", `{"unexpected":true}`, "application/json", http.StatusBadRequest},
		{"second value", `{} {}`, "application/json", http.StatusBadRequest},
		{"utf8", "{\"email\":\"\xff\"}", "application/json", http.StatusBadRequest},
		{"body limit", strings.Repeat(" ", 4097), "application/json", http.StatusRequestEntityTooLarge},
		{"media type", `{}`, "text/plain", http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newAccountHandler(db)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(test.body))
			request.ContentLength = -1
			request.Header.Set("Content-Type", test.media)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("invalid input = %d, want %d", response.Code, test.status)
			}
			login(t, handler, "a long password for testing")
		})
	}
}
