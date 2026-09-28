// Package remoteconfig owns desired collector configuration revisions.
package remoteconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// ErrNoPreviousRevision reports that rollback has no earlier revision.
var ErrNoPreviousRevision = errors.New("no previous configuration revision")

// ErrNoDesiredRevision reports that there is no desired configuration revision.
var ErrNoDesiredRevision = errors.New("no desired configuration revision")

// Revision is an immutable collector configuration revision.
type Revision struct {
	Number     int64
	ConfigHash [sha256.Size]byte
	Content    []byte
	// Specification records the immutable compilation input, or nil for raw YAML.
	Specification json.RawMessage
}

// Store persists and retrieves collector configuration revisions.
type Store struct {
	db *sql.DB
}

// NewStore requires a migrated database opened by database.OpenFile or
// OpenExistingFile. The caller owns its closure.
// NewStore creates a remote configuration store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save stores the next desired configuration revision for an agent.
func (store *Store) Save(
	ctx context.Context,
	agentUID agent.InstanceUID,
	content []byte,
) (result Revision, returnErr error) {
	configHash := sha256.Sum256(content)

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, fmt.Errorf("begin desired configuration update: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back desired configuration update: %w", err))
		}
	}()

	var revision Revision
	row := tx.QueryRowContext(
		ctx,
		`SELECT revision, content, specification
		 FROM agent_config_revisions
		 WHERE instance_uid = ? AND config_hash = ?`,
		agentUID[:],
		configHash[:],
	)
	var specification []byte
	scanErr := row.Scan(
		&revision.Number,
		&revision.Content,
		&specification,
	)
	switch {
	case scanErr == nil:
		revision.ConfigHash = configHash
	case !errors.Is(scanErr, sql.ErrNoRows):
		return Revision{}, fmt.Errorf("read existing configuration: %w", scanErr)
	default:
		row = tx.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM agent_config_revisions
			 WHERE instance_uid = ?`,
			agentUID[:],
		)
		if err := row.Scan(&revision.Number); err != nil {
			return Revision{}, fmt.Errorf("read next configuration revision: %w", err)
		}

		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO agent_config_revisions (
				instance_uid,
				revision,
				config_hash,
				content,
				created_at_unix_ms
			 ) VALUES (?, ?, ?, ?, ?)`,
			agentUID[:],
			revision.Number,
			configHash[:],
			content,
			time.Now().UTC().UnixMilli(),
		)
		if err != nil {
			return Revision{}, fmt.Errorf("store configuration revision: %w", err)
		}

		revision.ConfigHash = configHash
		revision.Content = content
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_config_assignments (
			instance_uid,
			desired_revision,
			previous_revision
		 ) VALUES (?, ?, NULL)
		 ON CONFLICT (instance_uid) DO UPDATE SET
			previous_revision = CASE
				WHEN agent_config_assignments.desired_revision <> excluded.desired_revision
				THEN agent_config_assignments.desired_revision
				ELSE agent_config_assignments.previous_revision END,
			desired_revision = excluded.desired_revision,
			failed_revision = CASE
				WHEN agent_config_assignments.desired_revision <> excluded.desired_revision THEN NULL
				ELSE agent_config_assignments.failed_revision END,
			reconciled_specification = NULL
		 WHERE agent_config_assignments.desired_revision
			<> excluded.desired_revision
			OR agent_config_assignments.reconciled_specification IS NOT NULL`,
		agentUID[:],
		revision.Number,
	)
	if err != nil {
		return Revision{}, fmt.Errorf("set desired configuration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Revision{}, fmt.Errorf("commit desired configuration update: %w", err)
	}

	revision.Content = bytes.Clone(revision.Content)
	revision.Specification = bytes.Clone(specification)

	return revision, nil
}

// Rollback makes the immediately previous configuration revision desired.
func (store *Store) Rollback(
	ctx context.Context,
	agentUID agent.InstanceUID,
) (result Revision, returnErr error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, fmt.Errorf("begin desired configuration rollback: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back desired configuration update: %w", err))
		}
	}()

	var previousRevision Revision
	row := tx.QueryRowContext(
		ctx,
		`SELECT revisions.revision, revisions.config_hash, revisions.content, revisions.specification
		 FROM agent_config_assignments AS assignments
		 JOIN agent_config_revisions AS revisions
		   ON revisions.instance_uid = assignments.instance_uid
		  AND revisions.revision = assignments.previous_revision
		 WHERE assignments.instance_uid = ?`,
		agentUID[:],
	)
	var previousConfigHash []byte
	var specification []byte
	if err := row.Scan(
		&previousRevision.Number,
		&previousConfigHash,
		&previousRevision.Content,
		&specification,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Revision{}, fmt.Errorf(
				"rollback desired configuration: %w",
				ErrNoPreviousRevision,
			)
		}

		return Revision{}, fmt.Errorf("read previous configuration revision: %w", err)
	}
	copy(previousRevision.ConfigHash[:], previousConfigHash)

	_, err = tx.ExecContext(
		ctx,
		`UPDATE agent_config_assignments
		 SET desired_revision = previous_revision,
		     previous_revision = desired_revision,
		     failed_revision = NULL,
		     reconciled_specification = NULL
		 WHERE instance_uid = ?`,
		agentUID[:],
	)
	if err != nil {
		return Revision{}, fmt.Errorf("swap desired and previous configurations: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Revision{}, fmt.Errorf("commit desired configuration rollback: %w", err)
	}

	previousRevision.Content = bytes.Clone(previousRevision.Content)
	previousRevision.Specification = bytes.Clone(specification)

	return previousRevision, nil
}

// Desired returns the desired configuration revision for an agent.
func (store *Store) Desired(
	ctx context.Context,
	agentUID agent.InstanceUID,
) (Revision, error) {
	revision, _, err := store.desired(ctx, agentUID)
	return revision, err
}

// desired reads the selected artifact and its failure marker together so an
// observation for a different revision cannot decide delivery of this target.
func (store *Store) desired(ctx context.Context, agentUID agent.InstanceUID) (Revision, bool, error) {
	var revision Revision
	row := store.db.QueryRowContext(
		ctx,
		`SELECT revisions.revision, revisions.config_hash, revisions.content, revisions.specification,
		        assignments.failed_revision
		 FROM agent_config_assignments AS assignments
		 JOIN agent_config_revisions AS revisions
		   ON revisions.instance_uid = assignments.instance_uid
		  AND revisions.revision = assignments.desired_revision
		 WHERE assignments.instance_uid = ?`,
		agentUID[:],
	)
	var configHash []byte
	var specification []byte
	var failed sql.NullInt64
	if err := row.Scan(
		&revision.Number,
		&configHash,
		&revision.Content,
		&specification,
		&failed,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Revision{}, false, fmt.Errorf(
				"read desired configuration: %w",
				ErrNoDesiredRevision,
			)
		}

		return Revision{}, false, fmt.Errorf("read desired configuration: %w", err)
	}
	copy(revision.ConfigHash[:], configHash)

	revision.Content = bytes.Clone(revision.Content)
	revision.Specification = bytes.Clone(specification)

	return revision, failed.Valid, nil
}
