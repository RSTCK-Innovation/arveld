package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerLogoutInvalidatesOnlyCurrentSession(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	server := newAccountHandler(db)
	if err := auth.NewStore(db).CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "a long password for testing",
	}); err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	send := func(method, path, token, origin, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		if token != "" {
			request.Header.Set("Cookie", "arveld_session="+token)
		}
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	const credentials = `{"email":"camille@example.com","password":"a long password for testing"}` //nolint:gosec // Public fixture credentials for the temporary test database.
	cookie := loginCookie(t, send(http.MethodPost, "/api/v1/auth/login", "", "", credentials))
	read := func(token string, want int) {
		t.Helper()
		if response := send(http.MethodGet, "/api/v1/auth/session", token, "", ""); response.Code != want {
			t.Fatalf("read session status = %d, want %d", response.Code, want)
		}
	}
	checkError := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want || response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("logout response = %d with cookie %q, want %d without cookie", response.Code, response.Header().Get("Set-Cookie"), want)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Error("logout failure must not be cached")
		}
	}
	logout := func(token string) {
		t.Helper()
		response := send(http.MethodPost, "/api/v1/auth/logout", token, "https://arveld.example", "")
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("logout response = %d with %d body bytes, want empty 204", response.Code, response.Body.Len())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Error("logout must not be cached")
		}
		result := response.Result()
		defer func() {
			if err := result.Body.Close(); err != nil {
				t.Errorf("close response body: %v", err)
			}
		}()
		cookies := result.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("logout emitted %d cookies, want 1", len(cookies))
		}
		cleared := cookies[0]
		if cleared.Name != cookie.Name || cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.Expires.Before(time.Now()) {
			t.Error("logout must expire the browser session cookie")
		}
		if cleared.Path != cookie.Path || cleared.Domain != cookie.Domain || !cleared.HttpOnly || !cleared.Secure || cleared.SameSite != cookie.SameSite {
			t.Error("cookie deletion must preserve the original cookie scope and security attributes")
		}
	}

	read(cookie.Value, http.StatusOK)
	checkError(send(http.MethodPost, "/api/v1/auth/logout", cookie.Value, "https://another.example", ""), http.StatusForbidden)
	read(cookie.Value, http.StatusOK)
	if response := send(http.MethodGet, "/api/v1/auth/logout", cookie.Value, "", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout status = %d, want 405", response.Code)
	}
	read(cookie.Value, http.StatusOK)

	// Wait for the login endpoint's one-second interval before opening another browser session.
	time.Sleep(time.Second)
	other := loginCookie(t, send(http.MethodPost, "/api/v1/auth/login", "", "", credentials))
	read(other.Value, http.StatusOK)

	// A failed deletion must leave the browser informed of the failure and the session usable.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_session_delete BEFORE DELETE ON sessions
		BEGIN SELECT RAISE(ABORT, 'test session deletion failure'); END;
	`); err != nil {
		t.Fatalf("install session deletion failure: %v", err)
	}
	checkError(send(http.MethodPost, "/api/v1/auth/logout", cookie.Value, "", ""), http.StatusInternalServerError)
	read(cookie.Value, http.StatusOK)
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_session_delete"); err != nil {
		t.Fatalf("remove session deletion failure: %v", err)
	}
	logout(cookie.Value)
	read(cookie.Value, http.StatusUnauthorized)
	read(other.Value, http.StatusOK)
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	read(cookie.Value, http.StatusUnauthorized)
	read(other.Value, http.StatusOK)
	logout(cookie.Value)
	logout("")
	logout("unknown-token")
	read(other.Value, http.StatusOK)

	if err := db.Close(); err != nil {
		t.Fatalf("close database before session loading failure: %v", err)
	}
	checkError(send(http.MethodPost, "/api/v1/auth/logout", other.Value, "", ""), http.StatusInternalServerError)
}

func TestHTTPServerReadsCurrentSession(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	server := newAccountHandler(db)
	read := func(cookie *http.Cookie, wantStatus int, wantBody string) {
		t.Helper()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/session", nil)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != wantStatus || response.Body.String() != wantBody {
			t.Fatalf("session response = %d %q, want %d %q", response.Code, response.Body.String(), wantStatus, wantBody)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Error("session response must not be cached")
		}
		if response.Header().Get("Set-Cookie") != "" {
			t.Error("reading the session must not issue or renew a cookie")
		}
		if wantStatus == http.StatusOK && response.Header().Get("Content-Type") != "application/json" {
			t.Error("authenticated session response must be JSON")
		}
	}

	read(nil, http.StatusUnauthorized, "Unauthorized\n")
	read(&http.Cookie{Name: "arveld_session", Value: "unknown-token"}, http.StatusUnauthorized, "Unauthorized\n") //nolint:gosec // Request cookie fixture; security attributes belong to Set-Cookie responses.

	fixtureCookie := func(administratorID int, lifetime time.Duration) *http.Cookie {
		t.Helper()
		return sessionCookie(t, db, administratorID, lifetime)
	}

	read(fixtureCookie(1, time.Hour), http.StatusUnauthorized, "Unauthorized\n")
	if err := auth.NewStore(db).CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "a long password for testing",
	}); err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	read(fixtureCookie(1, -time.Hour), http.StatusUnauthorized, "Unauthorized\n")
	read(fixtureCookie(0, time.Hour), http.StatusUnauthorized, "Unauthorized\n")
	read(fixtureCookie(2, time.Hour), http.StatusUnauthorized, "Unauthorized\n")

	login := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"camille@example.com","password":"a long password for testing"}`))
	login.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, login)
	cookie := loginCookie(t, response)
	const profile = "{\"name\":\"Camille\",\"email\":\"camille@example.com\"}\n"
	read(cookie, http.StatusOK, profile)
	read(&http.Cookie{Name: cookie.Name, Value: "tampered-" + cookie.Value}, http.StatusUnauthorized, "Unauthorized\n") //nolint:gosec // Request cookie fixture; security attributes belong to Set-Cookie responses.
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	read(cookie, http.StatusOK, profile)
	read(cookie, http.StatusOK, profile)

	// Keep session storage available while making the account lookup fail.
	if _, err := db.ExecContext(ctx, "DROP TABLE users"); err != nil {
		t.Fatalf("make account storage unavailable: %v", err)
	}
	read(cookie, http.StatusInternalServerError, "Internal Server Error\n")
	if err := db.Close(); err != nil {
		t.Fatalf("close database before session storage failure: %v", err)
	}
	read(cookie, http.StatusInternalServerError, "Internal Server Error\n")
}
