package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store persists and retrieves agents.
type Store struct {
	db *sql.DB
}

// UpsertParams contains the agent state to persist.
type UpsertParams struct {
	InstanceUID InstanceUID
	Hostname    *string
	Version     *string
	Connected   bool
	LastSeenAt  *time.Time
}

// NewStore requires a migrated database opened by database.OpenFile or
// OpenExistingFile. The caller owns its closure.
// NewStore creates an agent store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Exists reports whether the Agent has been registered, regardless of connectivity.
func (store *Store) Exists(ctx context.Context, uid InstanceUID) (bool, error) {
	var exists bool
	if err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE instance_uid=?)`, uid[:]).Scan(&exists); err != nil {
		return false, fmt.Errorf("check agent existence: %w", err)
	}
	return exists, nil
}

// Upsert stores an agent or updates its connection state.
func (store *Store) Upsert(
	ctx context.Context,
	params UpsertParams,
) error {
	_, err := store.db.ExecContext(
		ctx,
		`INSERT INTO agents (
			instance_uid,
			hostname,
			version,
			connected,
			last_seen_at_unix_ms
		 ) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (instance_uid) DO UPDATE
		 SET hostname = COALESCE(excluded.hostname, agents.hostname),
			 version = COALESCE(excluded.version, agents.version),
			 connected = excluded.connected,
			 last_seen_at_unix_ms = COALESCE(
				excluded.last_seen_at_unix_ms,
				agents.last_seen_at_unix_ms
			 )`,
		params.InstanceUID[:],
		nullableString(params.Hostname),
		nullableString(params.Version),
		params.Connected,
		nullableTime(params.LastSeenAt),
	)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}

	return nil
}

// MarkAllDisconnected marks every connected agent as disconnected.
func (store *Store) MarkAllDisconnected(ctx context.Context) error {
	_, err := store.db.ExecContext(
		ctx,
		"UPDATE agents SET connected = 0 WHERE connected = 1",
	)
	if err != nil {
		return fmt.Errorf("mark all agents disconnected: %w", err)
	}

	return nil
}

// List returns every stored agent ordered by instance UID.
func (store *Store) List(
	ctx context.Context,
) (agents []Agent, returnErr error) {
	rows, err := store.db.QueryContext(
		ctx,
		`SELECT
			instance_uid,
			hostname,
			version,
			connected,
			last_seen_at_unix_ms
		 FROM agents
		 ORDER BY instance_uid`,
	)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close agent rows: %w", err),
			)
		}
	}()

	agents = make([]Agent, 0)
	for rows.Next() {
		var value []byte
		var hostname sql.NullString
		var version sql.NullString
		var connected bool
		var lastSeenAt sql.NullInt64
		if err := rows.Scan(
			&value,
			&hostname,
			&version,
			&connected,
			&lastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent instance UID: %w", err)
		}

		uid, err := NewInstanceUID(value)
		if err != nil {
			return nil, fmt.Errorf("decode agent instance UID: %w", err)
		}

		agents = append(agents, Agent{
			InstanceUID: uid,
			Hostname:    stringPointer(hostname),
			Version:     stringPointer(version),
			Connected:   connected,
			LastSeenAt:  timePointer(lastSeenAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}

	return agents, nil
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: *value, Valid: true}
}

func nullableTime(value *time.Time) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{
		Int64: value.UTC().UnixMilli(),
		Valid: true,
	}
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return &value.String
}

func timePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}

	timestamp := time.UnixMilli(value.Int64).UTC()

	return &timestamp
}
