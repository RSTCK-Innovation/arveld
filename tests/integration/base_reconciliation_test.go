package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerReconcilesOlderBaseOnReconnect(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{0x12, 15: 0xab}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}

	// Simulate an earlier release's persisted artifact; never ask today's compiler
	// to invent historical output. Base 3 requires the Docker /hostfs mount.
	const oldSpecification = `{"schema_version":1,"base_version":3,"instance_uid":"12000000-0000-0000-0000-0000000000ab"}`
	oldContent, err := os.ReadFile("../../internal/configuration/testdata/base-v3.yaml")
	if err != nil {
		t.Fatal(err)
	}
	oldHash := sha256.Sum256(oldContent)
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO agent_config_revisions (instance_uid, revision, config_hash, content, specification)
		 VALUES (?, 1, ?, ?, ?)`, uid[:], oldHash[:], oldContent, oldSpecification,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO agent_config_assignments (instance_uid, desired_revision, reconciled_specification)
		 VALUES (?, 1, ?)`, uid[:], oldSpecification,
	); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	historical, err := store.Desired(t.Context(), uid)
	if err != nil || string(historical.Specification) != oldSpecification {
		t.Fatalf("read historical specification: %+v, %v", historical, err)
	}

	url, stop := startController(t, config)
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: oldHash[:], Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	}
	updated := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	if updated == nil {
		t.Fatal("reconnected Agent was not offered the current base")
	}
	wantContent, err := os.ReadFile("../../internal/configuration/testdata/base-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(wantContent)
	if !bytes.Equal(updated.GetConfig().GetConfigMap()[""].GetBody(), wantContent) || !bytes.Equal(updated.GetConfigHash(), wantHash[:]) {
		t.Fatal("reconnected Agent did not receive the independently specified current base")
	}

	desired, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if desired.Number != 2 || desired.ConfigHash != wantHash {
		t.Fatalf("desired revision = %d, want the single published upgrade at revision 2", desired.Number)
	}
	var specification configuration.Specification
	if err := json.Unmarshal(desired.Specification, &specification); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(specification, configuration.NewSpecification(uid)) {
		t.Fatalf("published specification = %+v, want the current Arveld selection", specification)
	}

	// A lost reply is recovered from the same durable revision, including across
	// a controller restart. Once acknowledged, no further offer is necessary.
	stop()
	url, _ = startController(t, config)
	if retry := sendAgentMessage(t, url, token, message).GetRemoteConfig(); !bytes.Equal(retry.GetConfigHash(), wantHash[:]) {
		t.Fatal("restart did not recover the committed target")
	}
	message.RemoteConfigStatus.LastRemoteConfigHash = wantHash[:]
	for range 2 {
		if sendAgentMessage(t, url, token, message).GetRemoteConfig() != nil {
			t.Fatal("an acknowledged current base was offered again")
		}
	}
	after, err := store.Desired(t.Context(), uid)
	if err != nil || after.Number != desired.Number || !bytes.Equal(after.Specification, desired.Specification) {
		t.Fatalf("unchanged inputs changed the persisted target: %+v, %v", after, err)
	}
	status, err := store.Status(t.Context(), uid)
	if err != nil || status.State != remoteconfig.ApplyStatusApplied {
		t.Fatalf("upgraded Agent status = %+v, %v", status, err)
	}
	previous, err := store.Rollback(t.Context(), uid)
	if err != nil || previous.Number != 1 || !bytes.Equal(previous.Content, oldContent) || string(previous.Specification) != oldSpecification {
		t.Fatalf("upgrade changed the historical artifact or specification: %+v, %v", previous, err)
	}
}
