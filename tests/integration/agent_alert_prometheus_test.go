package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNativeAgentResourceThresholds(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	cases := []struct {
		condition  string
		usage, age int
		want       string
	}{
		{"cpu", 90, 0, "pending"},
		{"memory", 90, 0, "pending"},
		{"disk", 90, 0, "pending"},
		{"cpu", 80, 0, "inactive"},
		{"memory", 80, 0, "inactive"},
		{"disk", 80, 0, "inactive"},
		{"memory", 90, 90, "inactive"},
		{"disk", 90, -90, "inactive"},
		{"memory", 110, 0, "inactive"},
	}
	for i := range cases {
		if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: agent.InstanceUID{byte(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	threshold := 80.0
	for i, test := range cases {
		uid := agent.InstanceUID{byte(i + 1)}
		emitAgentResourceMetrics(t, url, token, uid, test.usage, test.age)
		if err := alert.NewStore(db).Create(t.Context(), alert.Rule{ID: fmt.Sprintf("agent-rule-%d", i), AgentInstanceUID: &uid, Condition: test.condition, Threshold: &threshold, ForSeconds: 120, Severity: "warning"}); err != nil {
			t.Fatal(err)
		}
	}
	for i, test := range cases {
		t.Log(test.condition, test.usage, test.age)
		waitNativeState(t, url, cookie, fmt.Sprintf("agent-rule-%d", i), test.want, time.Time{})
	}
}

func emitAgentResourceMetrics(t *testing.T, url, token string, uid agent.InstanceUID, usage, age int) {
	t.Helper()
	attributes := func(values map[string]string) []map[string]any {
		result := make([]map[string]any, 0, len(values))
		for key, value := range values {
			result = append(result, map[string]any{"key": key, "value": map[string]string{"stringValue": value}})
		}
		return result
	}
	now := time.Now()
	point := func(value float64, seconds int, labels map[string]string) map[string]any {
		return map[string]any{"timeUnixNano": strconv.FormatInt(now.Add(-time.Duration(seconds)*time.Second).UnixNano(), 10), "startTimeUnixNano": "1", "asDouble": value, "attributes": attributes(labels)}
	}
	gauge := func(name string, value float64) map[string]any {
		return map[string]any{"name": name, "unit": "By", "gauge": map[string]any{"dataPoints": []map[string]any{point(value, age, nil)}}}
	}
	metrics := []map[string]any{gauge("system.memory.limit", 100), gauge("system.linux.memory.available", float64(100-usage))}
	var cpu []map[string]any
	for state, value := range map[string]float64{"idle": float64(100 - usage), "user": float64(usage)} {
		labels := map[string]string{"cpu": "0", "state": state}
		cpu = append(cpu, point(100, age+30, labels), point(100+value, age, labels))
	}
	metrics = append(metrics, map[string]any{"name": "system.cpu.time", "unit": "s", "sum": map[string]any{"aggregationTemporality": 2, "isMonotonic": true, "dataPoints": cpu}})
	var disk []map[string]any
	for state, value := range map[string]float64{"used": float64(usage), "free": float64(100 - usage)} {
		disk = append(disk, point(value, age, map[string]string{"state": state, "mode": "rw", "mountpoint": "/", "device": "test"}))
	}
	metrics = append(metrics, map[string]any{"name": "system.filesystem.usage", "unit": "By", "sum": map[string]any{"aggregationTemporality": 2, "isMonotonic": false, "dataPoints": disk}})
	body, err := json.Marshal(map[string]any{"resourceMetrics": []map[string]any{{"resource": map[string]any{"attributes": attributes(map[string]string{"service.name": "arveld-agent", "service.instance.id": uid.String()})}, "scopeMetrics": []map[string]any{{"metrics": metrics}}}}})
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
		t.Fatalf("send Agent metrics = %d %s %v %v", response.StatusCode, content, err, closeErr)
	}
}
