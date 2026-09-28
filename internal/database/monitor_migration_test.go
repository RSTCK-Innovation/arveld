package database

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

func TestMigrateUnifiesMonitorsWithoutChangingDefinitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := openMonitorMigrationFixture(t, path)
	for range 2 {
		if err := Migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := OpenExistingFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	otherUID, err := agent.ParseInstanceUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	// Compare every persisted field, including assignment and inactive settings.
	want := []monitor.Monitor{
		{ID: "dns", Name: "DNS", Protocol: "dns", AgentInstanceUID: otherUID, Endpoint: "example.com", IntervalSeconds: 60, TimeoutSeconds: 4, DNSServer: "1.1.1.1:53", RecordType: "AAAA", Transport: "tcp"},
		{ID: "http", Name: "HTTP", Protocol: "http", Endpoint: "https://example.com/$value", Method: "HEAD", IntervalSeconds: 30, TimeoutSeconds: 5},
		{ID: "icmp", Name: "ICMP", Protocol: "icmp", AgentInstanceUID: otherUID, Endpoint: "::1", IntervalSeconds: 20, TimeoutSeconds: 3, PingCount: 2},
		{ID: "tcp", Name: "TCP", Protocol: "tcp", Endpoint: "example.com:443", IntervalSeconds: 10, TimeoutSeconds: 1},
	}
	definitions, err := monitor.NewStore(db).List(t.Context())
	if err != nil || !reflect.DeepEqual(definitions, want) {
		t.Fatalf("migrated definitions = %+v, %v, want %+v", definitions, err, want)
	}
	var obsolete int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_schema
		WHERE name IN ('http_monitors', 'network_monitors', 'http_monitor_id_is_unique', 'network_monitor_id_is_unique')`).Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatalf("obsolete schema objects = %d, error = %v", obsolete, err)
	}
}

func TestMigrateMonitorsPreservesConfigurationHistoryAndTarget(t *testing.T) {
	db := openMonitorMigrationFixture(t, filepath.Join(t.TempDir(), "history.db"))
	uid := agent.InstanceUID{}
	specification := configuration.NewSpecification(uid)
	specification.HTTPMonitors = []configuration.HTTPMonitor{{
		ID: "http", Endpoint: "https://example.com/$value", Method: "HEAD", IntervalSeconds: 30, TimeoutSeconds: 5,
	}}
	specification.TCPMonitors = []configuration.TCPMonitor{{
		ID: "tcp", Endpoint: "example.com:443", IntervalSeconds: 10, TimeoutSeconds: 1,
	}}
	content, err := configuration.Compile(specification)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO agent_config_revisions (instance_uid, revision, config_hash, content, specification, created_at_unix_ms)
		VALUES (zeroblob(16), 1, zeroblob(32), x'6c6567616379', NULL, 100),
		(zeroblob(16), 2, ?, ?, ?, 200);
	`, hash[:], content, string(encoded)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO agent_config_assignments (instance_uid, desired_revision, previous_revision, failed_revision, reconciled_specification)
		VALUES (zeroblob(16), 2, 1, 2, ?);
	`, string(encoded)); err != nil {
		t.Fatal(err)
	}
	store := remoteconfig.NewStore(db)
	before, err := store.ListRevisions(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListRevisions(t.Context(), uid)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("migration or reconciliation changed history: %+v, %v; want %+v", after, err, before)
	}
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || desired.Number != 2 || desired.ConfigHash != hash || !bytes.Equal(desired.Content, content) || !bytes.Equal(desired.Specification, encoded) {
		t.Fatalf("migration changed the compiled target: %+v, %v", desired, err)
	}
	legacy, err := store.RevisionContent(t.Context(), uid, 1)
	if err != nil || string(legacy) != "legacy" {
		t.Fatalf("migration changed legacy YAML: %q, %v", legacy, err)
	}
	var previous, failed int
	if err := db.QueryRowContext(t.Context(), `SELECT previous_revision, failed_revision FROM agent_config_assignments WHERE instance_uid = zeroblob(16)`).Scan(&previous, &failed); err != nil || previous != 1 || failed != 2 {
		t.Fatalf("migration changed recovery state: previous=%d failed=%d, error=%v", previous, failed, err)
	}
}

func openMonitorMigrationFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := OpenFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	paths, err := readMigrationPaths(embeddedMigrations)
	if err != nil {
		t.Fatal(err)
	}
	oldRelease := fstest.MapFS{}
	for _, path := range paths[:7] {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	if err := migrate(t.Context(), db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO agents (instance_uid) VALUES (zeroblob(16)), (x'11111111111111111111111111111111');
		INSERT INTO http_monitors (id, name, agent_instance_uid, endpoint, method, interval_seconds, timeout_seconds)
		VALUES ('http', 'HTTP', zeroblob(16), 'https://example.com/$value', 'HEAD', 30, 5);
		INSERT INTO network_monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds, ping_count, dns_server, record_type, transport)
		VALUES ('tcp', 'TCP', 'tcp', zeroblob(16), 'example.com:443', 10, 1, 0, '', '', ''),
		('icmp', 'ICMP', 'icmp', x'11111111111111111111111111111111', '::1', 20, 3, 2, '', '', ''),
		('dns', 'DNS', 'dns', x'11111111111111111111111111111111', 'example.com', 60, 4, 0, '1.1.1.1:53', 'AAAA', 'tcp');
	`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateRollsBackMonitorCopiesOnConflictingID(t *testing.T) {
	db := openMonitorMigrationFixture(t, filepath.Join(t.TempDir(), "conflict.db"))
	// Older uniqueness triggers only protect INSERT, so a legacy UPDATE can
	// leave the same ID in both tables. Never silently discard either definition.
	if _, err := db.ExecContext(t.Context(), `UPDATE network_monitors SET id = 'http' WHERE id = 'tcp'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err == nil {
		t.Fatal("migration accepted conflicting Monitor IDs")
	}
	var version, httpCount, otherCount, newTable int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM http_monitors),
		(SELECT count(*) FROM network_monitors),
		(SELECT count(*) FROM sqlite_schema WHERE name = 'monitors')`).Scan(&httpCount, &otherCount, &newTable); err != nil {
		t.Fatal(err)
	}
	if version != 7 || httpCount != 1 || otherCount != 3 || newTable != 0 {
		t.Fatalf("partial migration: version=%d, HTTP=%d, others=%d, new table=%d", version, httpCount, otherCount, newTable)
	}
}
