package remoteconfig

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// ErrInputsChanged means compilation raced with a Monitor write and should be retried.
var ErrInputsChanged = errors.New("agent configuration inputs changed during reconciliation")

// ReconcileAgent composes the current base and this Agent's persisted Monitors.
// Legacy and explicit YAML assignments remain unchanged until their migration.
func (store *Store) ReconcileAgent(ctx context.Context, uid agent.InstanceUID) (returnErr error) {
	monitors, err := monitor.ListForAgent(ctx, store.db, uid)
	if err != nil {
		return fmt.Errorf("read current Monitor inputs: %w", err)
	}
	current := configuration.NewSpecification(uid)
	if err := addMonitorInputs(&current, monitors); err != nil {
		return err
	}
	content, err := configuration.Compile(current)
	if err != nil {
		return fmt.Errorf("compile current Agent configuration: %w", err)
	}
	specification, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("encode current Agent specification: %w", err)
	}
	configHash := sha256.Sum256(content)

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Agent reconciliation: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back Agent reconciliation: %w", err))
		}
	}()

	// Recheck the exact input snapshot under SQLite's immediate write lock.
	// Compilation stays outside the transaction; changed inputs are never published.
	if err := recheckMonitorInputs(ctx, tx, uid, monitors); err != nil {
		return err
	}
	// Ownership is checked under the same lock, including a concurrent explicit Save.
	var previous sql.NullString
	err = tx.QueryRowContext(
		ctx,
		`SELECT reconciled_specification FROM agent_config_assignments WHERE instance_uid = ?`, uid[:],
	).Scan(&previous)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// First registration has no target yet.
	case err != nil:
		return fmt.Errorf("read reconciled Agent specification: %w", err)
	case !previous.Valid:
		return nil
	default:
		matches, err := specificationMatches(previous.String, current)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_config_revisions (instance_uid, revision, config_hash, content, specification, created_at_unix_ms)
		 SELECT ?, COALESCE(MAX(revision), 0) + 1, ?, ?, ?, ?
		 FROM agent_config_revisions WHERE instance_uid = ?
		 ON CONFLICT (instance_uid, config_hash) DO NOTHING`,
		uid[:], configHash[:], content, string(specification), time.Now().UTC().UnixMilli(), uid[:],
	)
	if err != nil {
		return fmt.Errorf("store reconciled Agent revision: %w", err)
	}
	var revision int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT revision FROM agent_config_revisions WHERE instance_uid = ? AND config_hash = ?`,
		uid[:], configHash[:],
	).Scan(&revision); err != nil {
		return fmt.Errorf("read reconciled Agent revision: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_config_assignments (instance_uid, desired_revision, reconciled_specification)
		 VALUES (?, ?, ?)
		 ON CONFLICT (instance_uid) DO UPDATE SET
		     previous_revision = CASE
		         WHEN agent_config_assignments.desired_revision <> excluded.desired_revision
		         THEN agent_config_assignments.desired_revision
		         ELSE agent_config_assignments.previous_revision END,
		     desired_revision = excluded.desired_revision,
		     failed_revision = CASE
		         WHEN agent_config_assignments.desired_revision <> excluded.desired_revision THEN NULL
		         ELSE agent_config_assignments.failed_revision END,
		     reconciled_specification = excluded.reconciled_specification`,
		uid[:], revision, string(specification),
	)
	if err != nil {
		return fmt.Errorf("assign reconciled Agent revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Agent reconciliation: %w", err)
	}
	return nil
}

// specificationMatches reads historical inputs without recompiling an old base.
// Persisted Monitor definitions are authoritative, including removal and reassignment.
func specificationMatches(encoded string, current configuration.Specification) (bool, error) {
	var previous configuration.Specification
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&previous); err != nil {
		return false, fmt.Errorf("decode reconciled Agent specification: %w", err)
	}
	if previous.SchemaVersion != current.SchemaVersion ||
		previous.BaseVersion < 0 || previous.BaseVersion > current.BaseVersion ||
		previous.InstanceUID != current.InstanceUID {
		return false, errors.New("stored specification is not supported by Agent reconciliation")
	}
	return previous.BaseVersion == current.BaseVersion && slices.EqualFunc(previous.HTTPMonitors, current.HTTPMonitors, func(a, b configuration.HTTPMonitor) bool { return reflect.DeepEqual(a, b) }) &&
		slices.Equal(previous.TCPMonitors, current.TCPMonitors) && slices.Equal(previous.ICMPMonitors, current.ICMPMonitors) &&
		slices.Equal(previous.DNSMonitors, current.DNSMonitors), nil
}
