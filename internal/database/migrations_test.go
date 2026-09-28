package database

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrationsAreEmbedded(t *testing.T) {
	_, err := readMigration(
		embeddedMigrations,
		"migrations/0001_initial_schema.sql",
	)
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
}

func TestReadMigrationPaths(t *testing.T) {
	paths, err := readMigrationPaths(embeddedMigrations)
	if err != nil {
		t.Fatalf("readMigrationPaths(embeddedMigrations) error = %v, want nil", err)
	}

	want := []string{"migrations/0001_initial_schema.sql", "migrations/0002_configuration_specifications.sql", "migrations/0003_configuration_outcomes.sql", "migrations/0004_configuration_revision_dates.sql", "migrations/0005_http_monitors.sql", "migrations/0006_http_monitor_agent_index.sql", "migrations/0007_network_monitors.sql", "migrations/0008_unified_monitors.sql", "migrations/0009_monitor_options.sql", "migrations/0010_alert_rules.sql", "migrations/0011_notification_channels.sql", "migrations/0012_alert_rule_notifications.sql", "migrations/0013_alert_thresholds.sql", "migrations/0014_incidents.sql", "migrations/0015_notification_delivery.sql"}
	if !slices.Equal(paths, want) {
		t.Errorf("readMigrationPaths(embeddedMigrations) = %v, want %v", paths, want)
	}
}

func TestValidateMigrationPaths(t *testing.T) {
	tests := []struct {
		name      string
		paths     []string
		wantError bool
	}{
		{
			name: "consecutive migrations",
			paths: []string{
				"migrations/0001_initial_schema.sql",
				"migrations/0002_add_agent_connected.sql",
			},
		},
		{
			name: "missing migration",
			paths: []string{
				"migrations/0001_initial_schema.sql",
				"migrations/0003_add_agent_name.sql",
			},
			wantError: true,
		},
		{
			name: "invalid first migration",
			paths: []string{
				"migrations/0002_create_agents.sql",
			},
			wantError: true,
		},
		{
			name: "missing numeric prefix",
			paths: []string{
				"migrations/create_agents.sql",
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMigrationPaths(test.paths)
			if test.wantError && err == nil {
				t.Fatal("validateMigrationPaths() error = nil, want non-nil")
			}
			if !test.wantError && err != nil {
				t.Fatalf("validateMigrationPaths() error = %v, want nil", err)
			}
		})
	}
}

func TestSchemaVersionStartsAtZero(t *testing.T) {
	ctx := context.Background()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	version, err := schemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("schemaVersion() error = %v, want nil", err)
	}
	if version != 0 {
		t.Errorf("schemaVersion() = %d, want 0", version)
	}
}

func TestMigrateInitializesSchemaOnce(t *testing.T) {
	ctx := context.Background()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate() error = %v, want nil", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate() error = %v, want nil", err)
	}

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	const wantVersion = 15
	if version != wantVersion {
		t.Errorf(
			"schema version = %d, want %d",
			version,
			wantVersion,
		)
	}
}

func TestMigrateRejectsNewerSchemaVersion(t *testing.T) {
	ctx := context.Background()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 16"); err != nil {
		t.Fatalf("set schema version: %v", err)
	}

	if err := Migrate(ctx, db); err == nil {
		t.Fatal("Migrate() error = nil, want unsupported schema version error")
	}
}

func TestMigrateAddsNotificationChannelsToExistingAlertRules(t *testing.T) {
	ctx := t.Context()
	db, err := OpenFile(ctx, filepath.Join(t.TempDir(), "arveld.db"))
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
	for _, path := range paths[:10] {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	if err := migrate(ctx, db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
		INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds)
		VALUES ('existing-monitor', 'Existing Monitor', 'tcp', zeroblob(16), 'example.com:443', 30, 5);
		INSERT INTO alert_rules (id, monitor_id, condition, for_seconds, severity)
		VALUES ('existing-rule', 'existing-monitor', 'no_data', 120, 'warning');
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO notification_channels (id, name, type, config)
		VALUES ('operations', 'Operations', 'webhook', '{"url":"https://hooks.example.com/events"}')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var owner, condition, severity string
	var duration int
	if err := db.QueryRowContext(ctx, `SELECT monitor_id, condition, for_seconds, severity
		FROM alert_rules WHERE id = 'existing-rule'`).Scan(&owner, &condition, &duration, &severity); err != nil {
		t.Fatal(err)
	}
	if owner != "existing-monitor" || condition != "no_data" || duration != 120 || severity != "warning" {
		t.Fatalf("upgrade changed existing alert rule: %q %q %d %q", owner, condition, duration, severity)
	}
	var name, channelType, config string
	if err := db.QueryRowContext(ctx, `SELECT name, type, config FROM notification_channels WHERE id = 'operations'`).
		Scan(&name, &channelType, &config); err != nil {
		t.Fatal(err)
	}
	if name != "Operations" || channelType != "webhook" || config != `{"url":"https://hooks.example.com/events"}` {
		t.Fatalf("repeated migration changed notification channel: %q %q %q", name, channelType, config)
	}
}

func TestMigratePreservesAlertDestinationsWithThresholds(t *testing.T) {
	ctx := t.Context()
	db, err := OpenFile(ctx, filepath.Join(t.TempDir(), "arveld.db"))
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
	for _, path := range paths[:12] {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	if err := migrate(ctx, db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
		INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds)
		VALUES ('existing-monitor', 'Existing Monitor', 'tcp', zeroblob(16), 'example.com:443', 30, 5);
		INSERT INTO alert_rules (id, monitor_id, condition, for_seconds, severity)
		VALUES ('existing-rule', 'existing-monitor', 'no_data', 120, 'warning');
        INSERT INTO notification_channels (id,name,type,config) VALUES ('saved','Saved','webhook','{"url":"https://example.com/events"}');
        INSERT INTO alert_rule_notifications VALUES ('existing-rule','saved');
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO notification_channels (id, name, type, config)
		VALUES ('operations', 'Operations', 'webhook', '{"url":"https://hooks.example.com/events"}')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var owner, condition, severity string
	var duration int
	if err := db.QueryRowContext(ctx, `SELECT monitor_id, condition, for_seconds, severity
		FROM alert_rules WHERE id = 'existing-rule'`).Scan(&owner, &condition, &duration, &severity); err != nil {
		t.Fatal(err)
	}
	if owner != "existing-monitor" || condition != "no_data" || duration != 120 || severity != "warning" {
		t.Fatalf("upgrade changed existing alert rule: %q %q %d %q", owner, condition, duration, severity)
	}
	var name, channelType, config string
	if err := db.QueryRowContext(ctx, `SELECT name, type, config FROM notification_channels WHERE id = 'operations'`).
		Scan(&name, &channelType, &config); err != nil {
		t.Fatal(err)
	}
	if name != "Operations" || channelType != "webhook" || config != `{"url":"https://hooks.example.com/events"}` {
		t.Fatalf("repeated migration changed notification channel: %q %q %q", name, channelType, config)
	}
	var destination string
	if err := db.QueryRowContext(ctx, `SELECT notification_id FROM alert_rule_notifications WHERE rule_id='existing-rule'`).Scan(&destination); err != nil || destination != "saved" {
		t.Fatalf("upgrade lost rule destination: %q, %v", destination, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id='saved'`); err == nil {
		t.Fatal("upgrade lost destination deletion protection")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM monitors WHERE id='existing-monitor'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alert_rule_notifications`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("upgrade lost assignment cascading deletion: %d, %v", remaining, err)
	}
}

func TestMigrateAddsBothAlertConditionsToExistingMonitors(t *testing.T) {
	ctx := t.Context()
	db, err := OpenFile(ctx, filepath.Join(t.TempDir(), "arveld.db"))
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
	for _, path := range paths[:9] {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	if err := migrate(ctx, db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
		INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds)
		VALUES ('existing-monitor', 'Existing Monitor', 'tcp', zeroblob(16), 'example.com:443', 30, 5);
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var name, protocol, endpoint string
	var interval, timeout int
	var agentID []byte
	if err := db.QueryRowContext(ctx, `SELECT name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds
		FROM monitors WHERE id = 'existing-monitor'`).Scan(&name, &protocol, &agentID, &endpoint, &interval, &timeout); err != nil {
		t.Fatal(err)
	}
	if name != "Existing Monitor" || protocol != "tcp" || !bytes.Equal(agentID, make([]byte, 16)) || endpoint != "example.com:443" || interval != 30 || timeout != 5 {
		t.Fatalf("migration changed the existing Monitor: %q %q %x %q %d %d", name, protocol, agentID, endpoint, interval, timeout)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alert_rules (id, monitor_id, condition, for_seconds, severity)
		VALUES ('existing-rule', 'existing-monitor', 'failed', 120, 'critical'),
		       ('no-data-rule', 'existing-monitor', 'no_data', 60, 'warning')`); err != nil {
		t.Fatalf("upgraded schema must accept both alert conditions: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var owner, condition, severity string
	var duration int
	if err := db.QueryRowContext(ctx, `SELECT monitor_id, condition, for_seconds, severity
		FROM alert_rules WHERE id = 'existing-rule'`).Scan(&owner, &condition, &duration, &severity); err != nil {
		t.Fatal(err)
	}
	if owner != "existing-monitor" || condition != "failed" || duration != 120 || severity != "critical" {
		t.Fatalf("migration changed the existing rule: %q %q %d %q", owner, condition, duration, severity)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM alert_rules").Scan(&count); err != nil || count != 2 {
		t.Fatalf("repeated migration must preserve both rules: count %d, %v", count, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alert_rules (id, monitor_id, condition, for_seconds, severity)
		VALUES ('unsupported-rule', 'existing-monitor', 'latency', 60, 'warning')`); err == nil {
		t.Fatal("upgraded schema accepted an unsupported condition")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM monitors WHERE id = 'existing-monitor'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM alert_rules").Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleting the Monitor must still delete both rules: count %d, %v", count, err)
	}
}

func TestMigrateIndexesMonitorsByAgent(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "fresh database"
		if existing {
			name = "existing version 5"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			db, err := open(ctx, "file::memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if existing {
				paths, err := readMigrationPaths(embeddedMigrations)
				if err != nil {
					t.Fatal(err)
				}
				oldRelease := fstest.MapFS{}
				for _, path := range paths[:5] {
					content, err := readMigration(embeddedMigrations, path)
					if err != nil {
						t.Fatal(err)
					}
					oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
				}
				if err := migrate(ctx, db, oldRelease); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `
					INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
					INSERT INTO http_monitors (id, name, agent_instance_uid, endpoint, method, interval_seconds, timeout_seconds)
					VALUES ('kept', 'Existing Monitor', zeroblob(16), 'https://example.com', 'GET', 30, 5);
				`); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := Migrate(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			if existing {
				var name string
				if err := db.QueryRowContext(ctx, "SELECT name FROM monitors WHERE id = 'kept'").Scan(&name); err != nil || name != "Existing Monitor" {
					t.Fatalf("migration changed an existing Monitor: %q, %v", name, err)
				}
			}
			rows, err := db.QueryContext(ctx, `EXPLAIN QUERY PLAN
				SELECT id, name, endpoint, method, interval_seconds, timeout_seconds
				FROM monitors WHERE agent_instance_uid = ? ORDER BY id`, make([]byte, 16))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := rows.Close(); err != nil {
					t.Error(err)
				}
			}()
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(plan, "\n")
			if !strings.Contains(joined, "SEARCH monitors") || !strings.Contains(joined, "agent_instance_uid=?") ||
				strings.Contains(joined, "SCAN monitors") || strings.Contains(joined, "TEMP B-TREE") {
				t.Fatalf("Agent Monitor lookup must use an indexed search without sorting; query plan:\n%s", joined)
			}
		})
	}
}

func TestMigratePreservesLegacyConfigurationWithoutInventingProvenance(t *testing.T) {
	ctx := t.Context()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	initial, err := readMigration(embeddedMigrations, "migrations/0001_initial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	oldRelease := fstest.MapFS{
		"migrations/0001_initial_schema.sql": &fstest.MapFile{Data: []byte(initial)},
	}
	if err := migrate(ctx, db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (instance_uid) VALUES (X'01000000000000000000000000000000');
		INSERT INTO agent_config_revisions (instance_uid, revision, config_hash, content)
		VALUES (X'01000000000000000000000000000000', 7, zeroblob(32), X'010203');
		INSERT INTO agent_config_assignments (instance_uid, desired_revision)
		VALUES (X'01000000000000000000000000000000', 7);
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var revision int64
	var content []byte
	var historical, reconciled sql.NullString
	var createdAt sql.NullInt64
	if err := db.QueryRowContext(ctx, `
		SELECT r.revision, r.content, r.specification, a.reconciled_specification, r.created_at_unix_ms
		FROM agent_config_revisions r JOIN agent_config_assignments a
		ON a.instance_uid = r.instance_uid AND a.desired_revision = r.revision
	`).Scan(&revision, &content, &historical, &reconciled, &createdAt); err != nil {
		t.Fatal(err)
	}
	if revision != 7 || !bytes.Equal(content, []byte{1, 2, 3}) || historical.Valid || reconciled.Valid {
		t.Fatalf("migration altered legacy state: revision %d, content %x, provenance %v/%v", revision, content, historical, reconciled)
	}
	if createdAt.Valid {
		t.Fatalf("migration fabricated a legacy publication date: %d", createdAt.Int64)
	}
}

func TestMigrateConfigurationOutcomesUsesOnlyKnownReports(t *testing.T) {
	oldRelease := fstest.MapFS{}
	for _, path := range []string{"migrations/0001_initial_schema.sql", "migrations/0002_configuration_specifications.sql"} {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	for _, test := range []struct {
		name        string
		status      string
		hashByte    byte
		wantWorking int64
		wantFailed  bool
	}{
		{"desired applied", "applied", 2, 2, false},
		{"previous applied", "applied", 1, 1, false},
		{"unknown applied", "applied", 3, 0, false},
		{"desired failed", "failed", 2, 0, true},
		{"previous failed", "failed", 1, 0, false},
		{"desired applying", "applying", 2, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			db, err := open(ctx, "file::memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := migrate(ctx, db, oldRelease); err != nil {
				t.Fatal(err)
			}
			uid := make([]byte, 16)
			if _, err := db.ExecContext(ctx, "INSERT INTO agents (instance_uid) VALUES (?)", uid); err != nil {
				t.Fatal(err)
			}
			for _, revision := range []byte{1, 2} {
				if _, err := db.ExecContext(ctx, `INSERT INTO agent_config_revisions
					(instance_uid, revision, config_hash, content) VALUES (?, ?, ?, X'01')`,
					uid, revision, bytes.Repeat([]byte{revision}, 32)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO agent_config_assignments
				(instance_uid, desired_revision, previous_revision) VALUES (?, 2, 1)`, uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO agent_remote_config_statuses
				(instance_uid, config_hash, apply_status, error_message, reported_at_unix_ms)
				VALUES (?, ?, ?, '', 1234)`, uid, bytes.Repeat([]byte{test.hashByte}, 32), test.status); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := Migrate(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			var desired, previous int64
			var working, confirmedAt, failed sql.NullInt64
			if err := db.QueryRowContext(ctx, `SELECT desired_revision, previous_revision,
				failed_revision, working.revision, working.reported_at_unix_ms
				FROM agent_config_assignments AS assignments
				LEFT JOIN agent_remote_config_working AS working USING (instance_uid)
				WHERE instance_uid = ?`, uid).Scan(&desired, &previous, &failed, &working, &confirmedAt); err != nil {
				t.Fatal(err)
			}
			if desired != 2 || previous != 1 || working.Int64 != test.wantWorking || working.Valid != (test.wantWorking != 0) || failed.Valid != test.wantFailed {
				t.Fatalf("migrated outcomes: desired %d, previous %d, working %+v, failed %+v", desired, previous, working, failed)
			}
			if (working.Valid && confirmedAt.Int64 != 1234) || (failed.Valid && failed.Int64 != desired) {
				t.Fatalf("migration lost report identity: time %+v, failed %+v", confirmedAt, failed)
			}
		})
	}
}

func TestMigrateRollsBackIncompleteSchema(t *testing.T) {
	ctx := t.Context()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	// A name collision midway through initialization must undo earlier DDL too.
	if _, err := db.ExecContext(ctx, "CREATE TABLE users (sentinel TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err == nil {
		t.Fatal("initialization succeeded despite a conflicting table")
	}
	var tables, version int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if tables != 1 || version != 0 {
		t.Fatalf("failed initialization left %d tables and version %d, want 1 and 0", tables, version)
	}
}

func TestMigrateAppliesOnlyPendingFilesInOrder(t *testing.T) {
	ctx := t.Context()
	db, err := open(ctx, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	// Use independent SQL fixtures so this test survives future schema changes.
	files := fstest.MapFS{
		"migrations/0001_create_events.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT NOT NULL) STRICT;"),
		},
	}
	if err := migrate(ctx, db, files); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO events VALUES (1, 'kept')"); err != nil {
		t.Fatal(err)
	}

	files["migrations/0003_finish_event.sql"] = &fstest.MapFile{
		Data: []byte("UPDATE events SET name = name || '-finished' WHERE id = 1;"),
	}
	files["migrations/0002_update_event.sql"] = &fstest.MapFile{
		Data: []byte("UPDATE events SET name = name || '-updated' WHERE id = 1;"),
	}
	for range 2 {
		if err := migrate(ctx, db, files); err != nil {
			t.Fatalf("apply pending migrations: %v", err)
		}
	}
	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM events WHERE id = 1").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "kept-updated-finished" {
		t.Errorf("migrated name = %q, want kept-updated-finished", name)
	}
	version, err := schemaVersion(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Errorf("schema version = %d, want 3", version)
	}
}

func TestMigratePreservesNotificationDeliveryDefaults(t *testing.T) {
	ctx := t.Context()
	db, err := OpenFile(ctx, filepath.Join(t.TempDir(), "arveld.db"))
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
	for _, path := range paths[:14] {
		content, err := readMigration(embeddedMigrations, path)
		if err != nil {
			t.Fatal(err)
		}
		oldRelease[path] = &fstest.MapFile{Data: []byte(content)}
	}
	if err := migrate(ctx, db, oldRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
		INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds)
		VALUES ('existing-monitor', 'Existing Monitor', 'tcp', zeroblob(16), 'example.com:443', 30, 5);
		INSERT INTO alert_rules (id, monitor_id, condition, for_seconds, severity)
		VALUES ('existing-rule', 'existing-monitor', 'no_data', 120, 'warning');
        INSERT INTO notification_channels (id,name,type,config) VALUES ('saved','Saved','webhook','{"url":"https://example.com/events"}');
        INSERT INTO alert_rule_notifications VALUES ('existing-rule','saved');
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var destination string
	if err := db.QueryRowContext(ctx, `SELECT notification_id FROM alert_rule_notifications WHERE rule_id='existing-rule'`).Scan(&destination); err != nil || destination != "saved" {
		t.Fatalf("upgrade lost rule destination: %q, %v", destination, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id='saved'`); err == nil {
		t.Fatal("upgrade lost destination deletion protection")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM monitors WHERE id='existing-monitor'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alert_rule_notifications`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("upgrade lost assignment cascading deletion: %d, %v", remaining, err)
	}
	var delivery string
	if err := db.QueryRowContext(ctx, `SELECT delivery FROM notification_channels WHERE id = 'saved'`).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	if delivery != `{"group_by":"rule","group_wait_seconds":5,"group_interval_seconds":30,"repeat_interval_seconds":14400}` {
		t.Fatalf("upgraded channel defaults = %s", delivery)
	}
}
