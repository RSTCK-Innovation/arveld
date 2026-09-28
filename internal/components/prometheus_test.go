package components

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePrometheusConfigUsesRetentionTime(t *testing.T) {
	dataDirectory := t.TempDir()

	configPath, err := writePrometheusConfig(
		dataDirectory,
		PrometheusConfig{RetentionTime: "30d"},
	)
	if err != nil {
		t.Fatalf("write Prometheus config: %v", err)
	}

	expectedPath := filepath.Join(dataDirectory, "config", "prometheus.yml")
	if configPath != expectedPath {
		t.Errorf("config path = %q, want %q", configPath, expectedPath)
	}

	content, err := os.ReadFile(configPath) //nolint:gosec // path is returned for the test's temporary directory
	if err != nil {
		t.Fatalf("read Prometheus config: %v", err)
	}
	want := `rule_files:
  - alert-rules/*.yml
storage:
  tsdb:
    retention:
      time: 30d
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - 127.0.0.1:19093
scrape_configs: []
otlp:
  promote_resource_attributes:
    - arveld.monitor.id
`
	if got := string(content); got != want {
		t.Errorf("config content = %q, want %q", got, want)
	}
}

func TestWritePrometheusConfigUsesRetentionSizeWhenConfigured(t *testing.T) {
	dataDirectory := t.TempDir()

	configPath, err := writePrometheusConfig(
		dataDirectory,
		PrometheusConfig{
			RetentionTime: "30d",
			RetentionSize: "8GB",
		},
	)
	if err != nil {
		t.Fatalf("write Prometheus config: %v", err)
	}

	content, err := os.ReadFile(configPath) //nolint:gosec // path is returned for the test's temporary directory
	if err != nil {
		t.Fatalf("read Prometheus config: %v", err)
	}
	want := `rule_files:
  - alert-rules/*.yml
storage:
  tsdb:
    retention:
      time: 30d
      size: 8GB
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - 127.0.0.1:19093
scrape_configs: []
otlp:
  promote_resource_attributes:
    - arveld.monitor.id
`
	if got := string(content); got != want {
		t.Errorf("config content = %q, want %q", got, want)
	}
}
