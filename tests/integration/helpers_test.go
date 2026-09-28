package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

// Account scenarios need the real stores and router, without protocol servers.
func newAccountHandler(db *sql.DB) http.Handler {
	return httpapi.NewHandler(httpapi.Config{SecureCookie: true}, accountDependencies(db))
}

func accountDependencies(db *sql.DB) httpapi.Dependencies {
	return httpapi.Dependencies{
		AgentKeys:     agentauth.NewStore(db),
		Monitors:      monitor.NewStore(db),
		AlertRules:    alert.NewStore(db),
		Notifications: notification.NewStore(db),
		Agents:        agent.NewStore(db), Configs: remoteconfig.NewStore(db), Auth: auth.NewStore(db),
		Sessions: auth.NewSessionStore(db), Logger: testLogger(),
		CheckReadiness: func(context.Context) httpapi.DependencyReadiness { return httpapi.DependencyReadiness{} },
		// These scenarios exercise management without an OpAMP transport.
		NotifyAgentConfig: func(context.Context, agent.InstanceUID) error { return nil },
	}
}

func createAdministrator(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := auth.NewStore(db).CreateAdministrator(t.Context(), auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "a long password for testing",
	}); err != nil {
		t.Fatalf("create administrator fixture: %v", err)
	}
}

func createAgentKey(t *testing.T, db *sql.DB) string {
	t.Helper()
	_, token, err := agentauth.NewStore(db).CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Test agent"})
	if err != nil {
		t.Fatalf("create agent key: %v", err)
	}
	return token
}

func login(t *testing.T, handler http.Handler, password string) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": "camille@example.com", "password": password})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return loginCookie(t, response)
}

func loginCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("login response = %d %q, want empty 204", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Error("successful login must not be cached")
	}
	result := response.Result()
	defer func() {
		if err := result.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	cookies := result.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "arveld_session" || cookies[0].Value == "" {
		t.Fatal("login must issue exactly one nonempty arveld_session cookie")
	}
	return cookies[0]
}

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// sessionCookie persists a session fixture, including deliberately invalid identities.
func sessionCookie(t *testing.T, db *sql.DB, administratorID int, lifetime time.Duration) *http.Cookie {
	t.Helper()
	token := rand.Text()
	digest := sha256.Sum256([]byte(token))
	data, err := json.Marshal(struct {
		AdministratorID int `json:"administrator_id"`
	}{administratorID})
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately allow invalid identities and expirations in authentication fixtures.
	if _, err := db.ExecContext(t.Context(), "INSERT INTO sessions (token, data, expiry_ns) VALUES (?, ?, ?)", hex.EncodeToString(digest[:]), data, time.Now().Add(lifetime).UnixNano()); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "arveld_session", Value: token} //nolint:gosec // Request fixture; attributes belong to Set-Cookie responses.
}
