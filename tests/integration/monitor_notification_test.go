package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
	"go.yaml.in/yaml/v3"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerPushesCreatedMonitorToConnectedAgent(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	uid := agent.InstanceUID{1}
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	writeOpAMPWebSocket(t, connection, message)
	base := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	if base == nil {
		t.Fatal("missing initial base")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: base.GetConfigHash(), Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	writeOpAMPWebSocket(t, connection, message)
	if readOpAMPWebSocket(t, connection, uid).GetRemoteConfig() != nil {
		t.Fatal("acknowledged base was offered again")
	}
	created := createControllerHTTPMonitor(t, url, loginController(t, url), uid)
	// No message, poll or reconnect from the Agent: this must be a server push.
	pushed := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	if pushed == nil || bytes.Equal(pushed.GetConfigHash(), base.GetConfigHash()) {
		t.Fatal("connected Agent did not receive its newly created Monitor")
	}
	content := pushed.GetConfig().GetConfigMap()[""].GetBody()
	if !strings.Contains(string(content), "http_check/"+created.ID) || !strings.Contains(string(content), created.Endpoint) {
		t.Fatal("pushed configuration does not contain the created Monitor")
	}
	store := remoteconfig.NewStore(db)
	status, err := store.Status(t.Context(), uid)
	if err != nil || status.Desired.Number != 2 || !bytes.Equal(status.Desired.Content, content) ||
		!bytes.Equal(status.Desired.ConfigHash[:], pushed.GetConfigHash()) || status.State != remoteconfig.ApplyStatusApplying {
		t.Fatalf("pushed target was not durably published before delivery: %+v, %v", status, err)
	}
	message.RemoteConfigStatus.LastRemoteConfigHash = pushed.GetConfigHash()
	writeOpAMPWebSocket(t, connection, message)
	if readOpAMPWebSocket(t, connection, uid).GetRemoteConfig() != nil {
		t.Fatal("acknowledged push was offered again")
	}
	status, err = store.Status(t.Context(), uid)
	if err != nil || status.State != remoteconfig.ApplyStatusApplied || status.Desired.Number != 2 {
		t.Fatalf("push acknowledgement was not recorded: %+v, %v", status, err)
	}
}

func TestMonitorCreationKeepsIntentWhenPublicationFails(t *testing.T) {
	for _, test := range []struct {
		protocol, receiver string
		settings           map[string]any
	}{
		{"http", "http_check", map[string]any{"endpoint": "https://example.com/health", "method": http.MethodGet}},
		{"tcp", "tcp_check", map[string]any{"endpoint": "example.com:443"}},
		{"icmp", "icmpcheckreceiver", map[string]any{"endpoint": "127.0.0.1", "ping_count": 3}},
		{"dns", "dns_check", map[string]any{"endpoint": "example.com", "dns_server": "1.1.1.1:53", "record_type": "A", "transport": "udp"}},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			config := controllerConfig(t, "http://127.0.0.1:1")
			db := testutil.OpenDatabase(t, config.DatabasePath)
			createAdministrator(t, db)
			token := createAgentKey(t, db)
			url, _ := startController(t, config)
			uid := agent.InstanceUID{1}
			message := &protobufs.AgentToServer{
				InstanceUid: uid[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
			}
			base := sendAgentMessage(t, url, token, message).GetRemoteConfig()
			if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_monitor_publication
				BEFORE INSERT ON agent_config_revisions BEGIN SELECT RAISE(ABORT, 'test publication failure'); END`); err != nil {
				t.Fatal(err)
			}
			created := createControllerMonitor(t, url, loginController(t, url), uid, test.protocol, test.settings)
			stored, err := monitor.NewStore(db).Get(t.Context(), created.ID)
			if err != nil || stored.AgentInstanceUID != uid || stored.Endpoint != created.Endpoint || stored.Protocol != test.protocol {
				t.Fatalf("publication failure lost product intent: %+v, %v", stored, err)
			}
			desired, err := remoteconfig.NewStore(db).Desired(t.Context(), uid)
			if err != nil || desired.Number != 1 || !bytes.Equal(desired.ConfigHash[:], base.GetConfigHash()) {
				t.Fatalf("failed publication changed the previous target: %+v, %v", desired, err)
			}
			if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_monitor_publication"); err != nil {
				t.Fatal(err)
			}
			retried := sendAgentMessage(t, url, token, message).GetRemoteConfig()
			if retried == nil || !strings.Contains(string(retried.GetConfig().GetConfigMap()[""].GetBody()), test.receiver+"/"+created.ID) {
				t.Fatal("next Agent poll did not recover the persisted Monitor")
			}
		})
	}
}

func TestMonitorNotificationRechecksRevokedAgentKey(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	keys := agentauth.NewStore(db)
	key, token, err := keys.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Notification recipient"})
	if err != nil {
		t.Fatal(err)
	}
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	uid := agent.InstanceUID{1}
	writeOpAMPWebSocket(t, connection, &protobufs.AgentToServer{
		InstanceUid: uid[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	})
	readOpAMPWebSocket(t, connection, uid)
	if err := keys.RevokeKey(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	created := createControllerHTTPMonitor(t, url, loginController(t, url), uid)
	requireClosedOpAMPWebSocket(t, connection)
	desired, err := remoteconfig.NewStore(db).Desired(t.Context(), uid)
	if err != nil || desired.Number != 2 || !strings.Contains(string(desired.Content), "http_check/"+created.ID) {
		t.Fatalf("revoked recipient lost the committed target: %+v, %v", desired, err)
	}
}

func TestMonitorCreationKeepsCommittedTargetWhenNotificationFails(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	deps := accountDependencies(db)
	notified := make(chan agent.InstanceUID, 1)
	deps.NotifyAgentConfig = func(_ context.Context, recipient agent.InstanceUID) error {
		notified <- recipient
		return errors.New("test notification failure")
	}
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{}, deps))
	t.Cleanup(server.Close)
	cookie := loginController(t, server.URL)
	for _, test := range []struct {
		protocol string
		settings map[string]any
	}{
		{"http", map[string]any{"endpoint": "https://example.com/health", "method": http.MethodGet}},
		{"tcp", map[string]any{"endpoint": "example.com:443"}},
		{"icmp", map[string]any{"endpoint": "127.0.0.1", "ping_count": 3}},
		{"dns", map[string]any{"endpoint": "example.com", "dns_server": "1.1.1.1:53", "record_type": "A", "transport": "udp"}},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			created := createControllerMonitor(t, server.URL, cookie, uid, test.protocol, test.settings)
			select {
			case recipient := <-notified:
				if recipient != uid {
					t.Fatalf("notified Agent = %s, want %s", recipient, uid)
				}
			default:
				t.Fatal("notification failure was not exercised")
			}
			stored, err := deps.Monitors.Get(t.Context(), created.ID)
			if err != nil || stored.Endpoint != created.Endpoint || stored.Protocol != test.protocol {
				t.Fatalf("notification failure lost the definition: %+v, %v", stored, err)
			}
			desired, err := deps.Configs.Desired(t.Context(), uid)
			if err != nil || !strings.Contains(string(desired.Content), "metrics/monitor_"+created.ID) {
				t.Fatalf("notification failure lost the compiled target: %+v, %v", desired, err)
			}
			if err := deps.Configs.ReconcileAgent(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			after, err := deps.Configs.Desired(t.Context(), uid)
			if err != nil || after.Number != desired.Number || after.ConfigHash != desired.ConfigHash {
				t.Fatalf("retry replaced the committed target: %+v, %v", after, err)
			}
		})
	}
}

func TestMonitorNotificationOnlyTargetsCapableAssignedAgents(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	uid, otherUID := agent.InstanceUID{1}, agent.InstanceUID{2}
	connection := dialOpAMPWebSocket(t, url, token)
	other := dialOpAMPWebSocket(t, url, token)
	unsupported := &protobufs.AgentToServer{InstanceUid: uid[:]}
	writeOpAMPWebSocket(t, connection, unsupported)
	readOpAMPWebSocket(t, connection, uid)
	message := &protobufs.AgentToServer{
		InstanceUid: otherUID[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	writeOpAMPWebSocket(t, other, message)
	base := readOpAMPWebSocket(t, other, otherUID).GetRemoteConfig()
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: base.GetConfigHash(), Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	writeOpAMPWebSocket(t, other, message)
	readOpAMPWebSocket(t, other, otherUID)
	createControllerHTTPMonitor(t, url, loginController(t, url), uid)
	// The first queued response must be this acknowledgement, not a proactive offer.
	writeOpAMPWebSocket(t, connection, unsupported)
	if readOpAMPWebSocket(t, connection, uid).GetRemoteConfig() != nil {
		t.Fatal("Agent without remote-configuration capability received a push")
	}
	writeOpAMPWebSocket(t, other, message)
	if readOpAMPWebSocket(t, other, otherUID).GetRemoteConfig() != nil {
		t.Fatal("another Agent's Monitor was pushed to this connection")
	}
}

func TestConcurrentMonitorNotificationsDoNotSendOlderTargetsLast(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	uid := agent.InstanceUID{1}
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	writeOpAMPWebSocket(t, connection, message)
	base := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: base.GetConfigHash(), Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	writeOpAMPWebSocket(t, connection, message)
	readOpAMPWebSocket(t, connection, uid)
	cookie := loginController(t, url)
	t.Run("concurrent creations", func(t *testing.T) {
		for range 3 {
			t.Run("create", func(t *testing.T) {
				t.Parallel()
				createControllerHTTPMonitor(t, url, cookie, uid)
			})
		}
	})
	if t.Failed() {
		t.FailNow()
	}
	desired, err := remoteconfig.NewStore(db).Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	// This acknowledgement follows every completed notification. Drain all pushes
	// before its empty response and require monotonically growing Monitor sets.
	message.RemoteConfigStatus.LastRemoteConfigHash = desired.ConfigHash[:]
	writeOpAMPWebSocket(t, connection, message)
	lastCount := 0
	var lastHash []byte
	for {
		pushed := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
		if pushed == nil {
			break
		}
		var document struct {
			Receivers map[string]any `yaml:"receivers"`
		}
		if err := yaml.Unmarshal(pushed.GetConfig().GetConfigMap()[""].GetBody(), &document); err != nil {
			t.Fatal(err)
		}
		count := 0
		for name := range document.Receivers {
			if strings.HasPrefix(name, "http_check/") {
				count++
			}
		}
		if count < lastCount {
			t.Fatalf("notification moved backwards from %d to %d Monitors", lastCount, count)
		}
		lastCount, lastHash = count, pushed.GetConfigHash()
	}
	if lastCount != 3 || !bytes.Equal(lastHash, desired.ConfigHash[:]) {
		t.Fatal("final notification was not the complete committed Monitor set")
	}
}

type createdMonitor struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
}

func createControllerHTTPMonitor(t *testing.T, url string, cookie *http.Cookie, uid agent.InstanceUID) createdMonitor {
	t.Helper()
	return createControllerMonitor(t, url, cookie, uid, "http", map[string]any{
		"endpoint": "https://example.com/health", "method": http.MethodGet,
	})
}

func createControllerMonitor(t *testing.T, url string, cookie *http.Cookie, uid agent.InstanceUID, protocol string, settings map[string]any) createdMonitor {
	t.Helper()
	input := map[string]any{
		"name": "Live Monitor", "agent_instance_uid": uid.String(),
		"interval_seconds": 30, "timeout_seconds": 5,
	}
	maps.Copy(input, settings)
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/api/v1/monitors/"+protocol, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create %s Monitor = %d, want 201", protocol, response.StatusCode)
	}
	var created createdMonitor
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}
