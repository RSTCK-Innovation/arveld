//go:build docker

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestControllerComposePreservesDataAcrossRecreation(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	composeFile := os.Getenv("ARVELD_TEST_COMPOSE_FILE")
	if composeFile == "" {
		composeFile = filepath.Join(root, "compose.yaml")
	}
	project := "arveld-test-" + strings.ToLower(rand.Text())
	var agentToken string
	compose := func(ctx context.Context, arguments ...string) ([]byte, error) {
		arguments = append([]string{
			"compose", "--project-name", project, "--file", composeFile, "--env-file", os.DevNull, "--profile", "agent",
		}, arguments...)
		command := exec.CommandContext(ctx, "docker", arguments...) //nolint:gosec // Test-owned Compose arguments, without a shell.
		command.Env = append(os.Environ(), "ARVELD_HTTP_PORT=0", "ARVELD_SESSION_COOKIE_SECURE=false", "ARVELD_AGENT_TOKEN="+agentToken)
		return command.CombinedOutput()
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if t.Failed() {
			logs, err := compose(ctx, "logs", "--no-color", "--tail", "100")
			t.Logf("controller logs (%v):\n%s", err, logs)
		}
		if output, err := compose(ctx, "down", "--volumes", "--timeout", "30"); err != nil {
			t.Errorf("remove test-owned Compose project: %v\n%s", err, output)
		}
	})
	run := func(arguments ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		output, err := compose(ctx, arguments...)
		if err != nil {
			t.Fatalf("compose %s: %v\n%s", arguments[0], err, output)
		}
		return strings.TrimSpace(string(output))
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar}
	start := func() string {
		t.Helper()
		run("up", "--detach", "--no-build", "--pull", "never", "controller")
		origin := "http://" + run("port", "controller", "8080")
		waitDockerControllerReady(t, client, origin)
		return origin
	}
	origin := start()
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, origin+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != status {
			t.Fatalf("%s %s = %d, read %v, close %v; want %d\n%s", method, path, response.StatusCode, readErr, closeErr, status, content)
		}
		return content
	}
	if html := request(http.MethodGet, "/", "", http.StatusOK); !bytes.Contains(html, []byte(`<div id="root"></div>`)) {
		t.Fatal("the Docker image must include the built frontend")
	}
	request(http.MethodPost, "/api/v1/auth/setup", `{"name":"Camille","email":"camille@example.com","password":"a long password for testing"}`, http.StatusCreated)
	request(http.MethodPost, "/api/v1/auth/login", `{"email":"camille@example.com","password":"a long password for testing"}`, http.StatusNoContent)
	var created struct {
		Key struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"key"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(request(http.MethodPost, "/api/v1/agentkeys", `{"name":"Persistent Docker Agent"}`, http.StatusCreated), &created); err != nil {
		t.Fatal(err)
	}
	agentToken = created.Token
	run("up", "--detach", "--no-build", "--pull", "never", "agent")
	connectedAgent := func() string {
		t.Helper()
		var inventory struct {
			Agents []struct {
				InstanceUID string `json:"instance_uid"`
				Connected   bool   `json:"connected"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(request(http.MethodGet, "/api/v1/agents", "", http.StatusOK), &inventory); err != nil {
			t.Fatal(err)
		}
		if len(inventory.Agents) > 1 {
			t.Fatal("recreating the Agent must preserve its original identity")
		}
		if len(inventory.Agents) == 1 && inventory.Agents[0].Connected {
			return inventory.Agents[0].InstanceUID
		}
		return ""
	}
	var uid string
	awaitSupervisor(t, "Compose Agent connection", func() bool {
		uid = connectedAgent()
		return uid != ""
	})
	metricValue := func(expression string) (float64, bool) {
		t.Helper()
		var metrics struct {
			Data struct {
				Result []struct {
					Value [2]json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		content := request(http.MethodGet, "/api/v1/metrics/query?"+url.Values{"query": {expression}}.Encode(), "", http.StatusOK)
		if err := json.Unmarshal(content, &metrics); err != nil {
			t.Fatal(err)
		}
		if len(metrics.Data.Result) == 0 {
			return 0, false
		}
		var value string
		if err := json.Unmarshal(metrics.Data.Result[0].Value[1], &value); err != nil {
			t.Fatal(err)
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			t.Fatal(err)
		}
		return number, true
	}
	awaitSupervisor(t, "Compose Agent host metrics", func() bool {
		value, found := metricValue(fmt.Sprintf(`system_uptime_seconds{instance=%q}`, uid))
		return found && value > 0
	})
	monitorBody := request(http.MethodPost, "/api/v1/monitors/http", fmt.Sprintf(`{"name":"Compose health","agent_instance_uid":%q,"endpoint":"http://controller:8080/healthz","method":"GET","interval_seconds":10,"timeout_seconds":2}`, uid), http.StatusCreated)
	var configured struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(monitorBody, &configured); err != nil || configured.ID == "" {
		t.Fatalf("create Monitor: %v\n%s", err, monitorBody)
	}
	selector := fmt.Sprintf(`httpcheck_status{arveld_monitor_id=%q,http_status_code="200"}`, configured.ID)
	awaitSupervisor(t, "Compose Agent HTTP measurement", func() bool {
		value, found := metricValue("sum(" + selector + ")")
		return found && value == 1
	})
	instant := time.Now().UTC().Format(time.RFC3339Nano)
	parameters := url.Values{
		"query": {"sum(" + selector + ")"},
		"start": {instant},
		"end":   {instant},
		"step":  {"1s"},
	}
	query := "/api/v1/metrics/query_range?" + parameters.Encode()
	before := request(http.MethodGet, query, "", http.StatusOK)
	var metrics struct {
		Data struct {
			Result []struct {
				Values [][2]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(before, &metrics); err != nil || len(metrics.Data.Result) != 1 || len(metrics.Data.Result[0].Values) != 1 || string(metrics.Data.Result[0].Values[0][1]) != `"1"` {
		t.Fatalf("stored measurement = %s, %v; want one sample with value 1", before, err)
	}

	// Remove both containers and their network, retaining both named volumes.
	run("down", "--timeout", "30")
	resumedAfter := float64(time.Now().UnixNano()) / 1e9
	origin = start()
	request(http.MethodGet, "/api/v1/auth/session", "", http.StatusOK)
	request(http.MethodPost, "/api/v1/auth/login", `{"email":"camille@example.com","password":"a long password for testing"}`, http.StatusNoContent)
	var listed struct {
		Keys []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(request(http.MethodGet, "/api/v1/agentkeys", "", http.StatusOK), &listed); err != nil || len(listed.Keys) != 1 || listed.Keys[0] != created.Key {
		t.Fatalf("Agent keys after recreation = %+v, %v; want the original key", listed, err)
	}
	if after := request(http.MethodGet, query, "", http.StatusOK); !bytes.Equal(before, after) {
		t.Fatalf("historical measurement changed after recreation:\nbefore: %s\nafter: %s", before, after)
	}
	if after := request(http.MethodGet, "/api/v1/monitors/http/"+configured.ID, "", http.StatusOK); !bytes.Equal(monitorBody, after) {
		t.Fatalf("Monitor configuration changed after recreation: %s", after)
	}
	run("up", "--detach", "--no-build", "--pull", "never", "agent")
	awaitSupervisor(t, "Compose Agent identity and HTTP execution after recreation", func() bool {
		if connectedAgent() != uid {
			return false
		}
		value, found := metricValue("max(timestamp(" + selector + "))")
		return found && value > resumedAfter
	})
}

func waitDockerControllerReady(t *testing.T, client *http.Client, origin string) {
	t.Helper()
	// An empty volume downloads the two pinned upstream engines before readiness.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err == nil {
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("Docker controller and its managed engines did not become ready")
		case <-ticker.C:
		}
	}
}
