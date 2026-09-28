package remoteconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// RevisionSummary describes an immutable publication without loading its YAML.
// CreatedAt is absent for revisions created before dates were recorded.
type RevisionSummary struct {
	Number    int64
	CreatedAt *time.Time
}

// ListRevisions returns this Agent's history, newest revision first.
func (store *Store) ListRevisions(ctx context.Context, uid agent.InstanceUID) (revisions []RevisionSummary, returnErr error) {
	rows, err := store.db.QueryContext(ctx, `SELECT revision, created_at_unix_ms
		FROM agent_config_revisions WHERE instance_uid = ? ORDER BY revision DESC`, uid[:])
	if err != nil {
		return nil, fmt.Errorf("list configuration revisions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close configuration history: %w", err))
		}
	}()
	revisions = make([]RevisionSummary, 0)
	for rows.Next() {
		var revision RevisionSummary
		var createdAt sql.NullInt64
		if err := rows.Scan(&revision.Number, &createdAt); err != nil {
			return nil, fmt.Errorf("read configuration revision metadata: %w", err)
		}
		if createdAt.Valid {
			date := time.UnixMilli(createdAt.Int64).UTC()
			revision.CreatedAt = &date
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read configuration history: %w", err)
	}
	return revisions, nil
}

// ErrRevisionNotFound reports that this Agent has no revision with that number.
var ErrRevisionNotFound = errors.New("configuration revision not found")

// RevisionContent returns the exact published YAML, without recompiling it.
func (store *Store) RevisionContent(ctx context.Context, uid agent.InstanceUID, number int64) ([]byte, error) {
	var content []byte
	err := store.db.QueryRowContext(ctx, `SELECT content FROM agent_config_revisions
		WHERE instance_uid = ? AND revision = ?`, uid[:], number).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRevisionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read configuration revision content: %w", err)
	}
	return content, nil
}
