package integration

import (
	"bytes"
	"crypto/sha256"
	"os"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerReoffersReselectedFailedConfigurationOnSameConnection(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	store := remoteconfig.NewStore(db)
	uid := agent.InstanceUID{11}
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	send := func() *protobufs.ServerToAgent {
		t.Helper()
		writeOpAMPWebSocket(t, connection, message)
		return readOpAMPWebSocket(t, connection, uid)
	}
	initial := send().GetRemoteConfig()
	if initial == nil {
		t.Fatal("missing initial configuration")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: initial.GetConfigHash(),
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
		ErrorMessage:         "runtime startup failed",
	}
	if send().GetRemoteConfig() != nil {
		t.Fatal("failed target was automatically retried")
	}
	message.RemoteConfigStatus = nil
	if send().GetRemoteConfig() != nil {
		t.Fatal("compressed status automatically retried the failed target")
	}
	if _, err := store.Save(t.Context(), uid, []byte("receivers:\n  nop: {}\n")); err != nil {
		t.Fatal(err)
	}
	selected, err := store.Rollback(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selected.ConfigHash[:], initial.GetConfigHash()) {
		t.Fatal("rollback did not reselect the failed configuration")
	}
	status, err := store.Status(t.Context(), uid)
	if err != nil || status.State != remoteconfig.ApplyStatusApplying {
		t.Fatalf("reselected target status = %s, error %v, want applying", status.State, err)
	}
	offered := send().GetRemoteConfig()
	if offered == nil || !bytes.Equal(offered.GetConfigHash(), selected.ConfigHash[:]) {
		t.Fatal("reselected failed configuration was not offered on the existing connection")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: selected.ConfigHash[:],
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING,
	}
	if send().GetRemoteConfig() != nil {
		t.Fatal("applying acknowledgement caused another offer")
	}
	message.RemoteConfigStatus = nil
	if send().GetRemoteConfig() != nil {
		t.Fatal("compressed status retained the obsolete failure after acknowledgement")
	}
}

func TestControllerAssociatesInitialAppliedBaseWithWorkingRevision(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	store := remoteconfig.NewStore(db)
	// The Agent retained this configuration while the controller lost its state.
	uid := agent.InstanceUID{0x12, 15: 0xab}
	content, err := os.ReadFile("../../internal/configuration/testdata/base-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hash[:],
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	}
	for range 3 {
		writeOpAMPWebSocket(t, connection, message)
		if readOpAMPWebSocket(t, connection, uid).GetRemoteConfig() != nil {
			t.Fatal("already applied base was offered again")
		}
		status, err := store.Status(t.Context(), uid)
		if err != nil {
			t.Fatal(err)
		}
		if status.State != remoteconfig.ApplyStatusApplied || status.Desired.Number != 1 || status.Desired.ConfigHash != hash {
			t.Fatalf("initial base status = %+v, want applied revision 1", status)
		}
		working := status.LastWorking
		if working == nil || working.Revision == nil || *working.Revision != 1 ||
			working.Status != remoteconfig.ApplyStatusApplied || !bytes.Equal(working.ConfigHash, hash[:]) {
			t.Fatalf("initial APPLIED report did not establish the working revision: %+v", working)
		}
		message.RemoteConfigStatus = nil
	}
}
