package integration

import (
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
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestIncidentObservationsRejectUnsafeEngineEvidence(t *testing.T) {
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	threshold := 80.0
	store := alert.NewStore(db)
	if err := store.Create(t.Context(), alert.Rule{ID: "cpu", AgentInstanceUID: &uid, Condition: "cpu", Threshold: &threshold, ForSeconds: 60, Severity: "warning"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var native prometheus.AlertRule
	status := http.StatusOK
	observed := time.Now().Add(-4 * time.Second).UTC()
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var data any
		switch r.URL.Path {
		case "/api/v1/format_query":
			data = r.FormValue("query")
		case "/-/reload":
			// #nosec G304 -- read only the generated configuration beneath t.TempDir.
			content, err := os.ReadFile(filepath.Join(directory, "config", "alert-rules", "monitors.yml"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var file struct {
				Groups []struct {
					Rules []struct {
						Alert  string            `yaml:"alert"`
						Expr   string            `yaml:"expr"`
						Labels map[string]string `yaml:"labels"`
					} `yaml:"rules"`
				} `yaml:"groups"`
			}
			if err := yaml.Unmarshal(content, &file); err != nil || len(file.Groups) != 1 || len(file.Groups[0].Rules) != 1 {
				t.Errorf("invalid published rule: %s %v", content, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			rule := file.Groups[0].Rules[0]
			native = prometheus.AlertRule{Name: rule.Alert, Query: rule.Expr, Duration: 60, Labels: rule.Labels, Type: "alerting", State: "firing", Health: "ok", LastEvaluation: observed}
			return
		case "/api/v1/rules":
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			data = map[string]any{"groups": []map[string]any{{"name": "arveld-monitor-alerts", "rules": []prometheus.AlertRule{native}}}}
		default:
			t.Errorf("unexpected engine path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(engine.Close)
	client, err := prometheus.NewClient(engine.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := alert.NewPublisher(store, client, directory).Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check := func(count int, want string) {
		t.Helper()
		page, err := store.Incidents(t.Context(), alert.IncidentQuery{Limit: 10})
		if err != nil || page.Total != count || len(page.Incidents) != count || page.Incidents[0].Status != want {
			t.Fatalf("unsafe observation changed history: %+v %v", page, err)
		}
	}
	check(1, "open")
	for _, test := range []struct {
		name, state, health string
		evaluation          time.Time
		status              int
	}{
		{"unavailable", "inactive", "ok", observed.Add(time.Second), 503},
		{"unhealthy", "inactive", "err", observed.Add(time.Second), 200},
		{"pending after restart", "pending", "ok", observed.Add(time.Second), 200},
		{"stale", "inactive", "ok", observed.Add(-time.Minute), 200},
		{"future", "inactive", "ok", observed.Add(time.Hour), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			mu.Lock()
			native.State, native.Health, native.LastEvaluation, status = test.state, test.health, test.evaluation, test.status
			mu.Unlock()
			err := alert.ObserveIncidents(t.Context(), store, client)
			if (err != nil) != (test.status != 200) {
				t.Fatalf("observe error = %v", err)
			}
			check(1, "open")
		})
	}
	mu.Lock()
	native.State, native.Health, native.LastEvaluation, status = "inactive", "ok", observed.Add(2*time.Second), 200
	mu.Unlock()
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check(1, "closed")
	// Replaying an earlier firing snapshot must not resurrect the closed episode.
	mu.Lock()
	native.State, native.LastEvaluation = "firing", observed
	mu.Unlock()
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check(1, "closed")
	// Changing a desired rule must fence observations from the previous loaded revision.
	mu.Lock()
	native.State, native.LastEvaluation = "firing", observed.Add(3*time.Second)
	mu.Unlock()
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check(2, "open")
	if err := store.Update(t.Context(), alert.Rule{ID: "cpu", AgentInstanceUID: &uid, Condition: "cpu", Threshold: &threshold, ForSeconds: 60, Severity: "critical"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	native.State, native.LastEvaluation = "inactive", observed.Add(4*time.Second)
	mu.Unlock()
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check(2, "open")
	if err := alert.NewPublisher(store, client, directory).Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	native.State, native.LastEvaluation = "pending", observed.Add(4*time.Second)
	mu.Unlock()
	if err := alert.ObserveIncidents(t.Context(), store, client); err != nil {
		t.Fatal(err)
	}
	check(2, "closed")
	page, err := store.Incidents(t.Context(), alert.IncidentQuery{Limit: 10})
	if err != nil || page.Incidents[0].CloseReason != "rule_changed" || page.Incidents[0].Severity != "warning" {
		t.Fatalf("changed rule lost its historical definition: %+v %v", page, err)
	}
}
