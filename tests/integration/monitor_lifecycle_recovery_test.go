package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMonitorLifecycleRecoversDurableChangesAfterRestart(t *testing.T) {
	for _, test := range []struct {
		protocol         string
		initial, changed map[string]any
	}{
		{"http", map[string]any{"endpoint": "https://example.com/old", "method": "GET"}, map[string]any{"endpoint": "https://example.com/new", "method": "HEAD"}},
		{"tcp", map[string]any{"endpoint": "localhost:5432"}, map[string]any{"endpoint": "localhost:6432"}},
		{"icmp", map[string]any{"endpoint": "127.0.0.1", "ping_count": 3}, map[string]any{"endpoint": "127.0.0.2", "ping_count": 2}},
		{"dns", map[string]any{"endpoint": "example.com", "dns_server": "1.1.1.1:53", "record_type": "A", "transport": "udp"}, map[string]any{"endpoint": "example.net", "dns_server": "8.8.8.8:53", "record_type": "AAAA", "transport": "tcp"}},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			config := controllerConfig(t, "http://127.0.0.1:1")
			db := testutil.OpenDatabase(t, config.DatabasePath)
			createAdministrator(t, db)
			token := createAgentKey(t, db)
			url, stop := startController(t, config)
			cookie := loginController(t, url)
			oldUID, newUID := agent.InstanceUID{1}, agent.InstanceUID{2}
			message := func(uid agent.InstanceUID) *protobufs.AgentToServer {
				return &protobufs.AgentToServer{InstanceUid: uid[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig)}
			}
			sendAgentMessage(t, url, token, message(oldUID))
			newBase := sendAgentMessage(t, url, token, message(newUID)).GetRemoteConfig()
			retained := createControllerHTTPMonitor(t, url, cookie, oldUID)
			created := createControllerMonitor(t, url, cookie, oldUID, test.protocol, test.initial)
			configs := remoteconfig.NewStore(db)
			before, err := configs.Desired(t.Context(), oldUID)
			if err != nil {
				t.Fatal(err)
			}
			blockPublication := func() {
				t.Helper()
				if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_lifecycle_publication BEFORE INSERT ON agent_config_revisions BEGIN SELECT RAISE(ABORT, 'test publication failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			restart := func() {
				t.Helper()
				stop()
				if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_lifecycle_publication"); err != nil {
					t.Fatal(err)
				}
				url, stop = startController(t, config)
			}
			blockPublication()
			input := maps.Clone(test.changed)
			maps.Copy(input, map[string]any{"name": "Moved service", "agent_instance_uid": newUID.String(), "interval_seconds": 60, "timeout_seconds": 10})
			status, body := requestMonitorLifecycle(t, url, cookie, http.MethodPut, created.ID, input)
			if status != http.StatusOK {
				t.Fatalf("update during publication failure = %d %s", status, body)
			}
			var updated map[string]any
			if err := json.Unmarshal(body, &updated); err != nil {
				t.Fatal(err)
			}
			for key, expected := range input {
				// JSON numbers decode to float64; compare their encoded values instead.
				gotJSON, err := json.Marshal(updated[key])
				if err != nil {
					t.Fatal(err)
				}
				wantJSON, err := json.Marshal(expected)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(gotJSON, wantJSON) {
					t.Fatalf("updated %s = %s, want %s", key, gotJSON, wantJSON)
				}
			}
			desired, err := configs.Desired(t.Context(), oldUID)
			if err != nil || desired.ConfigHash != before.ConfigHash {
				t.Fatal("failed publication replaced the previous target")
			}
			restart()
			oldTarget := sendAgentMessage(t, url, token, message(oldUID)).GetRemoteConfig()
			oldContent := string(oldTarget.GetConfig().GetConfigMap()[""].GetBody())
			if strings.Contains(oldContent, "metrics/monitor_"+created.ID) || !strings.Contains(oldContent, "http_check/"+retained.ID) {
				t.Fatal("reassignment removed an unrelated Monitor or retained the moved one")
			}
			newTarget := sendAgentMessage(t, url, token, message(newUID)).GetRemoteConfig()
			newContent := string(newTarget.GetConfig().GetConfigMap()[""].GetBody())
			endpoint, ok := test.changed["endpoint"].(string)
			if !ok {
				t.Fatal("test endpoint must be a string")
			}
			if !strings.Contains(newContent, "metrics/monitor_"+created.ID) || !strings.Contains(newContent, endpoint) || !strings.Contains(newContent, "hostmetrics:") {
				t.Fatal("reconnected destination did not receive updated settings and base")
			}
			// Display-name edits and repeated identical writes must not reload an Agent.
			input["name"] = "Renamed service"
			for range 2 {
				status, body = requestMonitorLifecycle(t, url, cookie, http.MethodPut, created.ID, input)
				if status != http.StatusOK {
					t.Fatalf("rename = %d %s", status, body)
				}
			}
			desired, err = configs.Desired(t.Context(), newUID)
			if err != nil || !bytes.Equal(desired.ConfigHash[:], newTarget.GetConfigHash()) {
				t.Fatal("display-only edit changed compiled bytes")
			}
			history, err := configs.ListRevisions(t.Context(), newUID)
			if err != nil || len(history) != 2 {
				t.Fatalf("no-op updates created revisions: %+v, %v", history, err)
			}
			blockPublication()
			status, body = requestMonitorLifecycle(t, url, cookie, http.MethodDelete, created.ID, nil)
			if status != http.StatusNoContent || len(body) != 0 {
				t.Fatalf("delete during publication failure = %d %s", status, body)
			}
			restart()
			removed := sendAgentMessage(t, url, token, message(newUID)).GetRemoteConfig()
			if removed == nil || !bytes.Equal(removed.GetConfigHash(), newBase.GetConfigHash()) {
				t.Fatal("restart did not recover durable deletion")
			}
			status, _ = requestMonitorLifecycle(t, url, cookie, http.MethodGet, created.ID, nil)
			if status != http.StatusNotFound {
				t.Fatalf("deleted Monitor after restart = %d", status)
			}
			status, _ = requestMonitorLifecycle(t, url, cookie, http.MethodGet, retained.ID, nil)
			if status != http.StatusOK {
				t.Fatal("deletion affected another Monitor")
			}
		})
	}
}

func requestMonitorLifecycle(t *testing.T, url string, cookie *http.Cookie, method, id string, input map[string]any) (int, []byte) {
	t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	request, err := http.NewRequestWithContext(t.Context(), method, url+"/api/v1/monitors/"+id, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	body, err = io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read Monitor response: %v, %v", err, closeErr)
	}
	return response.StatusCode, body
}
