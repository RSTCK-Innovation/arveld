package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

func TestResetPasswordCLI(t *testing.T) {
	ctx := t.Context()
	executable := buildCLI(t)
	directory := t.TempDir()
	// A missing command dispatch must fail, never start a controller in this test.
	if err := os.Mkdir(filepath.Join(directory, "data"), 0o700); err != nil {
		t.Fatalf("create fallback config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "data", "arveld.yml"), []byte("unknown_field: true\n"), 0o600); err != nil {
		t.Fatalf("write fallback configuration: %v", err)
	}
	path := filepath.Join(directory, "arveld.db")
	configPath := filepath.Join(directory, "arveld.yml")
	if err := os.WriteFile(configPath, []byte("database_path: "+path+"\n"), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	db := testutil.OpenDatabase(t, path)
	store := auth.NewStore(db)
	const oldPassword = "the previous administrator password"
	const newPassword = "  a new password 🔑  "
	if err := store.CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: oldPassword,
	}); err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	sessions := auth.NewSessionStore(db)
	newHandler := func() http.Handler {
		return httpapi.NewHandler(httpapi.Config{SecureCookie: true}, httpapi.Dependencies{
			Agents: agent.NewStore(db), Configs: remoteconfig.NewStore(db), Auth: store, Sessions: sessions, Logger: logger,
			CheckReadiness: func(context.Context) httpapi.DependencyReadiness { return httpapi.DependencyReadiness{} },
		})
	}
	handler := newHandler()
	login := func(password string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"email": "camille@example.com", "password": password})
		if err != nil {
			t.Fatalf("encode login: %v", err)
		}
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	cookieFrom := func(recorder *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		response := recorder.Result()
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close login response: %v", err)
			}
		}()
		cookies := response.Cookies()
		if response.StatusCode != http.StatusNoContent || len(cookies) != 1 || cookies[0].Value == "" {
			t.Fatalf("login response = %d %q, want a session cookie", response.StatusCode, recorder.Body.String())
		}
		return cookies[0]
	}
	checkSession := func(cookie *http.Cookie, want int) {
		t.Helper()
		for _, path := range []string{"/api/v1/auth/session", "/api/v1/agents"} {
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != want {
				t.Fatalf("%s status = %d, want %d", path, response.Code, want)
			}
		}
	}
	oldCookie := cookieFrom(login(oldPassword))

	checkSession(oldCookie, http.StatusOK)
	arguments := []string{"reset-password", "--config", configPath, "--password-stdin"}
	reset := func(input string, want int) {
		t.Helper()
		code, stdout, stderr := runCLIProcess(t, executable, directory, input, arguments...)
		if code != want {
			t.Fatalf("reset exit = %d, want %d; stdout=%q stderr=%q", code, want, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, oldPassword) || strings.Contains(stdout+stderr, newPassword) {
			t.Fatal("reset output disclosed a password")
		}
		if want == 0 && stdout != "Administrator password reset; existing sessions invalidated.\n" {
			t.Fatalf("reset success output = %q", stdout)
		}
		if want != 0 && stdout != "" {
			t.Fatalf("failed reset reported success: %q", stdout)
		}
	}
	reset(newPassword+"\r\n", 0)
	checkSession(oldCookie, http.StatusUnauthorized)
	handler = newHandler()
	if response := login(oldPassword); response.Code != http.StatusUnauthorized {
		t.Fatalf("old password status = %d, want 401", response.Code)
	}
	time.Sleep(time.Second)
	newCookie := cookieFrom(login(newPassword))
	checkSession(newCookie, http.StatusOK)
	arguments = []string{"--config", configPath, "reset-password", "--password-stdin"}
	reset(newPassword+"\n", 0)
	checkSession(newCookie, http.StatusUnauthorized)
	handler = newHandler()
	newCookie = cookieFrom(login(newPassword))
	checkSession(newCookie, http.StatusOK)
	administrator, err := store.Administrator(ctx)
	if err != nil || administrator.Name != "Camille" || administrator.Email != "camille@example.com" {
		t.Fatalf("reset changed the administrator profile: %v", err)
	}

	for _, input := range []string{"", "too short", strings.Repeat("🔑", 129), "invalid UTF-8: \xff", newPassword + "\nsecond line", strings.Repeat("x", 10000)} {
		reset(input, 2)
		checkSession(newCookie, http.StatusOK)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_session_reset BEFORE DELETE ON sessions
		BEGIN SELECT RAISE(ABORT, 'test reset failure'); END;`); err != nil {
		t.Fatalf("inject reset failure: %v", err)
	}
	reset("another password that must not be saved", 1)
	checkSession(newCookie, http.StatusOK)
	if current, err := store.Administrator(ctx); err != nil || current != administrator {
		t.Fatalf("failed reset changed credentials: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_session_reset; DELETE FROM sessions; DELETE FROM users;"); err != nil {
		t.Fatalf("remove administrator fixture: %v", err)
	}
	reset(newPassword, 1)
	if needed, err := store.NeedsSetup(ctx); err != nil || !needed {
		t.Fatalf("reset created an administrator: %t, %v", needed, err)
	}

	for _, args := range [][]string{
		{"reset-password", "--config", configPath},
		{"reset-password", "--password-stdin", "unexpected"},
		{"reset-password", "--password-stdin", "--config", ""},
		{"--config", "", "reset-password", "--password-stdin"},
		{"unknown-command"},
	} {
		if code, _, _ := runCLIProcess(t, executable, directory, newPassword, args...); code != 2 {
			t.Fatalf("invalid command exit = %d, want 2", code)
		}
	}
	missingDirectory := t.TempDir()
	if code, _, _ := runCLIProcess(t, executable, missingDirectory, newPassword, "reset-password", "--password-stdin"); code != 1 {
		t.Fatalf("missing configuration exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(missingDirectory, "data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reset created default application data: %v", err)
	}
	missingDB := filepath.Join(missingDirectory, "missing.db")
	if err := os.WriteFile(configPath, []byte("database_path: "+missingDB+"\n"), 0o600); err != nil {
		t.Fatalf("write missing-database config: %v", err)
	}
	reset(newPassword, 1)
	if _, err := os.Stat(missingDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reset created a missing database: %v", err)
	}
}

func runCLIProcess(t *testing.T, executable, directory, input string, arguments ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...) //nolint:gosec // Executes the locally built Arveld binary with fixture arguments, without a shell.
	command.Dir = directory
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run CLI subprocess: %v", err)
		}
		return exitError.ExitCode(), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}

func buildCLI(t *testing.T) string {
	t.Helper()
	name := "arveld"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	arguments := []string{"build", "-o", executable}
	if os.Getenv("CGO_ENABLED") == "1" {
		arguments = append(arguments, "-race")
	}
	arguments = append(arguments, "./cmd/arveld")
	command := exec.CommandContext(t.Context(), "go", arguments...) //nolint:gosec // Fixed Go build command and arguments, using only a temporary output path.
	command.Dir = "../.."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return executable
}
