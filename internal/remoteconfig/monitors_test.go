package remoteconfig_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestReconcileAgentPreservesTargetWhenMonitorCompilationFails(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	before, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	// Persistence accepts caller-validated data; simulate an invalid compiler input.
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		Protocol: "http",
		ID:       "invalid/component", Name: "Invalid ID", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: http.MethodGet, IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAgent(t.Context(), uid); err == nil {
		t.Fatal("invalid Monitor compilation succeeded")
	}
	after, err := store.Desired(t.Context(), uid)
	if err != nil || after.Number != before.Number || after.ConfigHash != before.ConfigHash ||
		!bytes.Equal(after.Content, before.Content) || !bytes.Equal(after.Specification, before.Specification) {
		t.Fatalf("failed compilation changed the previous target: %+v, %v", after, err)
	}
	history, err := store.ListRevisions(t.Context(), uid)
	if err != nil || len(history) != 1 {
		t.Fatalf("failed compilation changed history: %+v, %v", history, err)
	}
}

func TestReconcileAgentDoesNotPublishStaleMonitorInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db, otherDB := testutil.OpenDatabase(t, path), testutil.OpenDatabase(t, path)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	// Turn a stale publication attempt into an observable storage failure. All
	// product changes below are inserts, so the assigned count identifies each set.
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_stale_monitor_publication
		BEFORE INSERT ON agent_config_revisions
		WHEN COALESCE(json_array_length(NEW.specification, '$.http_monitors'), 0) +
		    COALESCE(json_array_length(NEW.specification, '$.tcp_monitors'), 0) +
		    COALESCE(json_array_length(NEW.specification, '$.icmp_monitors'), 0) +
		    COALESCE(json_array_length(NEW.specification, '$.dns_monitors'), 0) <>
		    (SELECT COUNT(*) FROM monitors WHERE agent_instance_uid = NEW.instance_uid)
		BEGIN SELECT RAISE(ABORT, 'stale Monitor publication'); END`); err != nil {
		t.Fatal(err)
	}
	const count = 24
	samples := []monitor.Monitor{
		{Protocol: "http", Endpoint: "https://example.com", Method: http.MethodGet},
		{Protocol: "tcp", Endpoint: "example.com:443"},
		{Protocol: "icmp", Endpoint: "127.0.0.1", PingCount: 3},
		{Protocol: "dns", Endpoint: "example.com", DNSServer: "1.1.1.1:53", RecordType: "A", Transport: "udp"},
	}
	completed := make(chan error, 1)
	go func() {
		for index := range count {
			value := samples[index%len(samples)]
			value.ID, value.Name = fmt.Sprintf("monitor-%02d", index), "Concurrent Monitor"
			value.AgentInstanceUID = uid
			value.IntervalSeconds, value.TimeoutSeconds = 30, 5
			err := monitor.NewStore(otherDB).Create(t.Context(), value)
			if err != nil {
				completed <- err
				return
			}
		}
		completed <- nil
	}()
	var reconcileErr error
	for range count {
		if err := store.ReconcileAgent(t.Context(), uid); err != nil && !errors.Is(err, remoteconfig.ErrInputsChanged) {
			reconcileErr = err
			break
		}
	}
	// Join the writer before reporting failure or closing either database pool.
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if reconcileErr != nil {
		t.Fatalf("concurrent reconciliation attempted an invalid publication: %v", reconcileErr)
	}
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	var specification configuration.Specification
	if err := json.Unmarshal(desired.Specification, &specification); err != nil {
		t.Fatal(err)
	}
	if len(specification.HTTPMonitors) != count/4 || len(specification.TCPMonitors) != count/4 ||
		len(specification.ICMPMonitors) != count/4 || len(specification.DNSMonitors) != count/4 {
		t.Fatalf("final target must contain every protocol equally: %+v", specification)
	}
}
