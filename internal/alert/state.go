package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

// ErrEngine means native rule state could not be read reliably.
var ErrEngine = errors.New("alert engine unavailable")

// State separates configuration convergence from Prometheus evaluation.
// Inactive means no native alert is active; it does not establish Monitor health.
type State struct {
	RuleID           string    `json:"rule_id"`
	AgentInstanceUID string    `json:"agent_instance_uid,omitempty"`
	MonitorID        string    `json:"monitor_id,omitempty"`
	SyncStatus       string    `json:"sync_status"`
	State            string    `json:"state,omitempty"`
	Health           string    `json:"health,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	LastEvaluation   time.Time `json:"last_evaluation,omitzero"`
}

// ReadState reports native state only when the current SQLite definition is loaded.
func ReadState(ctx context.Context, store *Store, client *prometheus.Client, id string) (State, error) {
	if id == "" {
		return State{}, ErrNotFound
	}
	definitions, err := store.definitions(ctx, id)
	if err != nil {
		return State{}, err
	}
	if len(definitions) == 0 {
		return State{}, ErrNotFound
	}
	definition := definitions[0]
	want, err := definition.compile()
	if err != nil {
		return State{}, err
	}
	loaded, err := client.AlertRules(ctx, ruleGroupName)
	if err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrEngine, err)
	}
	state := State{RuleID: id, MonitorID: definition.rule.MonitorID, SyncStatus: "pending"}
	if definition.rule.AgentInstanceUID != nil {
		state.AgentInstanceUID = definition.rule.AgentInstanceUID.String()
	}
	var matches int
	for _, rule := range loaded {
		if !ruleMatches(want, rule) {
			continue
		}
		matches++
		state.SyncStatus, state.State, state.Health = "applied", rule.State, rule.Health
		state.LastEvaluation, state.LastError = rule.LastEvaluation, rule.LastError
	}
	if matches > 1 {
		return State{}, fmt.Errorf("%w: duplicate loaded rules", ErrEngine)
	}
	return state, nil
}
