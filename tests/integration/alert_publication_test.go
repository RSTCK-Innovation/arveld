package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestPublisherLoadsPersistedRuleIntoPrometheus(t *testing.T) {
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	store := alert.NewStore(db)
	if err := store.Create(t.Context(), alert.Rule{
		ID: "homepage-failed", MonitorID: "homepage", Condition: "failed", ForSeconds: 120, Severity: "critical",
	}); err != nil {
		t.Fatal(err)
	}
	rulesPath := filepath.Join(directory, "config", "alert-rules", "monitors.yml")
	var validated string
	var loaded []map[string]any
	var reloads int
	var mu sync.Mutex
	var published []byte
	var rejectValidation, rejectReload, omitLoading bool
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/format_query":
			content, err := os.ReadFile(rulesPath) //nolint:gosec // Generated path in the test directory.
			if err != nil && !os.IsNotExist(err) {
				t.Error(err)
			}
			if !bytes.Equal(content, published) {
				t.Error("validation must see the previous file, never the candidate")
			}
			if rejectValidation {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			validated = r.FormValue("query")
			if validated == "" {
				t.Error("missing PromQL expression")
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": validated}); err != nil {
				t.Error(err)
			}
		case "/-/reload":
			if rejectReload {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if omitLoading {
				return
			}
			if r.Method != http.MethodPost {
				t.Error("reload must use POST")
			}
			content, err := os.ReadFile(rulesPath) //nolint:gosec // Generated path in the test directory.
			if err != nil {
				t.Error(err)
				return
			}
			var file struct {
				Groups []struct {
					Rules []struct {
						Alert       string            `yaml:"alert"`
						Expr        string            `yaml:"expr"`
						For         string            `yaml:"for"`
						Labels      map[string]string `yaml:"labels"`
						Annotations map[string]string `yaml:"annotations"`
					} `yaml:"rules"`
				} `yaml:"groups"`
			}
			if err := yaml.Unmarshal(content, &file); err != nil || len(file.Groups) != 1 || len(file.Groups[0].Rules) > 1 {
				t.Errorf("reload must see a complete YAML rule group: %s (%v)", content, err)
				return
			}
			published = content
			if len(file.Groups[0].Rules) == 0 {
				loaded = nil
				reloads++
				return
			}
			rule := file.Groups[0].Rules[0]
			if rule.Annotations["resource"] != `{{ "Homepage" }}` || rule.Annotations["summary"] != "Check failed" || rule.Annotations["description"] == "" {
				t.Errorf("published alert lacks human-readable context: %+v", rule.Annotations)
			}
			if rule.Expr != validated || rule.For != "120s" || rule.Labels["severity"] != "critical" ||
				rule.Labels["arveld_rule_id"] != "homepage-failed" || rule.Labels["arveld_monitor_id"] != "homepage" {
				t.Errorf("published rule does not match the persisted definition: %+v", rule)
			}
			loaded = []map[string]any{{
				"name": rule.Alert, "query": rule.Expr, "duration": 120,
				"labels": rule.Labels, "type": "alerting", "state": "pending", "health": "ok", "lastEvaluation": time.Now().UTC(),
			}}
			reloads++
		case "/api/v1/rules":
			if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{
				"groups": []map[string]any{{"name": "arveld-monitor-alerts", "rules": loaded}},
			}}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected engine API: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(engine.Close)
	client, err := prometheus.NewClient(engine.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	publisher := alert.NewPublisher(store, client, directory)
	if err := publisher.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0o750); err != nil {
		t.Fatal(err)
	}
	// A previous Arveld build generated this file with experimental PromQL enabled.
	legacy := []byte("groups:\n  - name: arveld-monitor-alerts\n    rules:\n      - alert: ArveldMonitorFailed\n        expr: ts_of_last_over_time(tcpcheck_status_ratio[1m]) == 0\n")
	if err := os.WriteFile(rulesPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := alert.ReadState(t.Context(), store, client, "homepage-failed")
	if err != nil || state.SyncStatus != "applied" || state.State != "pending" || state.Health != "ok" {
		t.Fatalf("native rule state = %+v, %v, want applied/pending/ok", state, err)
	}
	if err := publisher.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := reloads
	before := bytes.Clone(published)
	rejectValidation = true
	mu.Unlock()
	if count != 1 {
		t.Fatalf("unchanged, verified rules must not reload again: got %d reloads", count)
	}
	// A Monitor reassignment invalidates the loaded rule revision immediately.
	nextUID := agent.InstanceUID{2}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: nextUID}); err != nil {
		t.Fatal(err)
	}
	owner, err := monitor.NewStore(db).Get(t.Context(), "homepage")
	if err != nil {
		t.Fatal(err)
	}
	owner.AgentInstanceUID = nextUID
	if _, err := monitor.NewStore(db).Update(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	assertPending := func() {
		t.Helper()
		state, err := alert.ReadState(t.Context(), store, client, "homepage-failed")
		if err != nil || state.SyncStatus != "pending" || state.State != "" {
			t.Fatalf("stale definition must not expose evaluation state: %+v, %v", state, err)
		}
	}
	assertPending()
	if err := publisher.Sync(t.Context()); err == nil {
		t.Fatal("rejected expression must fail publication")
	}
	content, err := os.ReadFile(rulesPath) //nolint:gosec // Generated path in the test directory.
	if err != nil || !bytes.Equal(content, before) {
		t.Fatal("rejected expression replaced the previous file")
	}
	mu.Lock()
	rejectValidation = false
	rejectReload = true
	mu.Unlock()
	if err := publisher.Sync(t.Context()); err == nil {
		t.Fatal("failed reload must fail publication")
	}
	assertPending()
	mu.Lock()
	rejectReload = false
	omitLoading = true
	mu.Unlock()
	if err := publisher.Sync(t.Context()); err == nil {
		t.Fatal("HTTP 200 without loaded rules must fail verification")
	}
	assertPending()
	mu.Lock()
	omitLoading = false
	mu.Unlock()
	// Retry uses the already written candidate and still performs the failed reload.
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = alert.ReadState(t.Context(), store, client, "homepage-failed")
	if err != nil || state.SyncStatus != "applied" {
		t.Fatalf("retried publication = %+v, %v", state, err)
	}
	if _, err := monitor.NewStore(db).Delete(t.Context(), "homepage"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	rules, err := client.AlertRules(t.Context(), "arveld-monitor-alerts")
	if err != nil || len(rules) != 0 {
		t.Fatalf("deleted Monitor still has loaded rules: %+v, %v", rules, err)
	}
	entries, err := os.ReadDir(filepath.Dir(rulesPath))
	if err != nil || len(entries) != 1 || entries[0].Name() != "monitors.yml" {
		t.Fatalf("temporary files leaked: %+v, %v", entries, err)
	}
}
