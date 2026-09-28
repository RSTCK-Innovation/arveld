package remoteconfig

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestReconcileAgentPreservesExistingConfiguration(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	first, err := store.Save(t.Context(), uid, []byte("first explicit configuration"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(t.Context(), uid, []byte("second explicit configuration"))
	if err != nil {
		t.Fatal(err)
	}
	// A caller may have observed an absent assignment before an explicit Save won the race.
	for range 2 {
		if err := store.ReconcileAgent(t.Context(), uid); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if desired.Number != second.Number || desired.ConfigHash != second.ConfigHash {
		t.Fatal("reconciliation overwrote an existing assignment")
	}
	previous, err := store.Rollback(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Number != first.Number || previous.ConfigHash != first.ConfigHash {
		t.Fatal("reconciliation changed the explicit rollback history")
	}
}

func TestReconcileAgentRecordsUnchangedOutputWithoutRewritingHistory(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	current := configuration.NewSpecification(uid)
	content, err := configuration.Compile(current)
	if err != nil {
		t.Fatal(err)
	}
	previous := current
	previous.BaseVersion = 0
	encoded, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	seedManagedBase(t, db, uid, content, string(encoded))
	store := NewStore(db)
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || desired.Number != 1 || !bytes.Equal(desired.Specification, encoded) {
		t.Fatalf("identical output rewrote its historical revision: %+v, %v", desired, err)
	}
	// A failed target cannot be silently selected again under unchanged inputs.
	// Reconciliation must keep the failed target and its completed inputs.
	if err := store.RecordStatus(t.Context(), uid, StatusReport{
		ConfigHash: desired.ConfigHash[:], Status: ApplyStatusFailed,
		ErrorMessage: "test rejection", ReportedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// Reject further publication: already checked inputs need no additional write.
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_republication
		BEFORE INSERT ON agent_config_revisions BEGIN SELECT RAISE(ABORT, 'unexpected publication'); END`); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(db).ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatalf("unchanged output was left pending: %v", err)
	}
}

func TestReconcileAgentKeepsFailedBaseSuppressed(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	const previous = `{"schema_version":1,"base_version":0,"instance_uid":"01000000-0000-0000-0000-000000000000"}`
	seedManagedBase(t, db, uid, []byte("old base"), previous)
	store := NewStore(db)
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordStatus(t.Context(), uid, StatusReport{
		ConfigHash: updated.ConfigHash[:], Status: ApplyStatusFailed,
		ErrorMessage: "test rejection", ReportedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := NewStore(db).ReconcileAgent(t.Context(), uid); err != nil {
			t.Fatal(err)
		}
	}
	status, err := store.Status(t.Context(), uid)
	if err != nil || status.Desired.Number != updated.Number || status.State != ApplyStatusFailed {
		t.Fatalf("reconciliation changed the failed target: %+v, %v", status, err)
	}
	failure, err := store.LatestFailure(t.Context(), uid)
	if err != nil || failure.Revision == nil || *failure.Revision != updated.Number {
		t.Fatalf("reconciliation lost the failed target: %+v, %v", failure, err)
	}
}

func TestReconcileAgentPublishesAtomically(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	const previous = `{"schema_version":1,"base_version":0,"instance_uid":"01000000-0000-0000-0000-000000000000"}`
	seedManagedBase(t, db, uid, []byte("old base"), previous)
	store := NewStore(db)
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_assignment
		BEFORE UPDATE ON agent_config_assignments BEGIN SELECT RAISE(ABORT, 'test assignment failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAgent(t.Context(), uid); err == nil || !strings.Contains(err.Error(), "test assignment failure") {
		t.Fatalf("failed assignment returned %v", err)
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || desired.Number != 1 || string(desired.Content) != "old base" || string(desired.Specification) != previous {
		t.Fatalf("failed publication changed the previous target: %+v, %v", desired, err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_assignment"); err != nil {
		t.Fatal(err)
	}
	next, err := store.Save(t.Context(), uid, []byte("different explicit configuration"))
	if err != nil || next.Number != 2 {
		t.Fatalf("failed transaction leaked an orphan revision: %+v, %v", next, err)
	}
}

func TestReconcileAgentPreservesConcurrentExplicitSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	otherDB := testutil.OpenDatabase(t, path)
	uid := agent.InstanceUID{1}
	const previous = `{"schema_version":1,"base_version":0,"instance_uid":"01000000-0000-0000-0000-000000000000"}`
	seedManagedBase(t, db, uid, []byte("old base"), previous)
	store, other := NewStore(db), NewStore(otherDB)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- store.ReconcileAgent(t.Context(), uid)
	}()
	go func() {
		<-start
		_, err := other.Save(t.Context(), uid, []byte("explicit configuration"))
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := store.ReconcileAgent(t.Context(), uid); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || string(desired.Content) != "explicit configuration" {
		t.Fatalf("reconciliation overwrote a concurrent explicit save: %+v, %v", desired, err)
	}
}

func TestReconcileAgentRejectsUnsupportedStoredInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*configuration.Specification)
	}{
		{name: "future schema", change: func(s *configuration.Specification) { s.SchemaVersion++ }},
		{name: "future base", change: func(s *configuration.Specification) { s.BaseVersion++ }},
		{name: "wrong Agent", change: func(s *configuration.Specification) { s.InstanceUID = agent.InstanceUID{2}.String() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
			uid := agent.InstanceUID{1}
			previous := configuration.NewSpecification(uid)
			test.change(&previous)
			encoded, err := json.Marshal(previous)
			if err != nil {
				t.Fatal(err)
			}
			seedManagedBase(t, db, uid, []byte("keep current artifact"), string(encoded))
			store := NewStore(db)
			if err := store.ReconcileAgent(t.Context(), uid); err == nil {
				t.Fatal("unsupported stored inputs were silently replaced")
			}
			desired, err := store.Desired(t.Context(), uid)
			if err != nil || desired.Number != 1 || !bytes.Equal(desired.Specification, encoded) || string(desired.Content) != "keep current artifact" {
				t.Fatalf("rejected reconciliation changed the persisted target: %+v, %v", desired, err)
			}
		})
	}
}

func seedManagedBase(t *testing.T, db *sql.DB, uid agent.InstanceUID, content []byte, specification string) {
	t.Helper()
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO agent_config_revisions (instance_uid, revision, config_hash, content, specification)
		 VALUES (?, 1, ?, ?, ?)`, uid[:], hash[:], content, specification,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO agent_config_assignments (instance_uid, desired_revision, reconciled_specification)
		 VALUES (?, 1, ?)`, uid[:], specification,
	); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileEnablesHTTPMetricsForExistingMonitors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	const previous = `{"schema_version":1,"base_version":1,"instance_uid":"01000000-0000-0000-0000-000000000000","http_monitors":[{"id":"homepage","endpoint":"https://example.com","method":"GET","interval_seconds":30,"timeout_seconds":5}]}`
	seedManagedBase(t, db, uid, []byte("legacy HTTP configuration"), previous)
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid, Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	for range 2 {
		if err := store.ReconcileAgent(t.Context(), uid); err != nil {
			t.Fatal(err)
		}
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || desired.Number != 2 || !strings.Contains(string(desired.Content), "httpcheck.dns.lookup.duration") {
		t.Fatalf("existing Monitor metrics upgrade = %+v, %v", desired, err)
	}
	old, err := store.RevisionContent(t.Context(), uid, 1)
	if err != nil || string(old) != "legacy HTTP configuration" {
		t.Fatal("upgrade changed immutable history")
	}
}
