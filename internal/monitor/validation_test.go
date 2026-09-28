package monitor_test

import (
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

func TestMonitorSettingsRejectInvalidOrMixedProtocols(t *testing.T) {
	values := []monitor.Monitor{
		{Name: "  Database  ", Protocol: "tcp", Endpoint: "[::1]:5432", IntervalSeconds: 30, TimeoutSeconds: 5},
		{Name: "  Router  ", Protocol: "icmp", Endpoint: "127.0.0.1", PingCount: 3, IntervalSeconds: 30, TimeoutSeconds: 5},
		{Name: "  Resolution  ", Protocol: "dns", Endpoint: "_dmarc.example.com.", DNSServer: "localhost:53", RecordType: "TXT", Transport: "tcp", IntervalSeconds: 30, TimeoutSeconds: 5},
		{Name: "  Homepage  ", Protocol: "http", Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5},
	}
	for _, value := range values {
		got, err := monitor.Validate(value)
		if err != nil || got.Name == value.Name {
			t.Fatalf("valid settings: %+v, %v", got, err)
		}
	}
	for _, test := range []struct {
		name   string
		index  int
		change func(*monitor.Monitor)
	}{
		{"TCP with HTTP method", 0, func(m *monitor.Monitor) { m.Method = "GET" }},
		{"HTTP with DNS fields", 3, func(m *monitor.Monitor) { m.DNSServer = "1.1.1.1:53" }},
		{"HTTP with ping count", 3, func(m *monitor.Monitor) { m.PingCount = 3 }},
		{"HTTP method", 3, func(m *monitor.Monitor) { m.Method = "TRACE" }},
		{"unknown protocol", 0, func(m *monitor.Monitor) { m.Protocol = "udp" }},
		{"short name", 0, func(m *monitor.Monitor) { m.Name = "a" }},
		{"invalid TCP port", 0, func(m *monitor.Monitor) { m.Endpoint = "example.com:65536" }},
		{"signed TCP port", 0, func(m *monitor.Monitor) { m.Endpoint = "example.com:+443" }},
		{"TCP zone variable", 0, func(m *monitor.Monitor) { m.Endpoint = "[fe80::1%${env:ZONE}]:443" }},
		{"TCP with ping count", 0, func(m *monitor.Monitor) { m.PingCount = 3 }},
		{"TCP with DNS fields", 0, func(m *monitor.Monitor) { m.DNSServer = "1.1.1.1:53" }},
		{"ICMP URL", 1, func(m *monitor.Monitor) { m.Endpoint = "https://example.com" }},
		{"ICMP port", 1, func(m *monitor.Monitor) { m.Endpoint = "example.com:443" }},
		{"ICMP variable", 1, func(m *monitor.Monitor) { m.Endpoint = "${env:HOST}" }},
		{"ICMP zone variable", 1, func(m *monitor.Monitor) { m.Endpoint = "fe80::1%${env:ZONE}" }},
		{"ICMP zero count", 1, func(m *monitor.Monitor) { m.PingCount = 0 }},
		{"ICMP excessive count", 1, func(m *monitor.Monitor) { m.PingCount = 11 }},
		{"ICMP insufficient timeout", 1, func(m *monitor.Monitor) { m.TimeoutSeconds = 2 }},
		{"DNS URL", 2, func(m *monitor.Monitor) { m.Endpoint = "https://example.com" }},
		{"DNS blank label", 2, func(m *monitor.Monitor) { m.Endpoint = "example..com" }},
		{"DNS malformed server", 2, func(m *monitor.Monitor) { m.DNSServer = "resolver:0" }},
		{"DNS server zone variable", 2, func(m *monitor.Monitor) { m.DNSServer = "[fe80::1%${env:ZONE}]:53" }},
		{"DNS unknown record", 2, func(m *monitor.Monitor) { m.RecordType = "FAKE" }},
		{"DNS unknown transport", 2, func(m *monitor.Monitor) { m.Transport = "https" }},
		{"DNS with ping count", 2, func(m *monitor.Monitor) { m.PingCount = 3 }},
		{"short interval", 2, func(m *monitor.Monitor) { m.IntervalSeconds = 9 }},
		{"timeout exceeds interval", 2, func(m *monitor.Monitor) { m.TimeoutSeconds = 31 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := values[test.index]
			test.change(&value)
			if _, err := monitor.Validate(value); err == nil {
				t.Fatal("invalid settings were accepted")
			}
		})
	}
}
