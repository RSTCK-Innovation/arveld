package configuration

import (
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.yaml.in/yaml/v3"
)

// collectorDocument is the compiler's private wire representation, never the
// persisted Arveld specification. Component bodies retain their upstream shape.
type collectorDocument struct {
	Receivers  map[string]any   `mapstructure:"receivers"`
	Processors map[string]any   `mapstructure:"processors"`
	Exporters  map[string]any   `mapstructure:"exporters"`
	Service    collectorService `mapstructure:"service"`
}

type collectorService struct {
	Pipelines map[string]collectorPipeline `mapstructure:"pipelines"`
}

type collectorPipeline struct {
	Receivers  []string `mapstructure:"receivers"`
	Processors []string `mapstructure:"processors"`
	Exporters  []string `mapstructure:"exporters"`
}

func composeMonitors(base []byte, specification Specification) ([]byte, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(base, &raw); err != nil {
		return nil, fmt.Errorf("decode base YAML: %w", err)
	}
	var document collectorDocument
	if err := confmap.NewFromStringMap(raw).Unmarshal(&document); err != nil {
		return nil, fmt.Errorf("decode base Collector configuration: %w", err)
	}
	for _, monitor := range specification.HTTPMonitors {
		if err := addHTTPMonitor(&document, monitor); err != nil {
			return nil, fmt.Errorf("compile HTTP Monitor %q: %w", monitor.ID, err)
		}
	}
	for _, monitor := range specification.TCPMonitors {
		if err := addTCPMonitor(&document, monitor); err != nil {
			return nil, fmt.Errorf("compile TCP Monitor %q: %w", monitor.ID, err)
		}
	}
	for _, monitor := range specification.ICMPMonitors {
		if err := addICMPMonitor(&document, monitor); err != nil {
			return nil, fmt.Errorf("compile ICMP Monitor %q: %w", monitor.ID, err)
		}
	}
	for _, monitor := range specification.DNSMonitors {
		if err := addDNSMonitor(&document, monitor); err != nil {
			return nil, fmt.Errorf("compile DNS Monitor %q: %w", monitor.ID, err)
		}
	}
	compiled := confmap.New()
	if err := compiled.Marshal(document); err != nil {
		return nil, fmt.Errorf("encode Collector configuration: %w", err)
	}
	content, err := yaml.Marshal(compiled.ToStringMap())
	if err != nil {
		return nil, fmt.Errorf("encode Collector YAML: %w", err)
	}
	return content, nil
}

func collectorID(kind, name string) (string, error) {
	var id component.ID
	if err := id.UnmarshalText([]byte(kind + "/" + name)); err != nil {
		return "", fmt.Errorf("invalid component ID: %w", err)
	}
	if id.Name() != name {
		return "", errors.New("component name must not contain surrounding whitespace")
	}
	return id.String(), nil
}

func addMonitorReceiver(document *collectorDocument, kind, id string, receiver map[string]any) error {
	if strings.ContainsAny(id, "/:") {
		return errors.New("monitor ID must not contain component or configuration delimiters")
	}
	receiverID, err := collectorID(kind, id)
	if err != nil {
		return err
	}
	processorID, err := collectorID("resource", "monitor_"+id)
	if err != nil {
		return err
	}
	pipelineID, err := collectorID("metrics", "monitor_"+id)
	if err != nil {
		return err
	}
	if _, exists := document.Service.Pipelines[pipelineID]; exists {
		return errors.New("duplicate Monitor ID")
	}
	document.Receivers[receiverID] = receiver
	document.Processors[processorID] = map[string]any{
		"attributes": []map[string]any{{"key": "arveld.monitor.id", "value": id, "action": "upsert"}},
	}
	document.Service.Pipelines[pipelineID] = collectorPipeline{
		Receivers: []string{receiverID}, Processors: []string{"resource/agent", processorID}, Exporters: []string{"otlphttp/arveld"},
	}
	return nil
}
