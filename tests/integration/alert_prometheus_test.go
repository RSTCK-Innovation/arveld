package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

// The test owns the pinned engine process, its metrics, rules and storage.
func TestControllerEvaluatesNativeAlertRulesWithPrometheus(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, protocol   string
		interval         int
		assertions       int
		samples          []alertMetricFixture
		want, wantNoData string
	}{
		{"beyond maximum freshness", "tcp", 3600, 0, []alertMetricFixture{{age: 7270, status: 0}}, "inactive", "pending"},
		{"near maximum freshness", "tcp", 3600, 0, []alertMetricFixture{{age: 7230, status: 0}}, "pending", "inactive"},
		{"slow HTTP failure replaces old success", "http", 3600, 0, []alertMetricFixture{
			{age: 900, status: 200}, {age: 600, status: 500},
		}, "pending", "inactive"},
		{"slow DNS recovery", "dns", 3600, 0, []alertMetricFixture{
			{age: 900, status: 0}, {age: 600, status: 1},
		}, "inactive", "inactive"},
		{"HTTP failure replaces old success", "http", 30, 0, []alertMetricFixture{
			{age: 20, status: 200}, {age: 2, status: 500},
		}, "pending", "inactive"},
		{"HTTP recovery", "http", 30, 0, []alertMetricFixture{
			{age: 20, status: 500}, {age: 2, status: 200},
		}, "inactive", "inactive"},
		{"HTTP missing assertions", "http", 30, 2, []alertMetricFixture{{age: 2, status: 200}}, "inactive", "pending"},
		{"HTTP partial assertions", "http", 30, 2, []alertMetricFixture{{age: 2, status: 200, passed: 1}}, "inactive", "pending"},
		{"HTTP repeated assertion type", "http", 30, 2, []alertMetricFixture{{age: 2, status: 200, passed: 2}}, "inactive", "inactive"},
		{"HTTP explicit assertion failure", "http", 30, 2, []alertMetricFixture{{age: 2, status: 200, failed: 1}}, "pending", "inactive"},
		{"HTTP sparse assertion recovery", "http", 30, 2, []alertMetricFixture{
			{age: 20, status: 200, failed: 1}, {age: 2, status: 200, passed: 2},
		}, "inactive", "inactive"},
		{"HTTP error without assertions", "http", 30, 2, []alertMetricFixture{{age: 2, status: 500}}, "pending", "inactive"},
		{"TCP failure", "tcp", 30, 0, []alertMetricFixture{{age: 2, status: 0}}, "pending", "inactive"},
		{"TCP recovery", "tcp", 30, 0, []alertMetricFixture{{age: 20, status: 0}, {age: 2, status: 1}}, "inactive", "inactive"},
		{"stale failure", "tcp", 30, 0, []alertMetricFixture{{age: 65, status: 0}}, "inactive", "pending"},
		{"fresh slow Monitor", "tcp", 3600, 0, []alertMetricFixture{{age: 600, status: 0}}, "pending", "inactive"},
		{"no measurements", "tcp", 30, 0, nil, "inactive", "pending"},
		{"old Agent measurement", "tcp", 30, 0, []alertMetricFixture{{age: 2, status: 0, otherAgent: true}}, "inactive", "pending"},
		{"other Monitor measurement", "tcp", 30, 0, []alertMetricFixture{{age: 2, status: 0, otherMonitor: true}}, "inactive", "pending"},
		{"future measurement", "tcp", 30, 0, []alertMetricFixture{{age: -60, status: 0}}, "inactive", "pending"},
		{"nonbinary measurement", "tcp", 30, 0, []alertMetricFixture{{age: 2, status: 0.5}}, "inactive", "pending"},
		{"DNS changed answer failure", "dns", 30, 0, []alertMetricFixture{{age: 20, status: 1}, {age: 2, status: 0}}, "pending", "inactive"},
		{"DNS recovery", "dns", 30, 0, []alertMetricFixture{{age: 20, status: 0}, {age: 2, status: 1}}, "inactive", "inactive"},
		{"ICMP packet loss", "icmp", 30, 0, []alertMetricFixture{{age: 2, status: 25}}, "pending", "inactive"},
		{"ICMP zero loss", "icmp", 30, 0, []alertMetricFixture{{age: 2, status: 0}}, "inactive", "inactive"},
		{"ICMP invalid loss", "icmp", 30, 0, []alertMetricFixture{{age: 2, status: 101}}, "inactive", "pending"},
	}
	owners := make([]monitor.Monitor, len(cases))
	for index, test := range cases {
		owner := monitor.Monitor{
			ID: rand.Text(), Name: test.name, Protocol: test.protocol, AgentInstanceUID: uid,
			Endpoint: "example.com", IntervalSeconds: test.interval, TimeoutSeconds: 5,
		}
		if test.interval == 3600 {
			owner.TimeoutSeconds = 60
		}
		switch test.protocol {
		case "http":
			owner.Endpoint, owner.Method = "https://example.com", "GET"
			for range test.assertions {
				owner.Validations = append(owner.Validations, monitor.Validation{Type: "contains", Value: "ready"})
			}
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
		owners[index] = owner
	}
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	// Seed historical samples before rules emit current ALERTS samples, which
	// would advance the TSDB ingestion boundary past the oldest fixtures.
	for index, test := range cases {
		for _, sample := range test.samples {
			emitAlertMetric(t, url, token, owners[index], sample)
		}
	}
	for _, owner := range owners {
		for _, rule := range []alert.Rule{
			{ID: owner.ID, MonitorID: owner.ID, Condition: "failed", ForSeconds: 120, Severity: "critical"},
			{ID: owner.ID + "-no-data", MonitorID: owner.ID, Condition: "no_data", ForSeconds: 120, Severity: "warning"},
		} {
			if err := alert.NewStore(db).Create(t.Context(), rule); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ingestedAt := time.Now()
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			waitNativeState(t, url, cookie, owners[index].ID, test.want, ingestedAt)
			waitNativeState(t, url, cookie, owners[index].ID+"-no-data", test.wantNoData, ingestedAt)
		})
	}
}

type alertMetricFixture struct {
	age                      int
	status                   float64
	passed, failed           int
	otherAgent, otherMonitor bool
	latency                  *float64
}

func emitAlertMetric(t *testing.T, url, token string, owner monitor.Monitor, sample alertMetricFixture) {
	t.Helper()
	attributes := func(values map[string]string) []map[string]any {
		result := make([]map[string]any, 0, len(values))
		for key, value := range values {
			result = append(result, map[string]any{"key": key, "value": map[string]string{"stringValue": value}})
		}
		return result
	}
	timestamp := strconv.FormatInt(time.Now().Add(-time.Duration(sample.age)*time.Second).UnixNano(), 10)
	metric := func(name, unit string, value float64, labels map[string]string, sum bool) map[string]any {
		point := map[string]any{"timeUnixNano": timestamp, "startTimeUnixNano": "1", "asDouble": value, "attributes": attributes(labels)}
		data := map[string]any{"dataPoints": []map[string]any{point}}
		kind := "gauge"
		if sum {
			kind = "sum"
			data["aggregationTemporality"], data["isMonotonic"] = 2, false
		}
		return map[string]any{"name": name, "unit": unit, kind: data}
	}
	var metrics []map[string]any
	switch owner.Protocol {
	case "http":
		for _, class := range []string{"1xx", "2xx", "3xx", "4xx", "5xx"} {
			labels := map[string]string{"http.status_class": class}
			value := 0.0
			if class == fmt.Sprintf("%dxx", int(sample.status)/100) {
				value = 1
				labels["http.status_code"] = strconv.Itoa(int(sample.status))
			}
			metrics = append(metrics, metric("httpcheck.status", "1", value, labels, true))
		}
	case "tcp":
		metrics = append(metrics, metric("tcpcheck.status", "1", sample.status, nil, false))
	case "dns":
		labels := map[string]string{"dns.rcode": "3"}
		if sample.status == 1 {
			labels = map[string]string{"dns.rcode": "0", "dns.resolved.ip": "192.0.2.1"}
		}
		metrics = append(metrics, metric("dnscheck.status", "1", sample.status, labels, true))
	case "icmp":
		metrics = append(metrics, metric("ping.loss.ratio", "%", sample.status, nil, false))
	}
	if sample.latency != nil {
		name := map[string]string{"http": "httpcheck.duration", "tcp": "tcpcheck.duration", "dns": "dnscheck.duration", "icmp": "ping.rtt.avg"}[owner.Protocol]
		metrics = append(metrics, metric(name, "ms", *sample.latency, nil, false))
	}
	for outcome, count := range map[string]int{"passed": sample.passed, "failed": sample.failed} {
		if count > 0 {
			metrics = append(metrics, metric("httpcheck.validation."+outcome, "{validation}", float64(count), map[string]string{"validation.type": "contains"}, true))
		}
	}
	uid := owner.AgentInstanceUID
	if sample.otherAgent {
		uid = agent.InstanceUID{2}
	}
	id := owner.ID
	if sample.otherMonitor {
		id += "-other"
	}
	body, err := json.Marshal(map[string]any{"resourceMetrics": []map[string]any{{
		"resource": map[string]any{"attributes": attributes(map[string]string{
			"service.name": "arveld-agent", "service.instance.id": uid.String(), "arveld.monitor.id": id,
		})},
		"scopeMetrics": []map[string]any{{"metrics": metrics}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/otlp/v1/metrics", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := doControllerRequest(t, request)
	content, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("ingest alert fixture = %d %s, %v, %v", response.StatusCode, content, err, closeErr)
	}
}

func TestControllerPublishesNativeAlertLifecycle(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	url, stop := startController(t, config)
	cookie := loginController(t, url)
	owner := createControllerHTTPMonitor(t, url, cookie, uid)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		url+"/api/v1/monitors/"+owner.ID+"/alert-rules",
		strings.NewReader(`{"condition":"failed","for_seconds":10,"severity":"critical"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	response := doControllerRequest(t, req)
	var created struct {
		ID string `json:"id"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&created)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || created.ID == "" {
		t.Fatalf("create native rule = %d, %v, %v", response.StatusCode, decodeErr, closeErr)
	}
	// Read the same owner from SQLite to build a realistic OTLP resource.
	db = testutil.OpenDatabase(t, config.DatabasePath)
	definition, err := monitor.NewStore(db).Get(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	emitAlertMetric(t, url, token, definition, alertMetricFixture{status: 500})
	waitNativeState(t, url, cookie, created.ID, "pending", time.Now())
	waitNativeState(t, url, cookie, created.ID, "firing", time.Now())
	stop()
	url, _ = startController(t, config)
	waitNativeState(t, url, cookie, created.ID, "firing", time.Now())
	emitAlertMetric(t, url, token, definition, alertMetricFixture{status: 200})
	waitNativeState(t, url, cookie, created.ID, "inactive", time.Now())
	if _, err := monitor.NewStore(db).Delete(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	client, err := prometheus.NewClient(config.prometheusURL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		rules, err := client.AlertRules(t.Context(), "arveld-monitor-alerts")
		if err == nil && len(rules) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("deleting the Monitor did not remove its loaded native rule")
}

func waitNativeState(t *testing.T, url string, cookie *http.Cookie, id, expected string, evaluatedAfter time.Time) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var state alert.State
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			url+"/api/v1/alert-rules/"+id+"/state", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		decodeErr := json.NewDecoder(response.Body).Decode(&state)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil {
			t.Fatalf("native state = %d, %v, %v", response.StatusCode, decodeErr, closeErr)
		}
		if state.SyncStatus == "applied" && state.Health == "ok" && state.State == expected && state.LastEvaluation.After(evaluatedAfter) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("native state = %+v, want applied/%s with an evaluation after %s", state, expected, evaluatedAfter)
}

func startRulePrometheus(t *testing.T, directory string, alertmanagerURL ...string) string {
	t.Helper()
	binary := os.Getenv("ARVELD_TEST_PROMETHEUS_BINARY")
	if binary == "" {
		t.Skip("set ARVELD_TEST_PROMETHEUS_BINARY to the pinned Prometheus executable")
	}
	configPath := filepath.Join(directory, "prometheus-test.yml")
	content := fmt.Sprintf("rule_files:\n  - %q\nscrape_configs: []\notlp:\n  promote_resource_attributes: [arveld.monitor.id]\n",
		filepath.Join(directory, "config", "alert-rules", "*.yml"))
	if len(alertmanagerURL) > 0 {
		content += fmt.Sprintf("alerting:\n  alertmanagers:\n    - static_configs:\n        - targets: [%q]\n", strings.TrimPrefix(alertmanagerURL[0], "http://"))
	}
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "prometheus-test.log")
	log, err := os.Create(logPath) //nolint:gosec // Generated path in the test directory.
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	command := exec.CommandContext(ctx, binary, //nolint:gosec // Explicit test executable, generated arguments, no shell.
		"--config.file="+configPath, "--storage.tsdb.path="+filepath.Join(directory, "prometheus-test-data"),
		"--web.listen-address=127.0.0.1:0", "--web.enable-otlp-receiver", "--web.enable-lifecycle",
		"--query.lookback-delta=2h1m", "--log.format=json")
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		cancel()
		if closeErr := log.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait() //nolint:errcheck // CommandContext kills the isolated engine during cleanup.
		if err := log.Close(); err != nil {
			t.Error(err)
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := os.ReadFile(logPath) //nolint:gosec // Generated path in the test directory.
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(logs), "\n") {
			var entry struct {
				Message string `json:"msg"`
				Address string `json:"address"`
			}
			if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Message != "Listening on" || entry.Address == "" {
				continue
			}
			url := "http://" + entry.Address
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url+"/-/ready", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(req)
			if err == nil {
				if err := response.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode == http.StatusOK {
					return url
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	logs, err := os.ReadFile(logPath) //nolint:gosec // Generated path in the test directory.
	t.Fatalf("Prometheus did not start: %s (%v)", logs, err)
	return ""
}
