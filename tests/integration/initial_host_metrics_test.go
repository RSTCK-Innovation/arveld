package integration

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerAssignsInitialHostMetricsConfiguration(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, stop := startController(t, config)
	uid := agent.InstanceUID{1}
	message := &protobufs.AgentToServer{
		InstanceUid:  uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	}

	initial := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	if initial == nil {
		t.Fatal("first OpAMP response has no initial host metrics configuration")
	}
	content := initial.GetConfig().GetConfigMap()[""].GetBody()
	for _, required := range []string{
		"hostmetrics:", `root_path: "${env:ARVELD_HOST_ROOT:-/hostfs}"`, "cpu:", "memory:", "filesystem:", "system:",
		"system.uptime:\n            enabled: true",
		"system.memory.limit:\n            enabled: true",
		"system.linux.memory.available:\n            enabled: true",
		"system.filesystem.usage:\n            enabled: true",
		"system.filesystem.inodes.usage:\n            enabled: false",
		"network:",
		"system.network.io:\n            enabled: true",
		"system.network.packets:\n            enabled: true",
		"system.network.errors:\n            enabled: true",
		"system.network.dropped:\n            enabled: true",
		"system.network.connections:\n            enabled: false",
		uid.String(), "${env:ARVELD_AGENT_TOKEN}", "${env:ARVELD_URL}",
	} {
		if !bytes.Contains(content, []byte(required)) {
			t.Errorf("initial host metrics configuration is missing %q", required)
		}
	}
	if bytes.Contains(content, []byte(token)) {
		t.Fatal("initial configuration contains the actual agent key")
	}
	hash := sha256.Sum256(content)
	store := remoteconfig.NewStore(db)
	desired, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if desired.Number != 1 || desired.ConfigHash != hash || !bytes.Equal(initial.GetConfigHash(), hash[:]) {
		t.Fatal("first response does not match the persisted initial revision")
	}

	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{LastRemoteConfigHash: hash[:]}
	if sendAgentMessage(t, url, token, message).GetRemoteConfig() != nil {
		t.Fatal("an acknowledged initial configuration was offered again")
	}
	stop()
	url, _ = startController(t, config)
	if sendAgentMessage(t, url, token, message).GetRemoteConfig() != nil {
		t.Fatal("controller restart replaced the initial configuration")
	}

	custom, err := store.Save(t.Context(), uid, []byte("receivers: {nop: {}}"))
	if err != nil {
		t.Fatal(err)
	}
	if custom.Number != 2 {
		t.Fatalf("custom revision = %d, want 2", custom.Number)
	}
	response := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	if !bytes.Equal(response.GetConfigHash(), custom.ConfigHash[:]) {
		t.Fatal("automatic host metrics configuration replaced an explicit configuration")
	}

	// The same reusable key can enroll a second UID with its own configuration.
	otherUID := agent.InstanceUID{2}
	message.InstanceUid = otherUID[:]
	message.RemoteConfigStatus = nil
	other := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	otherContent := string(other.GetConfig().GetConfigMap()[""].GetBody())
	if !strings.Contains(otherContent, otherUID.String()) || strings.Contains(otherContent, uid.String()) {
		t.Fatal("the second agent did not receive its own UID in the host metrics configuration")
	}
}
