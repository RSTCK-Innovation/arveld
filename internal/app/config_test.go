package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadConfigRejectsRemovedObservabilitySettings(t *testing.T) {
	for _, setting := range []string{
		"observability_mode: external",
		"observability_mode: managed",
		"prometheus_url: https://prometheus.example",
		"alertmanager_url: https://alertmanager.example",
	} {
		t.Run(setting, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arveld.yml")
			if err := os.WriteFile(path, []byte(setting+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			field, _, _ := strings.Cut(setting, ":")
			if err == nil || !strings.Contains(err.Error(), "field "+field+" not found") {
				t.Fatalf("LoadConfig() error = %v, want removed field %q rejected", err, field)
			}
		})
	}
}

func TestLoadConfigNetworkTimeouts(t *testing.T) {
	for _, test := range []struct {
		name string
		yaml string
		want [3]time.Duration
	}{
		{"defaults", "", [3]time.Duration{30 * time.Second, 30 * time.Second, 2 * time.Minute}},
		{"custom", "prometheus_query_timeout: 7s\notlp_metrics_timeout: 11s\ncomponent_download_timeout: 3m\n", [3]time.Duration{7 * time.Second, 11 * time.Second, 3 * time.Minute}},
		{"partial", "otlp_metrics_timeout: 500ms\n", [3]time.Duration{30 * time.Second, 500 * time.Millisecond, 2 * time.Minute}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			config, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := config.Validate(); err != nil {
				t.Fatal(err)
			}
			got := [3]time.Duration{config.PrometheusQueryTimeout, config.OTLPMetricsTimeout, config.ComponentDownloadTimeout}
			if got != test.want {
				t.Errorf("timeouts = %v, want %v", got, test.want)
			}
			generatedPath := filepath.Join(t.TempDir(), "generated.yml")
			if err := writeConfigFile(generatedPath, config); err != nil {
				t.Fatal(err)
			}
			reloaded, err := LoadConfig(generatedPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded != config {
				t.Errorf("reloaded configuration = %+v, want %+v", reloaded, config)
			}
		})
	}
}

func TestConfigRejectsInvalidNetworkTimeouts(t *testing.T) {
	for _, key := range []string{"prometheus_query_timeout", "otlp_metrics_timeout", "component_download_timeout"} {
		for _, value := range []string{"0s", "-1s", "30", "forever", "999999999999h"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.yml")
				if err := os.WriteFile(path, []byte(key+": "+value+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				config, err := LoadConfig(path)
				if err == nil {
					err = config.Validate()
				}
				if err == nil {
					t.Fatal("invalid timeout was accepted")
				}
			})
		}
	}
}

func TestLoadConfigCreatesDefaultFileWithoutAgentToken(t *testing.T) {
	t.Chdir(t.TempDir())

	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	if got, want := config.HTTPAddress, "127.0.0.1:8080"; got != want {
		t.Errorf("HTTPAddress = %q, want %q", got, want)
	}

	content, err := os.ReadFile(filepath.Join("data", "arveld.yml"))
	if err != nil {
		t.Fatalf("read generated configuration: %v", err)
	}
	if strings.Contains(string(content), "agent_token") {
		t.Error("generated configuration contains the removed agent_token field")
	}
	if config != DefaultConfig() {
		t.Error("generated configuration differs from the built-in defaults")
	}
	if err := config.Validate(); err != nil {
		t.Errorf("generated config validation error = %v, want nil", err)
	}

	reloadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("reload generated config: %v", err)
	}
	if reloadedConfig != config {
		t.Error("reloaded configuration differs from the generated configuration")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join("data", "arveld.yml"))
		if err != nil {
			t.Fatalf("stat generated config: %v", err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
			t.Errorf("generated config permissions = %o, want %o", got, want)
		}
	}
}

func TestLoadConfigConcurrentDefaultCreationUsesPersistedConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	const callerCount = 32
	type loadResult struct {
		config Config
		err    error
	}

	start := make(chan struct{})
	results := make(chan loadResult, callerCount)
	var callers sync.WaitGroup
	for range callerCount {
		callers.Go(func() {
			<-start
			config, err := LoadConfig("")
			results <- loadResult{config: config, err: err}
		})
	}
	close(start)
	callers.Wait()
	close(results)

	persisted, err := loadConfigFile(defaultConfigPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	for result := range results {
		if result.err != nil {
			t.Errorf("concurrent LoadConfig() error = %v, want nil", result.err)
			continue
		}
		if result.config != persisted {
			t.Errorf(
				"concurrent configuration = %+v, want persisted configuration %+v",
				result.config,
				persisted,
			)
		}
	}
}

func TestWriteConfigFileDoesNotPublishPartialContent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "data", "arveld.yml")
	writeFailure := errors.New("simulated write failure")

	err := writeConfigFileWithWriter(
		configPath,
		DefaultConfig(),
		func(file *os.File, content []byte) error {
			if _, err := file.Write(content[:len(content)/2]); err != nil {
				return fmt.Errorf("write partial test content: %w", err)
			}

			return writeFailure
		},
	)
	if !errors.Is(err, writeFailure) {
		t.Fatalf("writeConfigFileWithWriter() error = %v, want simulated failure", err)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("final config stat error = %v, want file not to exist", err)
	}
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read config directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("config directory contains %v, want no partial files", entries)
	}
}

func TestLoadConfigLoadsExistingDefaultFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("data", 0o700); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	content := `http_address: 0.0.0.0:8081
database_path: data/custom.db
prometheus_retention_time: 30d
prometheus_retention_size: 8GB
`
	if err := os.WriteFile(filepath.Join("data", "arveld.yml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	want := Config{
		Version:                  "dev",
		PrometheusQueryTimeout:   30 * time.Second,
		OTLPMetricsTimeout:       30 * time.Second,
		ComponentDownloadTimeout: 2 * time.Minute,
		HTTPAddress:              "0.0.0.0:8081",
		SessionCookieSecure:      true,
		DatabasePath:             "data/custom.db",
		PrometheusRetentionTime:  "30d",
		PrometheusRetentionSize:  "8GB",
	}
	if config != want {
		t.Errorf("LoadConfig() = %+v, want %+v", config, want)
	}
}

func TestLoadConfigLoadsExplicitFileOverDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.yml")
	if err := os.WriteFile(configPath, []byte("database_path: data/explicit.db\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	if got, want := config.DatabasePath, "data/explicit.db"; got != want {
		t.Errorf("DatabasePath = %q, want %q", got, want)
	}
	if got, want := config.HTTPAddress, "127.0.0.1:8080"; got != want {
		t.Errorf("HTTPAddress = %q, want default %q", got, want)
	}
	if got, want := config.PrometheusRetentionTime, "15d"; got != want {
		t.Errorf("PrometheusRetentionTime = %q, want default %q", got, want)
	}
	if config.PrometheusRetentionSize != "" {
		t.Errorf("PrometheusRetentionSize = %q, want no size limit", config.PrometheusRetentionSize)
	}
}

func TestLoadConfigRejectsUnknownYAMLField(t *testing.T) {
	for _, field := range []string{"unknown_field", "agent_token"} {
		t.Run(field, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "custom.yml")
			if err := os.WriteFile(configPath, []byte(field+": unsupported\n"), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := LoadConfig(configPath)
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want an unknown field error")
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("LoadConfig() error = %q, want it to mention the unknown field", err)
			}
		})
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	config := DefaultConfig()
	if err := config.Validate(); err != nil {
		t.Fatalf("default configuration validation = %v, want nil without static credentials", err)
	}
}

func TestConfigValidateRequiresRetentionTime(t *testing.T) {
	config := DefaultConfig()
	config.PrometheusRetentionTime = ""

	err := config.Validate()

	if err == nil {
		t.Fatal("Validate() error = nil, want a Prometheus retention time error")
	}
	if !strings.Contains(err.Error(), "retention time") {
		t.Errorf("Validate() error = %q, want it to mention the retention time", err)
	}
}

func TestConfigValidateRejectsInvalidPrometheusRetentionTime(t *testing.T) {
	config := DefaultConfig()
	config.PrometheusRetentionTime = "fifteen-days"

	err := config.Validate()

	if err == nil {
		t.Fatal("Validate() error = nil, want an invalid Prometheus retention time error")
	}
	if !strings.Contains(err.Error(), "prometheus retention time") {
		t.Errorf("Validate() error = %q, want it to mention the retention time", err)
	}
}

func TestConfigValidateRejectsInvalidPrometheusRetentionSize(t *testing.T) {
	config := DefaultConfig()
	config.PrometheusRetentionSize = "8XB"

	err := config.Validate()

	if err == nil {
		t.Fatal("Validate() error = nil, want an invalid Prometheus retention size error")
	}
	if !strings.Contains(err.Error(), "prometheus retention size") {
		t.Errorf("Validate() error = %q, want it to mention the retention size", err)
	}
}

func TestConfigValidateRejectsNegativePrometheusRetentionSize(t *testing.T) {
	config := DefaultConfig()
	config.PrometheusRetentionSize = "-8GB"

	err := config.Validate()

	if err == nil {
		t.Fatal("Validate() error = nil, want a negative Prometheus retention size error")
	}
	if !strings.Contains(err.Error(), "greater than or equal to zero") {
		t.Errorf("Validate() error = %q, want it to reject a negative size", err)
	}
}

func TestConfigValidateAcceptsPrometheusRetentionFormats(t *testing.T) {
	tests := []struct {
		name string
		time string
		size string
	}{
		{
			name: "combined duration and binary size",
			time: "1w2d3h4m5s6ms",
			size: "8GiB",
		},
		{
			name: "disabled time and size",
			time: "0",
			size: "0",
		},
		{
			name: "no size limit",
			time: "15d",
			size: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.PrometheusRetentionTime = test.time
			config.PrometheusRetentionSize = test.size

			if err := config.Validate(); err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestLoadConfigSessionCookieSecurity(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		secure     bool
	}{
		{"default", "", true},
		{"local HTTP", "session_cookie_secure: false\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arveld.yml")
			if err := os.WriteFile(path, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			config, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if config.SessionCookieSecure != test.secure {
				t.Errorf("SessionCookieSecure = %t, want %t", config.SessionCookieSecure, test.secure)
			}
		})
	}
}
