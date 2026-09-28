// Package configuration compiles Arveld specifications into Collector configuration.
package configuration

import (
	"fmt"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

const (
	schemaVersion = 1
	baseVersion   = 4
)

// Specification is an Arveld-owned, JSON-serializable snapshot of compilation inputs.
// Schema 1 describes the base and optional HTTP/TCP/ICMP/DNS Monitors during development.
type Specification struct {
	SchemaVersion int           `json:"schema_version"`
	BaseVersion   int           `json:"base_version"`
	InstanceUID   string        `json:"instance_uid"`
	HTTPMonitors  []HTTPMonitor `json:"http_monitors,omitempty"`
	TCPMonitors   []TCPMonitor  `json:"tcp_monitors,omitempty"`
	ICMPMonitors  []ICMPMonitor `json:"icmp_monitors,omitempty"`
	DNSMonitors   []DNSMonitor  `json:"dns_monitors,omitempty"`
}

// NewSpecification selects the current Arveld base for an Agent.
func NewSpecification(uid agent.InstanceUID) Specification {
	return Specification{
		SchemaVersion: schemaVersion,
		BaseVersion:   baseVersion,
		InstanceUID:   uid.String(),
	}
}

// Compile composes the selected base and Monitors without resolving environment references.
// Unsupported specifications return an error and no configuration bytes.
func Compile(specification Specification) ([]byte, error) {
	if specification.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("unsupported configuration schema version: %d", specification.SchemaVersion)
	}
	if specification.BaseVersion < 1 || specification.BaseVersion > baseVersion {
		return nil, fmt.Errorf("unsupported configuration base version: %d", specification.BaseVersion)
	}
	uid, err := agent.ParseInstanceUID(specification.InstanceUID)
	if err != nil {
		return nil, fmt.Errorf("compile base configuration: %w", err)
	}
	base := fmt.Appendf(nil, baseHostMetricsConfig, uid.String())
	if len(specification.HTTPMonitors)+len(specification.TCPMonitors)+len(specification.ICMPMonitors)+len(specification.DNSMonitors) == 0 {
		return base, nil
	}
	return composeMonitors(base, specification)
}

// The base owns collection policy; credentials remain in the Agent environment.
// Recompilation selects the current base; stored revision bytes remain immutable.
const baseHostMetricsConfig = `receivers:
  hostmetrics:
    root_path: "${env:ARVELD_HOST_ROOT:-/hostfs}"
    collection_interval: 15s
    scrapers:
      cpu:
        metrics:
          system.cpu.time:
            attributes: [cpu, state]
          system.cpu.logical.count:
            enabled: false
      memory:
        metrics:
          system.memory.usage:
            enabled: false
          system.memory.limit:
            enabled: true
          system.linux.memory.available:
            enabled: true
      filesystem:
        metrics:
          system.filesystem.usage:
            enabled: true
          system.filesystem.inodes.usage:
            enabled: false
      network:
        metrics:
          system.network.io:
            enabled: true
          system.network.packets:
            enabled: true
          system.network.errors:
            enabled: true
          system.network.dropped:
            enabled: true
          system.network.connections:
            enabled: false
      system:
        metrics:
          system.uptime:
            enabled: true

processors:
  resource/agent:
    attributes:
      - key: service.name
        value: arveld-agent
        action: upsert
      - key: service.instance.id
        value: "%s"
        action: upsert

exporters:
  otlphttp/arveld:
    endpoint: "${env:ARVELD_URL}/v1/otlp"
    headers:
      Authorization: "Bearer ${env:ARVELD_AGENT_TOKEN}"

service:
  pipelines:
    metrics:
      receivers: [hostmetrics]
      processors: [resource/agent]
      exporters: [otlphttp/arveld]
`
