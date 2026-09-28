package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

// requireManagementAccess runs before route matching, including 404/405 responses.
// Explicit management credentials never fall back to a browser cookie.
func (api *api) requireManagementAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if !strings.HasPrefix(requestPath, "/api/v1/") || strings.HasPrefix(requestPath, "/api/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		sessionOnly := requestPath == "/api/v1/apikeys" || strings.HasPrefix(requestPath, "/api/v1/apikeys/") ||
			requestPath == "/api/v1/agentkeys" || strings.HasPrefix(requestPath, "/api/v1/agentkeys/") ||
			strings.HasPrefix(requestPath, "/api/v1/account/")
		if !sessionOnly && len(r.Header.Values("Authorization")) > 0 {
			if api.requireAPIKey(w, r) {
				next.ServeHTTP(w, r)
			}
			return
		}
		if err := api.origins.Check(r); err != nil {
			fail(w, http.StatusForbidden)
			return
		}
		administrator, ok := api.browserAdministrator(w, r)
		if !ok {
			return
		}
		ctx := context.WithValue(r.Context(), administratorContextKey{}, administrator)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (api *api) requireAPIKey(w http.ResponseWriter, r *http.Request) bool {
	headers := r.Header.Values("Authorization")
	permission, err := "", auth.ErrInvalidAPIKey
	if len(headers) == 1 {
		parts := strings.Fields(headers[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			permission, err = api.auth.AuthenticateAPIKey(r.Context(), parts[1])
		}
	}
	if errors.Is(err, auth.ErrInvalidAPIKey) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, http.StatusUnauthorized)
		return false
	}
	if err != nil {
		api.internalError(w, r, "authenticate API key", err)
		return false
	}
	// Metrics POSTs evaluate queries and require the same read permission as GET.
	path := r.URL.Path
	metricsRead := r.Method == http.MethodPost &&
		(path == "/api/v1/metrics/query" || path == "/api/v1/metrics/query_range")
	if permission == "read" && r.Method != http.MethodGet && r.Method != http.MethodHead && !metricsRead {
		fail(w, http.StatusForbidden)
		return false
	}
	return true
}

func protectOrigins(protection *http.CrossOriginProtection) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := protection.Check(r); err != nil {
				fail(w, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

const sessionCookieName = "arveld_session"

// A private key type prevents collisions with context values owned by other packages.
type administratorContextKey struct{}

// SessionStore is the session work HTTP consumes; auth owns token and storage formats.
// The caller owns persistence and its cleanup lifecycle.
type SessionStore interface {
	Find(ctx context.Context, token string) (auth.Session, bool, error)
	Rotate(ctx context.Context, previous string, verified auth.VerifiedCredentials) (auth.Session, error)
	Delete(ctx context.Context, token string) error
}

// browserSessions owns the HTTP cookie; the store owns session validity and rotation.
type browserSessions struct {
	store  SessionStore
	secure bool
}

func (browser *browserSessions) load(r *http.Request) (auth.Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return auth.Session{}, nil
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("read session cookie: %w", err)
	}
	session, _, err := browser.store.Find(r.Context(), cookie.Value)
	if err != nil {
		return auth.Session{}, fmt.Errorf("load browser session: %w", err)
	}
	return session, nil
}

func (browser *browserSessions) start(w http.ResponseWriter, r *http.Request, verified auth.VerifiedCredentials) error {
	previous, err := browser.load(r)
	if err != nil {
		return err
	}
	session, err := browser.store.Rotate(r.Context(), previous.Token, verified)
	if err != nil {
		return fmt.Errorf("start browser session: %w", err)
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the explicit session_cookie_secure setting; HttpOnly and SameSite remain enforced.
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		MaxAge:   int(auth.SessionLifetime / time.Second),
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   browser.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (browser *browserSessions) end(w http.ResponseWriter, r *http.Request) error {
	session, err := browser.load(r)
	if err != nil {
		return err
	}
	if err := browser.store.Delete(r.Context(), session.Token); err != nil {
		return fmt.Errorf("end browser session: %w", err)
	}
	browser.expireCookie(w)
	return nil
}

// expireCookie follows successful revocation without writing another session.
func (browser *browserSessions) expireCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the explicit session_cookie_secure setting; HttpOnly and SameSite remain enforced.
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   browser.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (api *api) browserAdministrator(w http.ResponseWriter, r *http.Request) (auth.Administrator, bool) {
	// Reuse the credential snapshot authenticated by middleware for this request.
	value := r.Context().Value(administratorContextKey{})
	if administrator, ok := value.(auth.Administrator); ok {
		return administrator, true
	}
	session, err := api.browser.load(r)
	if err != nil {
		api.internalError(w, r, "load browser session", err)
		return auth.Administrator{}, false
	}
	if !session.Administrator {
		fail(w, http.StatusUnauthorized)
		return auth.Administrator{}, false
	}
	administrator, err := api.auth.Administrator(r.Context())
	if errors.Is(err, auth.ErrAdministratorNotFound) {
		fail(w, http.StatusUnauthorized)
		return administrator, false
	}
	if err != nil {
		api.internalError(w, r, "read session administrator", err)
		return administrator, false
	}
	return administrator, true
}
