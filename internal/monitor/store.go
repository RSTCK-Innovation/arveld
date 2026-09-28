// Package monitor owns persisted Monitor definitions and Agent assignments.
package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// ErrNotFound means the requested Monitor does not exist.
var ErrNotFound = errors.New("monitor not found")

// ErrAgentNotFound means the assigned Agent does not exist.
var ErrAgentNotFound = errors.New("assigned Agent not found")

// Store persists and retrieves Monitors.
type Store struct {
	db *sql.DB
}

// NewStore requires a migrated database opened by database.OpenFile or
// OpenExistingFile. The caller owns its closure.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const monitorColumns = `id, name, protocol, agent_instance_uid, endpoint, method,
	interval_seconds, timeout_seconds, ping_count, dns_server, record_type, transport, options, skip_tls_verify`

// Get reads one definition or returns ErrNotFound.
func (store *Store) Get(ctx context.Context, id string) (Monitor, error) {
	value, err := scanMonitor(store.db.QueryRowContext(ctx,
		`SELECT `+monitorColumns+` FROM monitors WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	return value, err
}

// List reads all definitions in stable ID order.
func (store *Store) List(ctx context.Context) ([]Monitor, error) {
	return listMonitors(ctx, store.db, `SELECT `+monitorColumns+` FROM monitors ORDER BY id`)
}

// ListForAgent reads the assigned set through a database or publication transaction.
// The caller owns the reader; this function closes the query's rows.
func ListForAgent(ctx context.Context, reader queryer, uid agent.InstanceUID) ([]Monitor, error) {
	return listMonitors(ctx, reader,
		`SELECT `+monitorColumns+` FROM monitors WHERE agent_instance_uid = ? ORDER BY id`, uid[:])
}

// Create persists caller-validated settings without overwriting an ID.
// The assigned Agent must already exist.
func (store *Store) Create(ctx context.Context, value Monitor) error {
	options, err := json.Marshal(value.HTTPOptions)
	if err != nil {
		return fmt.Errorf("encode Monitor options: %w", err)
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO monitors (`+monitorColumns+`)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM agents WHERE instance_uid = ?)`,
		value.ID, value.Name, value.Protocol, value.AgentInstanceUID[:], value.Endpoint, value.Method,
		value.IntervalSeconds, value.TimeoutSeconds, value.PingCount,
		value.DNSServer, value.RecordType, value.Transport, string(options), value.SkipTLSVerify, value.AgentInstanceUID[:])
	if err != nil {
		return fmt.Errorf("create Monitor: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Monitor creation result: %w", err)
	}
	if count == 0 {
		return ErrAgentNotFound
	}
	return nil
}

// Update replaces caller-validated settings while preserving ID and protocol.
// It returns the previous Agent atomically so callers can reconcile both assignments.
func (store *Store) Update(ctx context.Context, value Monitor) (previous agent.InstanceUID, returnErr error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return previous, fmt.Errorf("begin Monitor update: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back Monitor update: %w", err))
		}
	}()

	var uid []byte
	err = tx.QueryRowContext(ctx, `SELECT agent_instance_uid FROM monitors WHERE id = ? AND protocol = ?`,
		value.ID, value.Protocol).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return previous, ErrNotFound
	}
	if err != nil {
		return previous, fmt.Errorf("read previous Monitor assignment: %w", err)
	}
	previous, err = agent.NewInstanceUID(uid)
	if err != nil {
		return previous, fmt.Errorf("decode previous Monitor Agent UID: %w", err)
	}
	options, err := json.Marshal(value.HTTPOptions)
	if err != nil {
		return previous, fmt.Errorf("encode Monitor options: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE monitors SET
		name = ?, agent_instance_uid = ?, endpoint = ?, method = ?, interval_seconds = ?, timeout_seconds = ?,
		ping_count = ?, dns_server = ?, record_type = ?, transport = ?, options = ?, skip_tls_verify = ?
		WHERE id = ? AND EXISTS (SELECT 1 FROM agents WHERE instance_uid = ?)`,
		value.Name, value.AgentInstanceUID[:], value.Endpoint, value.Method, value.IntervalSeconds, value.TimeoutSeconds,
		value.PingCount, value.DNSServer, value.RecordType, value.Transport, string(options), value.SkipTLSVerify, value.ID, value.AgentInstanceUID[:])
	if err != nil {
		return previous, fmt.Errorf("update Monitor: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return previous, fmt.Errorf("read Monitor update result: %w", err)
	}
	if count == 0 {
		return previous, ErrAgentNotFound
	}
	if err := tx.Commit(); err != nil {
		return previous, fmt.Errorf("commit Monitor update: %w", err)
	}
	return previous, nil
}

// Delete removes a definition and returns its last assignment atomically.
// Historical Agent configuration revisions are retained.
func (store *Store) Delete(ctx context.Context, id string) (Monitor, error) {
	value, err := scanMonitor(store.db.QueryRowContext(ctx,
		`DELETE FROM monitors WHERE id = ? RETURNING `+monitorColumns, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("delete Monitor: %w", err)
	}
	return value, nil
}

// queryer is implemented by both *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func listMonitors(ctx context.Context, reader queryer, query string, args ...any) (values []Monitor, returnErr error) {
	rows, err := reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list Monitors: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Monitors: %w", err))
		}
	}()
	for rows.Next() {
		value, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Monitors: %w", err)
	}
	return values, nil
}

func scanMonitor(row scanner) (Monitor, error) {
	var value Monitor
	var options string
	var uid []byte
	if err := row.Scan(&value.ID, &value.Name, &value.Protocol, &uid, &value.Endpoint, &value.Method,
		&value.IntervalSeconds, &value.TimeoutSeconds, &value.PingCount,
		&value.DNSServer, &value.RecordType, &value.Transport, &options, &value.SkipTLSVerify); err != nil {
		return Monitor{}, fmt.Errorf("read Monitor: %w", err)
	}
	if err := json.Unmarshal([]byte(options), &value.HTTPOptions); err != nil {
		return Monitor{}, fmt.Errorf("decode Monitor options: %w", err)
	}
	var err error
	value.AgentInstanceUID, err = agent.NewInstanceUID(uid)
	if err != nil {
		return Monitor{}, fmt.Errorf("decode Monitor Agent UID: %w", err)
	}
	return value, nil
}
