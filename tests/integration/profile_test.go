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

func TestHTTPServerUpdatesAdministratorProfile(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	original, err := auth.NewStore(db).Administrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	update := func(body, contentType, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodPut,
			"https://arveld.example/api/v1/account/profile", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	const body = `{"name":"  Robin Smith 🔧  ","email":"  ROBIN@EXAMPLE.COM  "}`
	response := update(body, "application/json; charset=utf-8", "https://arveld.example", cookie)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("update response = %d %q, want empty 204", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Error("profile update must not be cached or replace the session cookie")
	}
	updated, err := auth.NewStore(db).Administrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Robin Smith 🔧" || updated.Email != "robin@example.com" || updated.PasswordHash != original.PasswordHash {
		t.Fatal("update must normalize name and email while preserving the password hash")
	}

	for _, test := range []struct {
		name, body, contentType, origin string
		cookie                          *http.Cookie
		status                          int
	}{
		{"anonymous", body, "application/json", "", nil, http.StatusUnauthorized},
		{"cross origin", body, "application/json", "https://another.example", cookie, http.StatusForbidden},
		{"malformed JSON", `{`, "application/json", "", cookie, http.StatusBadRequest},
		{"unknown field", `{"name":"Robin","email":"robin@example.com","password":"another password"}`, "application/json", "", cookie, http.StatusBadRequest},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", cookie, http.StatusRequestEntityTooLarge},
		{"invalid email", `{"name":"Robin","email":"invalid"}`, "application/json", "", cookie, http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := update(test.body, test.contentType, test.origin, test.cookie)
			if response.Code != test.status {
				t.Fatalf("update status = %d, want %d", response.Code, test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Error("rejected update must not be cached or issue a cookie")
			}
			account, err := auth.NewStore(db).Administrator(ctx)
			if err != nil || account != updated {
				t.Fatalf("rejected update changed the stored account or failed to read it: %v", err)
			}
		})
	}

	// Reopen SQLite and the HTTP handler to prove persistence with the same cookie.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/session", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"name\":\"Robin Smith 🔧\",\"email\":\"robin@example.com\"}\n" {
		t.Fatalf("session must expose the persisted profile: %d %q", response.Code, response.Body.String())
	}

	for i, email := range []string{"camille@example.com", "robin@example.com"} {
		if i > 0 {
			time.Sleep(time.Second) // Respect the real login attempt interval.
		}
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"email":"`+email+`","password":"a long password for testing"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if i == 0 {
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("old email login status = %d, want 401", response.Code)
			}
		} else {
			loginCookie(t, response)
		}
	}

	// Keep reads working while forcing the profile write to fail.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_profile_update BEFORE UPDATE ON users
		BEGIN SELECT RAISE(ABORT, 'test profile write failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	response = update(`{"name":"Camille","email":"camille@example.com"}`, "application/json", "", cookie)
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatalf("failed update response = %d %q, want generic 500", response.Code, response.Body.String())
	}
	account, err := auth.NewStore(db).Administrator(ctx)
	if err != nil || account != updated {
		t.Fatalf("failed update changed the stored account or failed to read it: %v", err)
	}
}
