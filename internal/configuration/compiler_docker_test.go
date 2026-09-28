//go:build docker

package configuration_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

func TestConfigurationValidatesWithCollector(t *testing.T) {
	for _, test := range []struct {
		name         string
		monitors     []configuration.HTTPMonitor
		tcpMonitors  []configuration.TCPMonitor
		icmpMonitors []configuration.ICMPMonitor
		dnsMonitors  []configuration.DNSMonitor
	}{
		{name: "base"},
		{name: "HTTP options", monitors: []configuration.HTTPMonitor{{
			ID: "api", Endpoint: "https://example.com", Method: "POST", IntervalSeconds: 30, TimeoutSeconds: 5, SkipTLSVerify: true,
			Body: `{"ready":"$literal"}`, Headers: map[string]string{"Authorization": "Bearer $literal"}, Validations: []monitor.Validation{
				{Type: "contains", Value: "ready"}, {Type: "not_contains", Value: "error"}, {Type: "regex", Value: "^.*$"}, {Type: "json_path", Path: "ready", Equals: new("true")}, {Type: "min_size", Size: new(int64(1))}, {Type: "max_size", Size: new(int64(1024))},
			},
		}}},
		{name: "ICMP and DNS Monitors", icmpMonitors: []configuration.ICMPMonitor{
			{ID: "router", Endpoint: "127.0.0.1", PingCount: 3, IntervalSeconds: 30, TimeoutSeconds: 5},
		}, dnsMonitors: []configuration.DNSMonitor{
			{ID: "dns", Endpoint: "example.com", DNSServer: "1.1.1.1:53", RecordType: "A", Transport: "udp", IntervalSeconds: 30, TimeoutSeconds: 5},
		}},
		{name: "HTTP Monitors", monitors: []configuration.HTTPMonitor{
			{ID: "homepage", Endpoint: "https://example.com/health?literal=${env:TOKEN}&price=$$", Method: "GET", IntervalSeconds: 60, TimeoutSeconds: 5},
			{ID: "status", Endpoint: "http://example.net/status", Method: "HEAD", IntervalSeconds: 15, TimeoutSeconds: 3},
		}},
		{name: "TCP Monitors", tcpMonitors: []configuration.TCPMonitor{
			{ID: "database", Endpoint: "db.example.com:5432", IntervalSeconds: 60, TimeoutSeconds: 5},
			{ID: "ipv6", Endpoint: "[::1]:443", IntervalSeconds: 15, TimeoutSeconds: 3},
		}},
		{
			name: "HTTP and TCP Monitors",
			monitors: []configuration.HTTPMonitor{
				{ID: "homepage", Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 60, TimeoutSeconds: 5},
			},
			tcpMonitors: []configuration.TCPMonitor{
				{ID: "database", Endpoint: "127.0.0.1:5432", IntervalSeconds: 60, TimeoutSeconds: 5},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			specification := configuration.NewSpecification(agent.InstanceUID{1})
			specification.HTTPMonitors = test.monitors
			specification.TCPMonitors = test.tcpMonitors
			specification.ICMPMonitors = test.icmpMonitors
			specification.DNSMonitors = test.dnsMonitors
			content, err := configuration.Compile(specification)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(
				ctx, "docker", "run", "--rm",
				"--volume=/:/hostfs:ro",
				"--env=ARVELD_AGENT_TOKEN=test-key",
				"--env=ARVELD_URL=http://arveld.example.com",
				"--env=ARVELD_COLLECTOR_CONFIG",
				"arveld-agent:0.161.0-arveld",
				"collector", "validate", "--config=env:ARVELD_COLLECTOR_CONFIG",
			)
			command.Env = append(os.Environ(), "ARVELD_COLLECTOR_CONFIG="+string(content))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("Collector rejected the compiled configuration: %v\n%s", err, output)
			}
		})
	}
}
