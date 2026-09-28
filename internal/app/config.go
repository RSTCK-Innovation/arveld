// Package app owns the controller process lifecycle.
package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/components"
)

const defaultPrometheusRetentionTime = "15d"

// Config contains the settings required to run the controller.
type Config struct {
	// Version is supplied by the executable, not by the configuration file.
	Version                  string        `yaml:"-"`
	HTTPAddress              string        `yaml:"http_address"`
	SessionCookieSecure      bool          `yaml:"session_cookie_secure"`
	DatabasePath             string        `yaml:"database_path"`
	PrometheusRetentionTime  string        `yaml:"prometheus_retention_time"`
	PrometheusRetentionSize  string        `yaml:"prometheus_retention_size"`
	PrometheusQueryTimeout   time.Duration `yaml:"prometheus_query_timeout"`
	OTLPMetricsTimeout       time.Duration `yaml:"otlp_metrics_timeout"`
	ComponentDownloadTimeout time.Duration `yaml:"component_download_timeout"`
}

// Validate reports whether the configuration can start the controller.
func (config Config) Validate() error {
	if config.HTTPAddress == "" {
		return errors.New("HTTP address is required")
	}
	if config.DatabasePath == "" {
		return errors.New("database path is required")
	}

	if err := config.validateNetworkTimeouts(); err != nil {
		return err
	}

	if err := components.ValidatePrometheusRetention(components.PrometheusConfig{
		RetentionTime: config.PrometheusRetentionTime,
		RetentionSize: config.PrometheusRetentionSize,
	}); err != nil {
		return fmt.Errorf("validate Prometheus retention: %w", err)
	}

	return nil
}

// validateNetworkTimeouts prevents disabling the network operation limits.
func (config Config) validateNetworkTimeouts() error {
	if config.PrometheusQueryTimeout <= 0 {
		return errors.New("prometheus_query_timeout must be positive")
	}
	if config.OTLPMetricsTimeout <= 0 {
		return errors.New("otlp_metrics_timeout must be positive")
	}
	if config.ComponentDownloadTimeout <= 0 {
		return errors.New("component_download_timeout must be positive")
	}
	return nil
}

// DefaultConfig returns the controller's built-in defaults.
func DefaultConfig() Config {
	return Config{
		Version:                  "dev",
		PrometheusQueryTimeout:   30 * time.Second,
		OTLPMetricsTimeout:       30 * time.Second,
		ComponentDownloadTimeout: 2 * time.Minute,
		HTTPAddress:              "127.0.0.1:8080",
		SessionCookieSecure:      true,
		DatabasePath:             "data/arveld.db",
		PrometheusRetentionTime:  defaultPrometheusRetentionTime,
	}
}
