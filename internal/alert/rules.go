// Package alert owns persisted resource alert rules and their publication to Prometheus.
package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// ErrNotFound means the requested rule does not exist.
var ErrNotFound = errors.New("alert rule not found")

// ErrMonitorNotFound means the rule's owning Monitor does not exist.
var ErrMonitorNotFound = errors.New("alert rule Monitor not found")

// ErrAgentNotFound means the rule owner does not exist.
var ErrAgentNotFound = errors.New("alert rule Agent not found")

// ErrInvalidRule means the condition, duration, severity or destinations are unsupported.
var ErrInvalidRule = errors.New("invalid alert rule")

// Rule describes a sustained condition owned by exactly one Monitor or Agent.
type Rule struct {
	ID               string
	MonitorID        string
	AgentInstanceUID *agent.InstanceUID
	Condition        string
	Threshold        *float64
	ForSeconds       int
	Severity         string
	NotificationIDs  []string
}

// Store persists and retrieves alert rules.
type Store struct{ db *sql.DB }

// NewStore requires a migrated database. The caller owns its closure.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get reads one rule or returns ErrNotFound.
func (store *Store) Get(ctx context.Context, id string) (Rule, error) {
	if id == "" {
		return Rule{}, ErrNotFound
	}
	values, err := store.read(ctx, id, "", nil)
	if err != nil {
		return Rule{}, err
	}
	if len(values) == 0 {
		return Rule{}, ErrNotFound
	}
	return values[0], nil
}

// List reads a Monitor's rules in stable ID order.
func (store *Store) List(ctx context.Context, monitorID string) ([]Rule, error) {
	return store.read(ctx, "", monitorID, nil)
}

func (store *Store) read(ctx context.Context, id, monitorID string, agentID []byte) (values []Rule, returnErr error) {
	rows, err := store.db.QueryContext(ctx, `SELECT r.id, COALESCE(r.monitor_id,''), r.agent_instance_uid, r.condition, r.threshold, r.for_seconds, r.severity,
  (SELECT json_group_array(notification_id) FROM
   (SELECT notification_id FROM alert_rule_notifications WHERE rule_id=r.id ORDER BY notification_id))
  FROM alert_rules r WHERE (?='' OR r.id=?) AND (?='' OR r.monitor_id=?) AND (? IS NULL OR r.agent_instance_uid=?) ORDER BY r.id`, id, id, monitorID, monitorID, agentID, agentID)
	if err != nil {
		return nil, fmt.Errorf("read alert rules: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close alert rules: %w", err))
		}
	}()
	values = make([]Rule, 0)
	for rows.Next() {
		var value Rule
		var destinations string
		var uid []byte
		if err := rows.Scan(&value.ID, &value.MonitorID, &uid, &value.Condition, &value.Threshold, &value.ForSeconds, &value.Severity, &destinations); err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}
		if uid != nil {
			owner, err := agent.NewInstanceUID(uid)
			if err != nil {
				return nil, fmt.Errorf("decode alert owner: %w", err)
			}
			value.AgentInstanceUID = &owner
		}
		if err := json.Unmarshal([]byte(destinations), &value.NotificationIDs); err != nil {
			return nil, fmt.Errorf("decode alert destinations: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert rules: %w", err)
	}
	return values, nil
}

// ListAgent reads the rules belonging directly to an existing Agent.
func (store *Store) ListAgent(ctx context.Context, uid agent.InstanceUID) ([]Rule, error) {
	var exists bool
	if err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE instance_uid=?)`, uid[:]).Scan(&exists); err != nil {
		return nil, fmt.Errorf("read alert owner: %w", err)
	}
	if !exists {
		return nil, ErrAgentNotFound
	}
	return store.read(ctx, "", "", uid[:])
}

// Create validates and persists a rule and its destinations in one transaction.
func (store *Store) Create(ctx context.Context, value Rule) error {
	return store.save(ctx, value, false)
}

// Update replaces settings and destinations while preserving Monitor ownership.
func (store *Store) Update(ctx context.Context, value Rule) error {
	return store.save(ctx, value, true)
}

func validateCondition(value Rule) error {
	if (value.MonitorID == "") == (value.AgentInstanceUID == nil) {
		return ErrInvalidRule
	}
	agentOwned := value.AgentInstanceUID != nil
	if value.ForSeconds < 1 || value.ForSeconds > 86400 {
		return ErrInvalidRule
	}
	switch value.Condition {
	case "failed", "no_data":
		if agentOwned || value.Threshold != nil {
			return ErrInvalidRule
		}
	case "latency":
		if agentOwned || value.Threshold == nil || math.IsNaN(*value.Threshold) || *value.Threshold <= 0 || *value.Threshold > 60000 {
			return ErrInvalidRule
		}
	case "cpu", "memory", "disk":
		if !agentOwned || value.Threshold == nil || math.IsNaN(*value.Threshold) || *value.Threshold <= 0 || *value.Threshold > 100 {
			return ErrInvalidRule
		}
	default:
		return ErrInvalidRule
	}
	return nil
}

func validateRule(value Rule) error {
	if err := validateCondition(value); err != nil {
		return err
	}
	switch value.Severity {
	case "info", "warning", "critical":
	default:
		return ErrInvalidRule
	}
	if len(value.NotificationIDs) > 64 {
		return ErrInvalidRule
	}
	seen := make(map[string]bool, len(value.NotificationIDs))
	for _, id := range value.NotificationIDs {
		if id == "" || seen[id] {
			return ErrInvalidRule
		}
		seen[id] = true
	}
	return nil
}

func (store *Store) save(ctx context.Context, value Rule, update bool) (returnErr error) {
	if err := validateRule(value); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alert rule write: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back alert rule: %w", err))
		}
	}()
	var uid []byte
	if value.AgentInstanceUID != nil {
		uid = value.AgentInstanceUID[:]
	}
	var result sql.Result
	if update {
		result, err = tx.ExecContext(ctx, `UPDATE alert_rules SET condition=?, threshold=?, for_seconds=?, severity=? WHERE id=?`, value.Condition, value.Threshold, value.ForSeconds, value.Severity, value.ID)
	} else {
		result, err = tx.ExecContext(ctx, `INSERT INTO alert_rules (id,monitor_id,agent_instance_uid,condition,threshold,for_seconds,severity)
   SELECT ?,NULLIF(?,''),?,?,?,?,? WHERE EXISTS (SELECT 1 FROM monitors WHERE id=?) OR EXISTS (SELECT 1 FROM agents WHERE instance_uid=?)`, value.ID, value.MonitorID, uid, value.Condition, value.Threshold, value.ForSeconds, value.Severity, value.MonitorID, uid)
	}
	if err != nil {
		return fmt.Errorf("save alert rule: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read alert rule write result: %w", err)
	}
	if count == 0 {
		if update {
			return ErrNotFound
		}
		if value.AgentInstanceUID != nil {
			return ErrAgentNotFound
		}
		return ErrMonitorNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rule_notifications WHERE rule_id=?`, value.ID); err != nil {
		return fmt.Errorf("replace alert destinations: %w", err)
	}
	for _, id := range value.NotificationIDs {
		result, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_notifications (rule_id,notification_id)
   SELECT ?,? WHERE EXISTS (SELECT 1 FROM notification_channels WHERE id=?)`, value.ID, id, id)
		if err != nil {
			return fmt.Errorf("save alert destination: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read alert destination write result: %w", err)
		}
		if count == 0 {
			return ErrInvalidRule
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alert rule: %w", err)
	}
	return nil
}

// Delete removes a rule and its destination assignments.
func (store *Store) Delete(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read alert rule deletion result: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
