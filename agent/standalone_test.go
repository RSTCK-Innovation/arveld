package agent_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestStandaloneAgentBootstrapsCollectorBeforeConnecting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the first standalone Agent slice targets Unix process signals")
	}
	executable := buildStandaloneAgent(t)

	received := make(chan *http.Request, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case received <- r.Clone(r.Context()):
		default:
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer endpoint.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable) //nolint:gosec // Runs only the executable built above, without a shell.
	command.Dir = t.TempDir()
	command.Env = []string{
		"PATH=",
		"ARVELD_URL=" + endpoint.URL + "/arveld/",
		"ARVELD_AGENT_TOKEN=standalone-test-token",
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	select {
	case request := <-received:
		if request.Method != http.MethodGet || request.URL.Path != "/arveld/v1/opamp" {
			t.Errorf("OpAMP request = %s %s, want GET /arveld/v1/opamp", request.Method, request.URL.Path)
		}
		if request.Header.Get("Upgrade") != "websocket" {
			t.Error("Agent must initiate an OpAMP WebSocket connection")
		}
		if request.Header.Get("Authorization") != "Bearer standalone-test-token" {
			t.Error("Agent must authenticate with the supplied token")
		}
		if err := command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Errorf("stop Agent: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Agent shutdown: %v\n%s", err, output.String())
		}
	case err := <-done:
		t.Fatalf("Agent exited before connecting: %v\n%s", err, output.String())
	}
}

func TestStandaloneAgentExposesCollectorDiagnostics(t *testing.T) {
	executable := buildStandaloneAgent(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "collector", "validate", "--config=env:ARVELD_TEST_CONFIG") //nolint:gosec // Locally built executable and fixed diagnostic arguments, without a shell.
	command.Dir = t.TempDir()
	command.Env = []string{
		"PATH=",
		"ARVELD_TEST_CONFIG=receivers:\n  nop: {}\nexporters:\n  nop: {}\nservice:\n  pipelines:\n    metrics:\n      receivers: [nop]\n      exporters: [nop]\n",
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("validate Collector configuration without controller credentials: %v\n%s", err, output)
	}
}

func buildStandaloneAgent(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "arveld-agent")
	arguments := []string{"build", "-tags=grpcnotrace", "-o", executable}
	if os.Getenv("CGO_ENABLED") == "1" {
		arguments = append(arguments, "-race")
	}
	arguments = append(arguments, "./cmd/arveld-agent")
	build := exec.CommandContext(t.Context(), "go", arguments...) //nolint:gosec // Fixed local build, with a test-owned output path and no shell.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build standalone Agent: %v\n%s", err, output)
	}
	return executable
}

func TestStandaloneAgentVersionRequiresNoCredentialsOrState(t *testing.T) {
	executable := buildStandaloneAgent(t)
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--version") //nolint:gosec // Test-built executable and fixed arguments, without a shell.
	command.Dir = directory
	command.Env = []string{"PATH="}
	if output, err := command.CombinedOutput(); err != nil || string(output) != "arveld-agent dev\n" {
		t.Fatalf("version without credentials = %q, %v", output, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version created state: %v, %v", entries, err)
	}
}
