package configuration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
)

func TestTCPMonitorComposition(t *testing.T) {
	const saved = `{
		"schema_version": 1,
		"base_version": 1,
		"instance_uid": "12000000-0000-0000-0000-0000000000ab",
		"http_monitors": [
			{"id":"homepage","endpoint":"https://example.com","method":"GET","interval_seconds":60,"timeout_seconds":5}
		],
		"tcp_monitors": [
			{"id":"database","endpoint":"db.example.com:5432","interval_seconds":60,"timeout_seconds":5},
			{"id":"ipv6","endpoint":"[::1]:443","interval_seconds":15,"timeout_seconds":3}
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
	content, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeCollectorDocument(t, content)
	contributions, err := os.ReadFile("testdata/tcp-monitors.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := decodeCollectorDocument(t, contributions)
	for _, section := range []string{"receivers", "processors"} {
		actual := documentMap(t, got[section])
		for name, expected := range documentMap(t, want[section]) {
			if !reflect.DeepEqual(actual[name], expected) {
				t.Fatalf("%s.%s = %#v, want %#v", section, name, actual[name], expected)
			}
			delete(actual, name)
		}
	}
	pipelines := documentMap(t, documentMap(t, got["service"])["pipelines"])
	for name, expected := range documentMap(t, documentMap(t, want["service"])["pipelines"]) {
		if !reflect.DeepEqual(pipelines[name], expected) {
			t.Fatalf("pipeline %s = %#v, want %#v", name, pipelines[name], expected)
		}
		delete(pipelines, name)
	}

	// Removing TCP contributions must leave the base and HTTP document intact.
	var httpOnly configuration.Specification
	if err := json.Unmarshal([]byte(`{
		"schema_version": 1,
		"base_version": 1,
		"instance_uid": "12000000-0000-0000-0000-0000000000ab",
		"http_monitors": [
			{"id":"homepage","endpoint":"https://example.com","method":"GET","interval_seconds":60,"timeout_seconds":5}
		]
	}`), &httpOnly); err != nil {
		t.Fatal(err)
	}
	previous, err := configuration.Compile(httpOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, decodeCollectorDocument(t, previous)) {
		t.Fatal("TCP contributions modified the base or HTTP configuration")
	}
	after, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, after) {
		t.Fatal("compilation mutated its specification")
	}
	slices.Reverse(restored.TCPMonitors)
	reordered, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, reordered) {
		t.Fatal("TCP Monitor order changed the compiled YAML")
	}
	restored.TCPMonitors = nil
	remaining, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remaining, previous) {
		t.Fatal("removing TCP Monitors did not restore the HTTP configuration")
	}

	// A TCP-only specification must also enter composition instead of returning the base.
	specification.HTTPMonitors = nil
	tcpOnly, err := configuration.Compile(specification)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(documentMap(t, decodeCollectorDocument(t, tcpOnly)["receivers"])["tcp_check/database"], documentMap(t, want["receivers"])["tcp_check/database"]) {
		t.Fatal("TCP-only compilation lost its receiver")
	}
}

func TestTCPMonitorRejectsInvalidSpecification(t *testing.T) {
	valid := configuration.TCPMonitor{
		ID: "database", Endpoint: "localhost:5432", IntervalSeconds: 60, TimeoutSeconds: 5,
	}
	for _, test := range []struct {
		name      string
		change    func(*configuration.TCPMonitor)
		wantError string
	}{
		{name: "empty ID", change: func(m *configuration.TCPMonitor) { m.ID = "" }, wantError: "component ID"},
		{name: "trimmed ID", change: func(m *configuration.TCPMonitor) { m.ID = " database" }, wantError: "whitespace"},
		{name: "component delimiter", change: func(m *configuration.TCPMonitor) { m.ID = "a/b" }, wantError: "monitor ID"},
		{name: "configuration delimiter", change: func(m *configuration.TCPMonitor) { m.ID = "a::b" }, wantError: "monitor ID"},
		{name: "ID interpolation", change: func(m *configuration.TCPMonitor) { m.ID = "$TOKEN" }, wantError: "component ID"},
		{name: "empty endpoint", change: func(m *configuration.TCPMonitor) { m.Endpoint = "" }, wantError: "endpoint"},
		{name: "missing host", change: func(m *configuration.TCPMonitor) { m.Endpoint = ":443" }, wantError: "endpoint"},
		{name: "missing port", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example.com" }, wantError: "endpoint"},
		{name: "scheme", change: func(m *configuration.TCPMonitor) { m.Endpoint = "tcp://example.com:443" }, wantError: "endpoint"},
		{name: "credentials", change: func(m *configuration.TCPMonitor) { m.Endpoint = "user:secret@example.com:443" }, wantError: "endpoint"},
		{name: "endpoint interpolation", change: func(m *configuration.TCPMonitor) { m.Endpoint = "${env:HOST}:443" }, wantError: "endpoint"},
		{name: "path", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example.com/path:443" }, wantError: "endpoint"},
		{name: "whitespace", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example .com:443" }, wantError: "endpoint"},
		{name: "control character", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example\x00.com:443" }, wantError: "endpoint"},
		{name: "invalid UTF-8", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example\xff.com:443" }, wantError: "endpoint"},
		{name: "invalid IPv6", change: func(m *configuration.TCPMonitor) { m.Endpoint = "[not::ipv6]:443" }, wantError: "endpoint"},
		{name: "named port", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example.com:https" }, wantError: "port"},
		{name: "zero port", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example.com:0" }, wantError: "port"},
		{name: "large port", change: func(m *configuration.TCPMonitor) { m.Endpoint = "example.com:65536" }, wantError: "port"},
		{name: "short interval", change: func(m *configuration.TCPMonitor) { m.IntervalSeconds = 9 }, wantError: "interval"},
		{name: "long interval", change: func(m *configuration.TCPMonitor) { m.IntervalSeconds = 3601 }, wantError: "interval"},
		{name: "zero timeout", change: func(m *configuration.TCPMonitor) { m.TimeoutSeconds = 0 }, wantError: "timeout"},
		{name: "long timeout", change: func(m *configuration.TCPMonitor) { m.TimeoutSeconds = 61 }, wantError: "timeout"},
		{name: "timeout exceeds interval", change: func(m *configuration.TCPMonitor) { m.IntervalSeconds = 10; m.TimeoutSeconds = 11 }, wantError: "timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.change(&value)
			specification := configuration.NewSpecification(agent.InstanceUID{1})
			specification.TCPMonitors = []configuration.TCPMonitor{value}
			content, err := configuration.Compile(specification)
			if err == nil || !strings.Contains(err.Error(), test.wantError) || len(content) != 0 {
				t.Fatalf("Compile() returned %d bytes and error %v, want no bytes and %q", len(content), err, test.wantError)
			}
		})
	}
	t.Run("duplicate TCP ID", func(t *testing.T) {
		specification := configuration.NewSpecification(agent.InstanceUID{1})
		specification.TCPMonitors = []configuration.TCPMonitor{valid, valid}
		content, err := configuration.Compile(specification)
		if err == nil || !strings.Contains(err.Error(), "duplicate Monitor ID") || len(content) != 0 {
			t.Fatalf("duplicate TCP Monitor returned %d bytes and error %v", len(content), err)
		}
	})
	t.Run("ID shared with HTTP", func(t *testing.T) {
		specification := configuration.NewSpecification(agent.InstanceUID{1})
		specification.HTTPMonitors = []configuration.HTTPMonitor{{
			ID: valid.ID, Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 60, TimeoutSeconds: 5,
		}}
		specification.TCPMonitors = []configuration.TCPMonitor{valid}
		content, err := configuration.Compile(specification)
		if err == nil || !strings.Contains(err.Error(), "duplicate Monitor ID") || len(content) != 0 {
			t.Fatalf("TCP Monitor overwrote HTTP: returned %d bytes and error %v", len(content), err)
		}
	})
}
