package integration

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerSessionCancellationDoesNotLogErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	var logs bytes.Buffer
	deps := accountDependencies(db)
	deps.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	for _, target := range []string{"/api/v1/auth/session", "/api/v1/agents"} {
		t.Run(target, func(t *testing.T) {
			logs.Reset()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("client cancellation logged as a server error: %s", logs.String())
			}
			if response.Body.Len() != 0 || response.Code == http.StatusInternalServerError || response.Header().Get("Set-Cookie") != "" {
				t.Errorf("canceled session read wrote a response or changed the cookie: %d %q", response.Code, response.Body.String())
			}
			request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			request.AddCookie(cookie)
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("session after cancellation = %d; the original cookie must remain usable", response.Code)
			}
		})
	}
}

func TestHTTPServerSessionFailuresRemainVisible(t *testing.T) {
	for _, test := range []struct {
		name         string
		closeStorage bool
	}{
		{"request deadline", false},
		{"storage failure after request cancellation", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
			cookie := sessionCookie(t, db, 1, time.Hour)
			var logs bytes.Buffer
			deps := accountDependencies(db)
			deps.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.closeStorage {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				cancel()
			} else {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Unix(1, 0))
				defer stop()
			}
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/session", nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
				t.Fatalf("session failure = %d %q, want generic 500", response.Code, response.Body.String())
			}
			if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), `operation="load browser session"`) {
				t.Errorf("session failure missing from error logs: %s", logs.String())
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatal("session failure must not overwrite the cookie")
			}
		})
	}
}

func TestHTTPServerPreservesSessionOnStoredIdentityFailure(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	// Simulate unreadable persisted data while keeping account lookup and writes available.
	if _, err := db.ExecContext(t.Context(), "UPDATE sessions SET data = ?", []byte("{")); err != nil {
		t.Fatalf("make session identity unreadable: %v", err)
	}
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/auth/session", ""},
		{http.MethodGet, "/api/v1/agents", ""},
		{http.MethodPost, "/api/v1/auth/login", `{"email":"camille@example.com","password":"a long password for testing"}`},
		{http.MethodPost, "/api/v1/auth/logout", ""},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), route.method, route.path, strings.NewReader(route.body))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
				t.Fatalf("session load failure = %d %q, want generic 500", response.Code, response.Body.String())
			}
			if response.Header().Get("Set-Cookie") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("failed session loading must not issue a cookie or cache the response")
			}
		})
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE sessions SET data = ?", []byte(`{"administrator_id":1}`)); err != nil {
		t.Fatalf("restore session identity: %v", err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/session", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"name\":\"Camille\",\"email\":\"camille@example.com\"}\n" || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("session after recovery = %d %q; the original cookie must remain usable", response.Code, response.Body.String())
	}
}
