package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerChangesAdministratorPassword(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	server := newAccountHandler(db)
	const current = "  a current password 🔑  "
	const replacement = "  a new password 🔐  "
	if err := auth.NewStore(db).CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: current,
	}); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, server, current)
	// A second controller router shares persistence while owning a separate admission limiter.
	other := login(t, newAccountHandler(db), current)
	marshal := func(value map[string]string) string {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	body := marshal(map[string]string{"currentPassword": current, "password": replacement})
	send := func(method, path, body, contentType, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, method, "https://arveld.example"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	change := func(body string) *httptest.ResponseRecorder {
		return send(http.MethodPut, "/api/v1/account/password", body, "application/json", "https://arveld.example", cookie)
	}
	readSession := func(t *testing.T, cookie *http.Cookie, want int) {
		t.Helper()
		response := send(http.MethodGet, "/api/v1/auth/session", "", "", "", cookie)
		if response.Code != want {
			t.Fatalf("session status = %d, want %d", response.Code, want)
		}
		if want == http.StatusOK && response.Body.String() != "{\"name\":\"Camille\",\"email\":\"camille@example.com\"}\n" {
			t.Fatal("password change must preserve the administrator profile")
		}
	}
	checkRejected := func(t *testing.T, response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want || response.Body.String() != http.StatusText(want)+"\n" {
			t.Fatalf("password change response = %d %q, want generic %d", response.Code, response.Body.String(), want)
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
			t.Error("rejected password change must not be cached or issue a cookie")
		}
		readSession(t, cookie, http.StatusOK)
		readSession(t, other, http.StatusOK)
	}

	// A current browser session does not replace proof of the current password.
	checkRejected(t, change(marshal(map[string]string{
		"currentPassword": strings.TrimSpace(current), "password": replacement,
	})), http.StatusForbidden)
	busy := change(body)
	checkRejected(t, busy, http.StatusTooManyRequests)
	if busy.Header().Get("Retry-After") != "1" {
		t.Error("password attempt limiting must advertise Retry-After: 1")
	}

	for _, test := range []struct {
		name, body, contentType, origin string
		cookie                          *http.Cookie
		status                          int
	}{
		{"anonymous", body, "application/json", "", nil, http.StatusUnauthorized},
		{"cross origin", body, "application/json", "https://another.example", cookie, http.StatusForbidden},
		{"malformed JSON", `{`, "application/json", "", cookie, http.StatusBadRequest},
		{"unknown field", `{"currentPassword":"current","password":"new","email":"other@example.com"}`, "application/json", "", cookie, http.StatusBadRequest},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", cookie, http.StatusRequestEntityTooLarge},
		{"missing current password", marshal(map[string]string{"password": replacement}), "application/json", "", cookie, http.StatusUnprocessableEntity},
		{"missing new password", marshal(map[string]string{"currentPassword": current}), "application/json", "", cookie, http.StatusUnprocessableEntity},
		{"short new password", marshal(map[string]string{"currentPassword": current, "password": strings.Repeat("🔑", 14)}), "application/json", "", cookie, http.StatusUnprocessableEntity},
		{"long new password", marshal(map[string]string{"currentPassword": current, "password": strings.Repeat("🔑", 129)}), "application/json", "", cookie, http.StatusUnprocessableEntity},
		{"unchanged password", marshal(map[string]string{"currentPassword": current, "password": current}), "application/json", "", cookie, http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkRejected(t, send(http.MethodPut, "/api/v1/account/password", test.body, test.contentType, test.origin, test.cookie), test.status)
		})
	}

	// A deletion failure must roll back the new hash and keep both sessions usable.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER reject_password_session_delete BEFORE DELETE ON sessions
		BEGIN SELECT RAISE(ABORT, 'test session deletion failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // Respect the real password attempt interval.
	checkRejected(t, change(body), http.StatusInternalServerError)
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_password_session_delete"); err != nil {
		t.Fatal(err)
	}

	// Reusing the original password also proves that the failed change rolled back.
	time.Sleep(time.Second)
	response := change(body)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("password change response = %d %q, want empty 204", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Error("successful password change must not be cached")
	}
	result := response.Result()
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	cookies := result.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "arveld_session" || cookies[0].Value != "" || cookies[0].MaxAge >= 0 || !cookies[0].Expires.Before(time.Now()) {
		t.Fatal("password change must expire the browser session cookie")
	}
	if !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].Domain != "" {
		t.Error("cookie deletion must use the configured security attributes and scope")
	}
	readSession(t, cookie, http.StatusUnauthorized)
	readSession(t, other, http.StatusUnauthorized)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	server = newAccountHandler(db)
	readSession(t, cookie, http.StatusUnauthorized)
	readSession(t, other, http.StatusUnauthorized)
	for i, password := range []string{current, strings.TrimSpace(replacement), replacement} {
		if i > 0 {
			time.Sleep(time.Second) // Respect the login attempt interval.
		}
		response := send(http.MethodPost, "/api/v1/auth/login",
			marshal(map[string]string{"email": "camille@example.com", "password": password}), "application/json", "", nil)
		if i < 2 {
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("login attempt %d status = %d, want 401", i, response.Code)
			}
		} else {
			readSession(t, loginCookie(t, response), http.StatusOK)
		}
	}
}
