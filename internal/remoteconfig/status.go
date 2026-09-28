package remoteconfig

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// ErrNoReportedStatus reports that an agent has not reported remote configuration status.
var ErrNoReportedStatus = errors.New("no reported remote configuration status")

// ErrNoReportedFailure reports that an agent has not reported a failed configuration.
var ErrNoReportedFailure = errors.New("no reported remote configuration failure")

// ErrNoWorkingRevision reports that no known revision has been confirmed applied.
var ErrNoWorkingRevision = errors.New("no confirmed working configuration revision")

// ApplyStatus describes an agent's progress applying remote configuration.
type ApplyStatus string

const (
	// ApplyStatusApplying means the agent is currently applying the configuration.
	ApplyStatusApplying ApplyStatus = "applying"
	// ApplyStatusApplied means the agent successfully applied the configuration.
	ApplyStatusApplied ApplyStatus = "applied"
	// ApplyStatusFailed means the agent failed to apply the configuration.
	ApplyStatusFailed ApplyStatus = "failed"
)

// StatusReport is the latest remote configuration status reported by an agent.
type StatusReport struct {
	Revision     *int64
	ConfigHash   []byte
	Status       ApplyStatus
	ErrorMessage string
	ReportedAt   time.Time
}

// RecordStatus stores the latest remote configuration status reported by an agent.
// Reports never select a different desired revision. A failed target is held
// until a different target is selected or it is subsequently confirmed applied.
func (store *Store) RecordStatus(
	ctx context.Context,
	agentUID agent.InstanceUID,
	report StatusReport,
) (returnErr error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remote configuration status update: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back remote configuration status update: %w", err))
		}
	}()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_remote_config_statuses (
			instance_uid,
			config_hash,
			apply_status,
			error_message,
			reported_at_unix_ms
		 ) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (instance_uid) DO UPDATE
		 SET config_hash = excluded.config_hash,
			 apply_status = excluded.apply_status,
			 error_message = excluded.error_message,
			 reported_at_unix_ms = excluded.reported_at_unix_ms`,
		agentUID[:],
		report.ConfigHash,
		report.Status,
		report.ErrorMessage,
		report.ReportedAt.UTC().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("record remote configuration status: %w", err)
	}

	if report.Status == ApplyStatusFailed {
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO agent_remote_config_failures (
				instance_uid,
				config_hash,
				error_message,
				failed_at_unix_ms
			 ) VALUES (?, ?, ?, ?)
			 ON CONFLICT (instance_uid, config_hash) DO UPDATE
			 SET error_message = excluded.error_message,
				 failed_at_unix_ms = excluded.failed_at_unix_ms`,
			agentUID[:],
			report.ConfigHash,
			report.ErrorMessage,
			report.ReportedAt.UTC().UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("record remote configuration failure: %w", err)
		}

		_, err = tx.ExecContext(
			ctx,
			`UPDATE agent_config_assignments
			 SET failed_revision = desired_revision
			 WHERE instance_uid = ?
			   AND desired_revision = (
				SELECT revision
				FROM agent_config_revisions
				WHERE instance_uid = ? AND config_hash = ?
			   )`,
			agentUID[:],
			agentUID[:],
			report.ConfigHash,
		)
		if err != nil {
			return fmt.Errorf("mark desired configuration failed: %w", err)
		}
	}
	if report.Status == ApplyStatusApplied {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO agent_remote_config_working (instance_uid, revision, reported_at_unix_ms)
			 SELECT instance_uid, revision, ? FROM agent_config_revisions
			 WHERE instance_uid = ? AND config_hash = ?
			 ON CONFLICT (instance_uid) DO UPDATE SET
			     revision = excluded.revision,
			     reported_at_unix_ms = excluded.reported_at_unix_ms`,
			report.ReportedAt.UTC().UnixMilli(), agentUID[:], report.ConfigHash,
		)
		if err != nil {
			return fmt.Errorf("record confirmed working configuration: %w", err)
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE agent_config_assignments SET failed_revision = NULL
			 WHERE instance_uid = ? AND desired_revision = (
			     SELECT revision FROM agent_config_revisions WHERE instance_uid = ? AND config_hash = ?
			 )`,
			agentUID[:], agentUID[:], report.ConfigHash,
		)
		if err != nil {
			return fmt.Errorf("confirm desired configuration applied: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remote configuration status update: %w", err)
	}

	return nil
}

// LatestStatus returns the latest remote configuration status reported by an agent.
func (store *Store) LatestStatus(
	ctx context.Context,
	agentUID agent.InstanceUID,
) (StatusReport, error) {
	var report StatusReport
	var revision sql.NullInt64
	var reportedAtUnixMS int64
	row := store.db.QueryRowContext(
		ctx,
		`SELECT
			revisions.revision,
			statuses.config_hash,
			statuses.apply_status,
			statuses.error_message,
			statuses.reported_at_unix_ms
		 FROM agent_remote_config_statuses AS statuses
		 LEFT JOIN agent_config_revisions AS revisions
		   ON revisions.instance_uid = statuses.instance_uid
		  AND revisions.config_hash = statuses.config_hash
		 WHERE statuses.instance_uid = ?`,
		agentUID[:],
	)
	if err := row.Scan(
		&revision,
		&report.ConfigHash,
		&report.Status,
		&report.ErrorMessage,
		&reportedAtUnixMS,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StatusReport{}, fmt.Errorf(
				"read remote configuration status: %w",
				ErrNoReportedStatus,
			)
		}

		return StatusReport{}, fmt.Errorf("read remote configuration status: %w", err)
	}

	if revision.Valid {
		revisionNumber := revision.Int64
		report.Revision = &revisionNumber
	}
	report.ConfigHash = bytes.Clone(report.ConfigHash)
	report.ReportedAt = time.UnixMilli(reportedAtUnixMS).UTC()

	return report, nil
}

// LatestFailure returns the most recently reported failed configuration for an agent.
func (store *Store) LatestFailure(
	ctx context.Context,
	agentUID agent.InstanceUID,
) (StatusReport, error) {
	var report StatusReport
	var revision sql.NullInt64
	var failedAtUnixMS int64
	row := store.db.QueryRowContext(
		ctx,
		`SELECT
			revisions.revision,
			failures.config_hash,
			failures.error_message,
			failures.failed_at_unix_ms
		 FROM agent_remote_config_failures AS failures
		 LEFT JOIN agent_config_revisions AS revisions
		   ON revisions.instance_uid = failures.instance_uid
		  AND revisions.config_hash = failures.config_hash
		 WHERE failures.instance_uid = ?
		 ORDER BY failures.failed_at_unix_ms DESC
		 LIMIT 1`,
		agentUID[:],
	)
	if err := row.Scan(
		&revision,
		&report.ConfigHash,
		&report.ErrorMessage,
		&failedAtUnixMS,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StatusReport{}, fmt.Errorf(
				"read remote configuration failure: %w",
				ErrNoReportedFailure,
			)
		}

		return StatusReport{}, fmt.Errorf("read remote configuration failure: %w", err)
	}

	if revision.Valid {
		revisionNumber := revision.Int64
		report.Revision = &revisionNumber
	}
	report.ConfigHash = bytes.Clone(report.ConfigHash)
	report.Status = ApplyStatusFailed
	report.ReportedAt = time.UnixMilli(failedAtUnixMS).UTC()

	return report, nil
}

// ConfigurationStatus combines the desired revision with reported progress.
// A failed target stays failed even when the Agent reports local recovery.
type ConfigurationStatus struct {
	State       ApplyStatus
	Desired     Revision
	Reported    *StatusReport
	LastFailure *StatusReport
	LastWorking *StatusReport
}

// Status assembles the current view using the same independent reads as the
// protocol-facing methods. It does not promise a transaction-wide snapshot.
func (store *Store) Status(ctx context.Context, uid agent.InstanceUID) (ConfigurationStatus, error) {
	desired, failed, err := store.desired(ctx, uid)
	if err != nil {
		return ConfigurationStatus{}, err
	}
	result := ConfigurationStatus{State: ApplyStatusApplying, Desired: desired}
	reported, err := store.LatestStatus(ctx, uid)
	if err != nil && !errors.Is(err, ErrNoReportedStatus) {
		return ConfigurationStatus{}, err
	}
	if err == nil {
		result.Reported = &reported
		if bytes.Equal(reported.ConfigHash, desired.ConfigHash[:]) && reported.Status == ApplyStatusApplied {
			result.State = ApplyStatusApplied
		}
	}
	if failed {
		result.State = ApplyStatusFailed
	}
	failure, err := store.LatestFailure(ctx, uid)
	if err != nil && !errors.Is(err, ErrNoReportedFailure) {
		return ConfigurationStatus{}, err
	}
	if err == nil {
		result.LastFailure = &failure
	}
	working, err := store.LastWorking(ctx, uid)
	if err != nil && !errors.Is(err, ErrNoWorkingRevision) {
		return ConfigurationStatus{}, err
	}
	if err == nil {
		result.LastWorking = &working
	}
	return result, nil
}

// LastWorking returns the last known revision positively reported as applied.
// Reports for unknown hashes remain observations, never working artifacts.
func (store *Store) LastWorking(ctx context.Context, uid agent.InstanceUID) (StatusReport, error) {
	var revision int64
	var reportedAt int64
	var hash []byte
	err := store.db.QueryRowContext(ctx,
		`SELECT working.revision, revisions.config_hash, working.reported_at_unix_ms
		 FROM agent_remote_config_working AS working
		 JOIN agent_config_revisions AS revisions
		   ON revisions.instance_uid = working.instance_uid AND revisions.revision = working.revision
		 WHERE working.instance_uid = ?`, uid[:],
	).Scan(&revision, &hash, &reportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StatusReport{}, ErrNoWorkingRevision
	}
	if err != nil {
		return StatusReport{}, fmt.Errorf("read confirmed working configuration: %w", err)
	}
	return StatusReport{
		Revision: &revision, ConfigHash: bytes.Clone(hash), Status: ApplyStatusApplied,
		ReportedAt: time.UnixMilli(reportedAt).UTC(),
	}, nil
}
