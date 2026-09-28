package alert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

// Publisher reconciles SQLite definitions with one owned Prometheus rule file.
// The application calls Sync serially; it owns the store and client lifetimes.
type Publisher struct {
	store  *Store
	client *prometheus.Client
	path   string
}

// NewPublisher locates the derived file beneath the controller data directory.
func NewPublisher(store *Store, client *prometheus.Client, dataDirectory string) *Publisher {
	return &Publisher{
		store: store, client: client,
		path: filepath.Join(dataDirectory, "config", "alert-rules", "monitors.yml"),
	}
}

// Prepare removes the obsolete experimental rule file before the owned engine
// starts without that feature flag. SQLite definitions are untouched; Sync
// validates and republishes them once the engine is available. Call before Sync.
// Current files are retained so ordinary restarts can restore native rule state.
func (publisher *Publisher) Prepare() error {
	content, err := os.ReadFile(publisher.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect generated alert rules before startup: %w", err)
	}
	if !bytes.Contains(content, []byte("ts_of_last_over_time(")) {
		return nil
	}
	if err := os.Remove(publisher.path); err != nil {
		return fmt.Errorf("remove obsolete generated alert rules: %w", err)
	}
	return nil
}

// Sync validates changed expressions, replaces the file, reloads and verifies the
// loaded definitions. Failed reloads are retried even when the file is unchanged.
func (publisher *Publisher) Sync(ctx context.Context) error {
	definitions, err := publisher.store.definitions(ctx, "")
	if err != nil {
		return err
	}
	rules := make([]prometheusRule, 0, len(definitions))
	for _, definition := range definitions {
		rule, err := definition.compile()
		if err != nil {
			return err
		}
		rules = append(rules, rule)
	}
	content, err := marshalRules(rules)
	if err != nil {
		return err
	}
	previous, err := os.ReadFile(publisher.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read published alert rules: %w", err)
	}
	if errors.Is(err, os.ErrNotExist) && len(rules) == 0 {
		return nil
	}
	if !bytes.Equal(content, previous) {
		for _, rule := range rules {
			if err := publisher.client.ValidateExpression(ctx, rule.Expr); err != nil {
				return fmt.Errorf("validate generated alert expression: %w", err)
			}
		}
		if err := publisher.replace(content); err != nil {
			return err
		}
	} else {
		loaded, err := publisher.client.AlertRules(ctx, ruleGroupName)
		if err != nil {
			return fmt.Errorf("read loaded alert rules: %w", err)
		}
		if rulesMatch(rules, loaded) {
			return nil
		}
	}
	if err := publisher.client.Reload(ctx); err != nil {
		return fmt.Errorf("reload alert rules: %w", err)
	}
	loaded, err := publisher.client.AlertRules(ctx, ruleGroupName)
	if err != nil {
		return fmt.Errorf("verify loaded alert rules: %w", err)
	}
	if !rulesMatch(rules, loaded) {
		return errors.New("desired alert rules are not loaded in Prometheus")
	}
	return nil
}

func marshalRules(rules []prometheusRule) ([]byte, error) {
	type group struct {
		Name     string           `yaml:"name"`
		Interval string           `yaml:"interval"`
		Rules    []prometheusRule `yaml:"rules"`
	}
	content, err := yaml.Marshal(struct {
		Groups []group `yaml:"groups"`
	}{Groups: []group{{Name: ruleGroupName, Interval: "5s", Rules: rules}}})
	if err != nil {
		return nil, fmt.Errorf("encode alert rule file: %w", err)
	}
	return content, nil
}

func (publisher *Publisher) replace(content []byte) error {
	if err := os.MkdirAll(filepath.Dir(publisher.path), 0o750); err != nil {
		return fmt.Errorf("create alert rules directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(publisher.path), ".monitors-*.partial")
	if err != nil {
		return fmt.Errorf("create temporary alert rules: %w", err)
	}
	defer func() {
		_ = file.Close()           //nolint:errcheck // best-effort cleanup after the primary operation
		_ = os.Remove(file.Name()) //nolint:errcheck // renamed files no longer exist at this path
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write temporary alert rules: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary alert rules: %w", err)
	}
	if err := os.Rename(file.Name(), publisher.path); err != nil {
		return fmt.Errorf("replace alert rules: %w", err)
	}
	return nil
}

func ruleMatches(want prometheusRule, got prometheus.AlertRule) bool {
	seconds, err := strconv.ParseFloat(strings.TrimSuffix(want.For, "s"), 64)
	return err == nil && got.Name == want.Alert && got.Duration == seconds && maps.Equal(got.Labels, want.Labels)
}

func rulesMatch(want []prometheusRule, got []prometheus.AlertRule) bool {
	if len(want) != len(got) {
		return false
	}
	byID := make(map[string]prometheus.AlertRule, len(got))
	for _, rule := range got {
		byID[rule.Labels["arveld_rule_id"]] = rule
	}
	for _, rule := range want {
		if !ruleMatches(rule, byID[rule.Labels["arveld_rule_id"]]) {
			return false
		}
	}
	return true
}
