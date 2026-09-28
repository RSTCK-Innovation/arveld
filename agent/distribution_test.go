//go:build docker

package agent_test

import (
	"context"
	"os/exec"
	"slices"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestAgentDistribution(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "run", "--rm",
		"arveld-agent:0.161.0-arveld", "collector", "components",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list Agent components: %v\n%s", err, output)
	}

	type component struct {
		Name   string `yaml:"name"`
		Scheme string `yaml:"scheme"`
	}
	var distribution struct {
		BuildInfo struct {
			Command string `yaml:"command"`
		} `yaml:"buildinfo"`
		Receivers  []component `yaml:"receivers"`
		Processors []component `yaml:"processors"`
		Exporters  []component `yaml:"exporters"`
		Extensions []component `yaml:"extensions"`
		Connectors []component `yaml:"connectors"`
		Providers  []component `yaml:"providers"`
	}
	if err := yaml.Unmarshal(output, &distribution); err != nil {
		t.Fatalf("decode Agent components: %v", err)
	}
	if distribution.BuildInfo.Command != "arveld-agent" {
		t.Errorf("Agent command = %q, want arveld-agent", distribution.BuildInfo.Command)
	}
	for _, group := range []struct {
		name       string
		components []component
		want       []string
	}{
		{"receivers", distribution.Receivers, []string{"dns_check", "host_metrics", "http_check", "icmp_check", "nop", "tcp_check"}},
		{"processors", distribution.Processors, []string{"resource"}},
		{"exporters", distribution.Exporters, []string{"nop", "otlp_http"}},
		{"extensions", distribution.Extensions, []string{"opamp"}},
		{"connectors", distribution.Connectors, nil},
		{"providers", distribution.Providers, []string{"env", "file"}},
	} {
		var names []string
		for _, component := range group.components {
			name := component.Name
			if group.name == "providers" {
				name = component.Scheme
			}
			names = append(names, name)
		}
		slices.Sort(names)
		if !slices.Equal(names, group.want) {
			t.Errorf("Agent %s = %v, want %v", group.name, names, group.want)
		}
	}
}
