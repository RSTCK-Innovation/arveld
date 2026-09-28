package alert

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

const ruleGroupName = "arveld-monitor-alerts"

type definition struct {
	rule       Rule
	owner      monitor.Monitor
	assertions int
	ownerName  string
}

type prometheusRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

// definitionReader allows the same snapshot query on a database or transaction.
type definitionReader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (store *Store) definitions(ctx context.Context, id string) ([]definition, error) {
	return readDefinitions(ctx, store.db, id)
}

// readDefinitions includes resource ownership without reading private Monitor headers/bodies.
func readDefinitions(ctx context.Context, reader definitionReader, id string) (values []definition, returnErr error) {
	rows, err := reader.QueryContext(ctx, `SELECT r.id, COALESCE(r.monitor_id,''), r.condition, r.threshold, r.for_seconds, r.severity,
		COALESCE(m.protocol,''), COALESCE(r.agent_instance_uid,m.agent_instance_uid), COALESCE(m.interval_seconds,0), COALESCE(m.timeout_seconds,0),
		COALESCE(json_array_length(m.options, '$.validations'), 0), COALESCE(m.name,a.hostname,'')
		FROM alert_rules AS r LEFT JOIN monitors AS m ON m.id = r.monitor_id
 LEFT JOIN agents AS a ON a.instance_uid=r.agent_instance_uid
		WHERE (? = '' OR r.id = ?) ORDER BY r.id`, id, id)
	if err != nil {
		return nil, fmt.Errorf("read alert rule definitions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close alert rule definitions: %w", err))
		}
	}()
	for rows.Next() {
		var value definition
		var uid []byte
		if err := rows.Scan(&value.rule.ID, &value.rule.MonitorID, &value.rule.Condition, &value.rule.Threshold, &value.rule.ForSeconds,
			&value.rule.Severity, &value.owner.Protocol, &uid, &value.owner.IntervalSeconds,
			&value.owner.TimeoutSeconds, &value.assertions, &value.ownerName); err != nil {
			return nil, fmt.Errorf("scan alert rule definition: %w", err)
		}
		value.owner.ID = value.rule.MonitorID
		value.owner.AgentInstanceUID, err = agent.NewInstanceUID(uid)
		if err != nil {
			return nil, fmt.Errorf("decode alert Monitor Agent UID: %w", err)
		}
		if value.rule.MonitorID == "" {
			value.rule.AgentInstanceUID = &value.owner.AgentInstanceUID
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert rule definitions: %w", err)
	}
	return values, nil
}

func (value definition) compile() (prometheusRule, error) {
	var expression string
	var err error
	if value.rule.AgentInstanceUID != nil {
		expression, err = agentConditionExpression(*value.rule.AgentInstanceUID, value.rule.Condition, value.rule.Threshold)
	} else {
		expression, err = conditionExpression(value.owner, value.assertions, value.rule.Condition, value.rule.Threshold)
	}
	if err != nil {
		return prometheusRule{}, err
	}
	name := "ArveldMonitorFailed"
	switch value.rule.Condition {
	case "no_data":
		name = "ArveldMonitorNoData"
	case "latency":
		name = "ArveldMonitorLatency"
	case "cpu":
		name = "ArveldAgentCPU"
	case "memory":
		name = "ArveldAgentMemory"
	case "disk":
		name = "ArveldAgentDisk"
	}
	duration := strconv.Itoa(value.rule.ForSeconds) + "s"
	// A changed condition/owner/duration/severity is a new native alert identity.
	// This prevents a previous definition's pending timer being reused accidentally.
	revision := sha256.Sum256([]byte(expression + "\n" + duration + "\n" + value.rule.Severity))
	rule := prometheusRule{
		Alert: name, Expr: expression, For: duration,
		Annotations: value.annotations(),
		Labels: map[string]string{
			"arveld_rule_id": value.rule.ID, "arveld_monitor_id": value.rule.MonitorID,
			"arveld_agent_id": value.owner.AgentInstanceUID.String(), "severity": value.rule.Severity,
			"arveld_rule_revision": hex.EncodeToString(revision[:]),
		},
	}
	if value.rule.AgentInstanceUID != nil {
		delete(rule.Labels, "arveld_monitor_id")
	}
	return rule, nil
}
