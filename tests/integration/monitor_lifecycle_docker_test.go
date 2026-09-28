//go:build docker

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestSupervisorAppliesMonitorLifecycle(t *testing.T) {
	var firstProbes, secondProbes atomic.Int64
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			firstProbes.Add(1)
		case "/second":
			secondProbes.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	var listen net.ListenConfig
	listener, err := listen.Listen(t.Context(), "tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	endpoint.Listener = listener
	endpoint.Start()
	t.Cleanup(endpoint.Close)
	config := controllerConfig(t, endpoint.URL)
	config.HTTPAddress = "0.0.0.0:0"
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	agents, configs := agent.NewStore(db), remoteconfig.NewStore(db)

	for range 2 {
		container := "arveld-lifecycle-test-" + rand.Text()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		output, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", container, //nolint:gosec // Test-owned arguments, no shell interpolation.
			"--add-host", "host.docker.internal:host-gateway",
			"--env", "ARVELD_AGENT_TOKEN="+token, "--env", "ARVELD_URL="+dockerHostURL(t, url),
			"--volume", "/:/hostfs:ro", "--volume", "/var/lib/arveld-agent", "arveld-agent:0.161.0-arveld").CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("start lifecycle Agent: %v\n%s", err, output)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if t.Failed() {
				logs, err := exec.CommandContext(ctx, "docker", "logs", "--tail", "100", container).CombinedOutput() //nolint:gosec // Generated test container name.
				t.Logf("Supervisor logs (error %v):\n%s", err, logs)
			}
			if output, err := exec.CommandContext(ctx, "docker", "rm", "--force", "--volumes", container).CombinedOutput(); err != nil { //nolint:gosec // Generated test container name.
				t.Errorf("remove lifecycle Agent: %v\n%s", err, output)
			}
		})
	}
	var oldUID, newUID agent.InstanceUID
	awaitSupervisor(t, "both initial managed configurations", func() bool {
		values, err := agents.List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 2 {
			return false
		}
		oldUID, newUID = values[0].InstanceUID, values[1].InstanceUID
		for _, uid := range []agent.InstanceUID{oldUID, newUID} {
			status, err := configs.Status(t.Context(), uid)
			if err != nil || status.State != remoteconfig.ApplyStatusApplied {
				return false
			}
		}
		return true
	})
	oldBase, err := configs.Desired(t.Context(), oldUID)
	if err != nil {
		t.Fatal(err)
	}
	newBase, err := configs.Desired(t.Context(), newUID)
	if err != nil {
		t.Fatal(err)
	}
	created := createControllerMonitor(t, url, cookie, oldUID, "http", map[string]any{
		"endpoint": dockerHostURL(t, endpoint.URL) + "/first", "method": "GET", "interval_seconds": 10, "timeout_seconds": 2,
	})
	awaitSupervisor(t, "original Monitor execution", func() bool { return firstProbes.Load() > 0 })
	status, body := requestMonitorLifecycle(t, url, cookie, http.MethodPut, created.ID, map[string]any{
		"name": "Transferred service", "agent_instance_uid": newUID.String(), "endpoint": dockerHostURL(t, endpoint.URL) + "/second",
		"method": "HEAD", "interval_seconds": 10, "timeout_seconds": 2,
	})
	if status != http.StatusOK {
		t.Fatalf("reassign real Monitor = %d %s", status, body)
	}
	awaitSupervisor(t, "old Agent removal and destination execution", func() bool {
		old, err := configs.Status(t.Context(), oldUID)
		if err != nil {
			t.Fatal(err)
		}
		next, err := configs.Status(t.Context(), newUID)
		if err != nil {
			t.Fatal(err)
		}
		return old.State == remoteconfig.ApplyStatusApplied && old.Desired.ConfigHash == oldBase.ConfigHash &&
			next.State == remoteconfig.ApplyStatusApplied && secondProbes.Load() > 0
	})
	firstCount, secondCount := firstProbes.Load(), secondProbes.Load()
	awaitSupervisor(t, "destination periodic execution", func() bool { return secondProbes.Load() > secondCount })
	if firstProbes.Load() != firstCount {
		t.Fatal("old Agent continued the reassigned Monitor")
	}
	status, body = requestMonitorLifecycle(t, url, cookie, http.MethodDelete, created.ID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete real Monitor = %d %s", status, body)
	}
	awaitSupervisor(t, "destination base restored after deletion", func() bool {
		status, err := configs.Status(t.Context(), newUID)
		if err != nil {
			t.Fatal(err)
		}
		return status.State == remoteconfig.ApplyStatusApplied && bytes.Equal(status.Desired.Content, newBase.Content)
	})
	secondCount = secondProbes.Load()
	// Observe longer than the ten-second interval; APPLIED alone does not prove a probe stopped.
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	case <-timer.C:
	}
	if firstProbes.Load() != firstCount || secondProbes.Load() != secondCount {
		t.Fatal("a deleted Monitor continued executing")
	}
}
