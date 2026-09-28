package configuration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
)

func TestHTTPMonitorComposition(t *testing.T) {
	const saved = `{
		"schema_version": 1,
		"base_version": 1,
		"instance_uid": "12000000-0000-0000-0000-0000000000ab",
		"http_monitors": [
			{"id":"homepage","endpoint":"https://example.com/health?literal=${env:TOKEN}&price=$$","method":"GET","interval_seconds":60,"timeout_seconds":5},
			{"id":"status","endpoint":"http://example.net/status","method":"HEAD","interval_seconds":15,"timeout_seconds":3}
		]
	}`
	var specification configuration.Specification
	if err := json.Unmarshal([]byte(saved), &specification); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var restored configuration.Specification
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARVELD_URL", "http://controller-only.invalid")
	t.Setenv("ARVELD_AGENT_TOKEN", "controller-only-secret")
	t.Setenv("TOKEN", "controller-only-target")
	content, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}

	got := decodeCollectorDocument(t, content)
	baseContent, err := os.ReadFile("testdata/base-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := decodeCollectorDocument(t, baseContent)
	contributions, err := os.ReadFile("testdata/http-monitors.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := decodeCollectorDocument(t, contributions)
	for _, section := range []string{"receivers", "processors"} {
		actual := documentMap(t, got[section])
		for name, expected := range documentMap(t, want[section]) {
			if !reflect.DeepEqual(actual[name], expected) {
				t.Errorf("%s.%s = %#v, want %#v", section, name, actual[name], expected)
			}
			delete(actual, name)
		}
	}
	pipelines := documentMap(t, documentMap(t, got["service"])["pipelines"])
	for name, expected := range documentMap(t, documentMap(t, want["service"])["pipelines"]) {
		if !reflect.DeepEqual(pipelines[name], expected) {
			t.Errorf("pipeline %s = %#v, want %#v", name, pipelines[name], expected)
		}
		delete(pipelines, name)
	}
	if !reflect.DeepEqual(got, base) {
		t.Fatal("Monitor contributions modified the base configuration")
	}

	again, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, again) {
		t.Fatal("unchanged specification produced different YAML bytes")
	}
	after, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, after) {
		t.Fatal("compilation mutated its specification")
	}
	slices.Reverse(restored.HTTPMonitors)
	reordered, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, reordered) {
		t.Fatal("Monitor order changed the compiled YAML")
	}
	restored.HTTPMonitors = restored.HTTPMonitors[:1]
	remaining, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	remainingDocument := decodeCollectorDocument(t, remaining)
	if _, found := documentMap(t, remainingDocument["receivers"])["http_check/homepage"]; found {
		t.Fatal("removed Monitor still has a receiver")
	}
	if _, found := documentMap(t, remainingDocument["processors"])["resource/monitor_homepage"]; found {
		t.Fatal("removed Monitor still has a processor")
	}
	remainingPipelines := documentMap(t, documentMap(t, remainingDocument["service"])["pipelines"])
	if _, found := remainingPipelines["metrics/monitor_homepage"]; found {
		t.Fatal("removed Monitor still has a pipeline")
	}
	if !reflect.DeepEqual(remainingPipelines["metrics/monitor_status"], documentMap(t, documentMap(t, want["service"])["pipelines"])["metrics/monitor_status"]) {
		t.Fatal("removing one Monitor changed the remaining Monitor's pipeline")
	}
	restored.HTTPMonitors = nil
	baseOnly, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseOnly, baseContent) {
		t.Fatal("removing every Monitor did not restore the original base bytes")
	}
}

func TestHTTPMonitorRejectsInvalidSpecification(t *testing.T) {
	valid := configuration.HTTPMonitor{
		ID: "homepage", Endpoint: "https://example.com/health", Method: "GET",
		IntervalSeconds: 60, TimeoutSeconds: 5,
	}
	cases := []struct {
		name      string
		change    func(*configuration.HTTPMonitor)
		wantError string
	}{
		{name: "empty ID", change: func(m *configuration.HTTPMonitor) { m.ID = "" }, wantError: "component ID"},
		{name: "trimmed ID", change: func(m *configuration.HTTPMonitor) { m.ID = " homepage" }, wantError: "whitespace"},
		{name: "confmap delimiter", change: func(m *configuration.HTTPMonitor) { m.ID = "a::b" }, wantError: "monitor ID"},
		{name: "component delimiter", change: func(m *configuration.HTTPMonitor) { m.ID = "a/b" }, wantError: "monitor ID"},
		{name: "empty URL", change: func(m *configuration.HTTPMonitor) { m.Endpoint = "" }, wantError: "endpoint"},
		{name: "scheme", change: func(m *configuration.HTTPMonitor) { m.Endpoint = "ftp://example.com" }, wantError: "endpoint"},
		{name: "missing host", change: func(m *configuration.HTTPMonitor) { m.Endpoint = "https:///health" }, wantError: "endpoint"},
		{name: "credentials", change: func(m *configuration.HTTPMonitor) { m.Endpoint = "https://user:secret@example.com" }, wantError: "endpoint"},
		{name: "invalid port", change: func(m *configuration.HTTPMonitor) { m.Endpoint = "http://example.com:65536" }, wantError: "port"},
		{name: "method", change: func(m *configuration.HTTPMonitor) { m.Method = "TRACE" }, wantError: "method"},
		{name: "short interval", change: func(m *configuration.HTTPMonitor) { m.IntervalSeconds = 9 }, wantError: "interval"},
		{name: "long interval", change: func(m *configuration.HTTPMonitor) { m.IntervalSeconds = 3601 }, wantError: "interval"},
		{name: "zero timeout", change: func(m *configuration.HTTPMonitor) { m.TimeoutSeconds = 0 }, wantError: "timeout"},
		{name: "long timeout", change: func(m *configuration.HTTPMonitor) { m.TimeoutSeconds = 61 }, wantError: "timeout"},
		{name: "timeout exceeds interval", change: func(m *configuration.HTTPMonitor) { m.IntervalSeconds = 10; m.TimeoutSeconds = 11 }, wantError: "timeout"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			monitor := valid
			test.change(&monitor)
			specification := configuration.NewSpecification(agent.InstanceUID{1})
			specification.HTTPMonitors = []configuration.HTTPMonitor{monitor}
			content, err := configuration.Compile(specification)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Compile() error = %v, want %q", err, test.wantError)
			}
			if len(content) != 0 {
				t.Fatal("invalid Monitor produced a configuration")
			}
		})
	}
	t.Run("duplicate ID", func(t *testing.T) {
		specification := configuration.NewSpecification(agent.InstanceUID{1})
		specification.HTTPMonitors = []configuration.HTTPMonitor{valid, valid}
		content, err := configuration.Compile(specification)
		if err == nil || !strings.Contains(err.Error(), "duplicate Monitor") || len(content) != 0 {
			t.Fatalf("duplicate Monitor produced %d bytes with error %v", len(content), err)
		}
	})
	t.Run("unsupported schema", func(t *testing.T) {
		specification := configuration.NewSpecification(agent.InstanceUID{1})
		specification.SchemaVersion = 2
		specification.HTTPMonitors = []configuration.HTTPMonitor{valid}
		content, err := configuration.Compile(specification)
		if err == nil || !strings.Contains(err.Error(), "schema version: 2") || len(content) != 0 {
			t.Fatalf("unsupported schema produced %d bytes with error %v", len(content), err)
		}
	})
}

func decodeCollectorDocument(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func documentMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected a YAML mapping, got %T", value)
	}
	return result
}

func TestHTTPOptionsCompileToSupportedCollectorSettings(t *testing.T) {
	const input = `{"schema_version":1,"base_version":1,"instance_uid":"01000000-0000-0000-0000-000000000000","http_monitors":[{"id":"api","endpoint":"https://example.com","method":"PUT","interval_seconds":30,"timeout_seconds":5,"body":"{\"value\":\"${env:TOKEN}\"}","headers":{"authorization":"Bearer $literal","content-type":"application/custom"},"skip_tls_verify":true,"validations":[{"type":"contains","value":"$ok"},{"type":"not_contains","value":"error"},{"type":"regex","value":"^ready$"},{"type":"json_path","path":"ready","equals":"true"},{"type":"min_size","size":1},{"type":"max_size","size":1024}]}]}`
	var spec configuration.Specification
	if err := json.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatal(err)
	}
	content, err := configuration.Compile(spec)
	if err != nil {
		t.Fatal(err)
	}
	got := documentMap(t, decodeCollectorDocument(t, content)["receivers"])["http_check/api"]
	want := decodeCollectorDocument(t, []byte(`collection_interval: 30s
metrics:
  httpcheck.dns.lookup.duration: {enabled: true}
  httpcheck.client.connection.duration: {enabled: true}
  httpcheck.tls.handshake.duration: {enabled: true}
  httpcheck.client.request.duration: {enabled: true}
  httpcheck.response.duration: {enabled: true}
  httpcheck.response.size: {enabled: true}
  httpcheck.tls.cert_remaining: {enabled: true}
  httpcheck.validation.passed: {enabled: true}
  httpcheck.validation.failed: {enabled: true}
targets:
  - endpoint: https://example.com
    method: PUT
    timeout: 5s
    auto_content_type: true
    body: '{"value":"$${env:TOKEN}"}'
    headers:
      Authorization: Bearer $$literal
      Content-Type: application/custom
    tls: {insecure_skip_verify: true}
    validations:
      - {contains: $$ok}
      - {not_contains: error}
      - {regex: '^ready$$'}
      - {json_path: ready, equals: 'true'}
      - {min_size: 1}
      - {max_size: 1024}
`))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled receiver = %#v, want %#v", got, want)
	}
}
