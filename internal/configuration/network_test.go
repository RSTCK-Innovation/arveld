package configuration_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
)

func TestDNSAndICMPMonitorComposition(t *testing.T) {
	specification := configuration.NewSpecification(agent.InstanceUID{1})
	specification.DNSMonitors = []configuration.DNSMonitor{{ID: "dns", Endpoint: "example.com.", DNSServer: "[::1]:53", RecordType: "AAAA", Transport: "tcp", IntervalSeconds: 60, TimeoutSeconds: 5}}
	specification.ICMPMonitors = []configuration.ICMPMonitor{{ID: "icmp", Endpoint: "127.0.0.1", PingCount: 3, IntervalSeconds: 30, TimeoutSeconds: 5}}
	encoded, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var restored configuration.Specification
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, specification) {
		t.Fatal("JSON lost protocol settings")
	}
	content, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	document := decodeCollectorDocument(t, content)
	want := decodeCollectorDocument(t, []byte(`receivers:
  dns_check/dns:
    collection_interval: 60s
    dns_servers:
      - endpoint: "[::1]:53"
        network: tcp
        timeout: 5s
    hostnames:
      - name: example.com.
        record_type: AAAA
  icmpcheckreceiver/icmp:
    collection_interval: 30s
    targets:
      - host: 127.0.0.1
        ping_count: 3
        ping_interval: 1s
        ping_timeout: 5s
processors:
  resource/monitor_dns:
    attributes:
      - {key: arveld.monitor.id, value: dns, action: upsert}
  resource/monitor_icmp:
    attributes:
      - {key: arveld.monitor.id, value: icmp, action: upsert}
service:
  pipelines:
    metrics/monitor_dns:
      receivers: [dns_check/dns]
      processors: [resource/agent, resource/monitor_dns]
      exporters: [otlphttp/arveld]
    metrics/monitor_icmp:
      receivers: [icmpcheckreceiver/icmp]
      processors: [resource/agent, resource/monitor_icmp]
      exporters: [otlphttp/arveld]
`))
	for _, section := range []string{"receivers", "processors"} {
		actual := documentMap(t, document[section])
		for name, expected := range documentMap(t, want[section]) {
			if !reflect.DeepEqual(actual[name], expected) {
				t.Fatalf("%s.%s = %#v, want %#v", section, name, actual[name], expected)
			}
			delete(actual, name)
		}
	}
	pipelines := documentMap(t, documentMap(t, document["service"])["pipelines"])
	for name, expected := range documentMap(t, documentMap(t, want["service"])["pipelines"]) {
		if !reflect.DeepEqual(pipelines[name], expected) {
			t.Fatalf("pipeline %s = %#v", name, pipelines[name])
		}
		delete(pipelines, name)
	}
	base, err := configuration.Compile(configuration.NewSpecification(agent.InstanceUID{1}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document, decodeCollectorDocument(t, base)) {
		t.Fatal("network Monitors changed the base")
	}
	again, err := configuration.Compile(restored)
	if err != nil || !bytes.Equal(content, again) {
		t.Fatalf("nondeterministic compilation: %v", err)
	}
	restored.TCPMonitors = []configuration.TCPMonitor{{ID: "dns", Endpoint: "localhost:443", IntervalSeconds: 30, TimeoutSeconds: 5}}
	if content, err := configuration.Compile(restored); err == nil || len(content) != 0 || !strings.Contains(err.Error(), "duplicate Monitor ID") {
		t.Fatalf("protocol collision: %v", err)
	}
}
