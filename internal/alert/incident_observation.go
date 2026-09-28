package alert

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

// ObserveIncidents records native states without evaluating metrics or alert timers.
// The caller invokes it serially after successful publication. Network reads happen
// before the transaction; current definitions inside it fence stale engine responses.
func ObserveIncidents(ctx context.Context, store *Store, client *prometheus.Client) (returnErr error) {
	var needed bool
	if err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_rules) OR EXISTS(SELECT 1 FROM incidents WHERE closed_at IS NULL)`).Scan(&needed); err != nil {
		return fmt.Errorf("check incident observation: %w", err)
	}
	if !needed {
		return nil
	}
	loaded, err := client.AlertRules(ctx, ruleGroupName)
	if err != nil {
		return fmt.Errorf("read incident evaluation: %w", err)
	}
	byID := make(map[string]prometheus.AlertRule, len(loaded))
	for _, rule := range loaded {
		id := rule.Labels["arveld_rule_id"]
		if _, exists := byID[id]; exists {
			return fmt.Errorf("%w: duplicate incident rule", ErrEngine)
		}
		byID[id] = rule
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin incident observation: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back incident observation: %w", err))
		}
	}()
	definitions, err := readDefinitions(ctx, tx, "")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, definition := range definitions {
		want, err := definition.compile()
		if err != nil {
			return err
		}
		got := byID[definition.rule.ID]
		if !observableRule(want, got, now) {
			continue
		}
		if got.State != "inactive" && got.State != "firing" && got.State != "pending" {
			continue
		}
		if err := recordIncidentState(ctx, tx, definition, got, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE incidents SET closed_at=?,close_reason='rule_removed' WHERE closed_at IS NULL AND NOT EXISTS(SELECT 1 FROM alert_rules WHERE id=incidents.rule_id)`, now.UnixMilli()); err != nil {
		return fmt.Errorf("close removed rule incidents: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit incident observation: %w", err)
	}
	return nil
}

func recordIncidentState(ctx context.Context, tx *sql.Tx, definition definition, native prometheus.AlertRule, now time.Time) error {
	rule := definition.rule
	revision := native.Labels["arveld_rule_revision"]
	evaluated := native.LastEvaluation.UnixMilli()
	// Replacing a definition ends its previous episode without claiming recovery.
	if _, err := tx.ExecContext(ctx, `UPDATE incidents SET closed_at=?,close_reason='rule_changed' WHERE rule_id=? AND closed_at IS NULL AND rule_revision<>? AND last_evaluated_at<=?`, now.UnixMilli(), rule.ID, revision, evaluated); err != nil {
		return fmt.Errorf("close changed rule incident: %w", err)
	}
	if native.State == "firing" {
		kind, id, name := "monitor", rule.MonitorID, definition.ownerName
		if rule.AgentInstanceUID != nil {
			kind, id = "agent", rule.AgentInstanceUID.String()
		}
		if name == "" {
			name = id
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO incidents
            (rule_id,rule_revision,owner_kind,owner_id,owner_name,condition,threshold,for_seconds,severity,opened_at,last_evaluated_at)
            SELECT ?,?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM incidents WHERE rule_id=? AND closed_at IS NULL)
            AND NOT EXISTS(SELECT 1 FROM incidents WHERE rule_id=? AND last_evaluated_at>=?)`,
			rule.ID, revision, kind, id, name, rule.Condition, rule.Threshold, rule.ForSeconds, rule.Severity, now.UnixMilli(), evaluated, rule.ID, rule.ID, evaluated); err != nil {
			return fmt.Errorf("open incident: %w", err)
		}
	}
	if native.State == "inactive" {
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET closed_at=?,close_reason='condition_ended',last_evaluated_at=? WHERE rule_id=? AND rule_revision=? AND closed_at IS NULL AND last_evaluated_at<?`, now.UnixMilli(), evaluated, rule.ID, revision, evaluated); err != nil {
			return fmt.Errorf("close ended condition incident: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET last_evaluated_at=? WHERE rule_id=? AND rule_revision=? AND closed_at IS NULL AND last_evaluated_at<?`, evaluated, rule.ID, revision, evaluated); err != nil {
			return fmt.Errorf("refresh incident evaluation: %w", err)
		}
	}
	return nil
}

func observableRule(want prometheusRule, got prometheus.AlertRule, now time.Time) bool {
	return ruleMatches(want, got) && got.Health == "ok" && !got.LastEvaluation.IsZero() && !got.LastEvaluation.After(now) && now.Sub(got.LastEvaluation) <= 30*time.Second
}
