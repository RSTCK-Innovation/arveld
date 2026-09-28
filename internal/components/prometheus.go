package components

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/prometheus/common/model"
	"go.yaml.in/yaml/v3"
)

// PrometheusConfig contains the managed Prometheus configuration.
type PrometheusConfig struct {
	RetentionTime string
	RetentionSize string
}

// ValidatePrometheusRetention reports whether Prometheus accepts the retention settings.
func ValidatePrometheusRetention(config PrometheusConfig) error {
	if _, err := model.ParseDuration(config.RetentionTime); err != nil {
		return fmt.Errorf(
			"prometheus retention time %q is invalid: %w",
			config.RetentionTime,
			err,
		)
	}
	if config.RetentionSize == "" {
		return nil
	}

	retentionSize, err := parsePrometheusRetentionSize(config.RetentionSize)
	if err != nil {
		return fmt.Errorf(
			"prometheus retention size %q is invalid: %w",
			config.RetentionSize,
			err,
		)
	}
	if retentionSize < 0 {
		return fmt.Errorf(
			"prometheus retention size %q must be greater than or equal to zero",
			config.RetentionSize,
		)
	}

	return nil
}

type prometheusFileConfig struct {
	RuleFiles     []string                 `yaml:"rule_files"`
	Storage       prometheusStorageConfig  `yaml:"storage"`
	Alerting      prometheusAlertingConfig `yaml:"alerting"`
	ScrapeConfigs []struct{}               `yaml:"scrape_configs"`
	OTLP          prometheusOTLPConfig     `yaml:"otlp"`
}

type prometheusAlertingConfig struct {
	Alertmanagers []prometheusAlertmanagerConfig `yaml:"alertmanagers"`
}
type prometheusAlertmanagerConfig struct {
	StaticConfigs []prometheusStaticTargets `yaml:"static_configs"`
}
type prometheusStaticTargets struct {
	Targets []string `yaml:"targets"`
}

type prometheusOTLPConfig struct {
	PromoteResourceAttributes []string `yaml:"promote_resource_attributes"`
}

type prometheusStorageConfig struct {
	TSDB prometheusTSDBConfig `yaml:"tsdb"`
}

type prometheusTSDBConfig struct {
	Retention prometheusRetentionConfig `yaml:"retention"`
}

type prometheusRetentionConfig struct {
	Time string `yaml:"time"`
	Size string `yaml:"size,omitempty"`
}

const prometheusListenAddress = "127.0.0.1:19090"

// ManagedPrometheusURL is the base URL of managed Prometheus.
const ManagedPrometheusURL = "http://" + prometheusListenAddress

// ManagedPrometheusReadinessEndpoint is the readiness endpoint of managed Prometheus.
const ManagedPrometheusReadinessEndpoint = ManagedPrometheusURL + "/-/ready"

// writePrometheusConfig writes the managed Prometheus configuration and returns its path.
func writePrometheusConfig(
	dataDirectory string,
	config PrometheusConfig,
) (string, error) {
	var content bytes.Buffer
	encoder := yaml.NewEncoder(&content)
	encoder.SetIndent(2)
	if err := encoder.Encode(prometheusFileConfig{
		RuleFiles: []string{"alert-rules/*.yml"},
		Storage: prometheusStorageConfig{
			TSDB: prometheusTSDBConfig{
				Retention: prometheusRetentionConfig{
					Time: config.RetentionTime,
					Size: config.RetentionSize,
				},
			},
		},
		Alerting:      prometheusAlertingConfig{Alertmanagers: []prometheusAlertmanagerConfig{{StaticConfigs: []prometheusStaticTargets{{Targets: []string{alertmanagerListenAddress}}}}}},
		ScrapeConfigs: []struct{}{},
		OTLP: prometheusOTLPConfig{
			PromoteResourceAttributes: []string{"arveld.monitor.id"},
		},
	}); err != nil {
		return "", fmt.Errorf("encode managed Prometheus config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close managed Prometheus config encoder: %w", err)
	}

	return writeComponentConfig(dataDirectory, prometheusComponent, content.String())
}

// runPrometheus prepares and supervises the managed Prometheus process.
func (supervisor *Supervisor) runPrometheus(ctx context.Context) error {
	configPath, err := writePrometheusConfig(
		supervisor.dataDirectory,
		supervisor.prometheusConfig,
	)
	if err != nil {
		return fmt.Errorf("write managed Prometheus config: %w", err)
	}

	if err := supervisor.runComponent(
		ctx,
		prometheusComponent,
		"--config.file="+configPath,
		"--storage.tsdb.path="+filepath.Join(supervisor.dataDirectory, "prometheus"),
		"--web.listen-address="+prometheusListenAddress,
		"--web.enable-otlp-receiver",
		"--web.enable-lifecycle",
		// Covers max(60, 2*interval+timeout): interval <= 3600s, timeout <= 60s.
		"--query.lookback-delta=2h1m",
		"--log.format=json",
	); err != nil {
		return fmt.Errorf("run managed Prometheus: %w", err)
	}

	return nil
}
