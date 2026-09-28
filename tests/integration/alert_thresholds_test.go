package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNativeMonitorLatencyThresholds(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	threshold, high, low, negative := 500.0, 750.0, 200.0, -1.0
	tests := []struct {
		name, protocol string
		samples        []alertMetricFixture
		want           string
	}{
		{"HTTP above", "http", []alertMetricFixture{{status: 200, latency: &high}}, "pending"},
		{"TCP above", "tcp", []alertMetricFixture{{status: 1, latency: &high}}, "pending"},
		{"DNS above", "dns", []alertMetricFixture{{status: 1, latency: &high}}, "pending"},
		{"ICMP above", "icmp", []alertMetricFixture{{status: 0, latency: &high}}, "pending"},
		{"equal threshold", "http", []alertMetricFixture{{status: 200, latency: &threshold}}, "inactive"},
		{"below threshold", "tcp", []alertMetricFixture{{status: 1, latency: &low}}, "inactive"},
		{"latest duration wins", "http", []alertMetricFixture{{age: 20, status: 500, latency: &high}, {status: 200, latency: &low}}, "inactive"},
		{"duration must match latest measurement", "http", []alertMetricFixture{{age: 20, status: 500, latency: &high}, {status: 200}}, "inactive"},
		{"stale duration", "tcp", []alertMetricFixture{{age: 90, status: 1, latency: &high}}, "inactive"},
		{"future duration", "tcp", []alertMetricFixture{{age: -90, status: 1, latency: &high}}, "inactive"},
		{"missing duration", "tcp", []alertMetricFixture{{status: 1}}, "inactive"},
		{"invalid duration", "tcp", []alertMetricFixture{{status: 1, latency: &negative}}, "inactive"},
		{"wrong Agent", "tcp", []alertMetricFixture{{status: 1, latency: &high, otherAgent: true}}, "inactive"},
		{"ICMP total loss", "icmp", []alertMetricFixture{{status: 100, latency: &high}}, "inactive"},
	}
	owners := make([]monitor.Monitor, len(tests))
	for i, test := range tests {
		owner := monitor.Monitor{
			ID: fmt.Sprintf("latency-%d", i), Name: test.name, Protocol: test.protocol,
			AgentInstanceUID: uid, Endpoint: "example.com", IntervalSeconds: 30, TimeoutSeconds: 5,
		}
		switch test.protocol {
		case "http":
			owner.Endpoint, owner.Method = "https://example.com", "GET"
		case "tcp":
			owner.Endpoint = "example.com:443"
		case "dns":
			owner.DNSServer, owner.RecordType, owner.Transport = "1.1.1.1:53", "A", "udp"
		case "icmp":
			owner.PingCount = 3
		}
		if err := monitor.NewStore(db).Create(t.Context(), owner); err != nil {
			t.Fatal(err)
		}
		owners[i] = owner
	}
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	for i, test := range tests {
		for _, sample := range test.samples {
			emitAlertMetric(t, url, token, owners[i], sample)
		}
	}
	for _, owner := range owners {
		if err := alert.NewStore(db).Create(t.Context(), alert.Rule{
			ID: owner.ID, MonitorID: owner.ID,
			Condition: "latency", Threshold: &threshold, ForSeconds: 120, Severity: "warning",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, test := range tests {
		t.Log(test.name)
		waitNativeState(t, url, cookie, owners[i].ID, test.want, time.Time{})
	}
	rule, err := alert.NewStore(db).Get(t.Context(), owners[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	rule.ForSeconds = 1
	if err := alert.NewStore(db).Update(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	waitNativeState(t, url, cookie, rule.ID, "firing", time.Now())
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, config.prometheusURL+"/api/v1/alerts", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := doControllerRequest(t, request)
	var payload struct {
		Data struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
			} `json:"alerts"`
		} `json:"data"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	closeErr := response.Body.Close()
	if decodeErr != nil || closeErr != nil {
		t.Fatalf("read native alert: %v, %v", decodeErr, closeErr)
	}
	var found bool
	for _, current := range payload.Data.Alerts {
		if current.Labels["arveld_rule_id"] == rule.ID {
			found = true
			if current.Annotations["resource"] != owners[0].Name || current.Annotations["summary"] != "Slow response" || current.Annotations["description"] != "Response time is 750 ms (threshold: 500 ms). Alert delay: 1s." {
				t.Fatalf("latency notification lacks readable measurements: %+v", current.Annotations)
			}
		}
	}
	if !found {
		t.Fatal("missing native latency alert")
	}
	emitAlertMetric(t, url, token, owners[0], alertMetricFixture{status: 200, latency: &low})
	waitNativeState(t, url, cookie, rule.ID, "inactive", time.Now())
}
