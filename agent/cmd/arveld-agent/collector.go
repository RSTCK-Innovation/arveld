package main

import (
	"fmt"
	"os"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/otelcol"
)

func runCollector(arguments []string) error {
	if _, configured := os.LookupEnv("ARVELD_HOST_ROOT"); !configured {
		if err := os.Setenv("ARVELD_HOST_ROOT", "/"); err != nil {
			return fmt.Errorf("set native host root: %w", err)
		}
	}
	command := otelcol.NewCommand(otelcol.CollectorSettings{
		BuildInfo: component.BuildInfo{
			Command:     "arveld-agent",
			Description: "Arveld Agent",
			Version:     version,
		},
		Factories: components,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				ProviderFactories: []confmap.ProviderFactory{
					envprovider.NewFactory(),
					fileprovider.NewFactory(),
				},
			},
		},
		ProviderModules: map[string]string{
			"env":  "go.opentelemetry.io/collector/confmap/provider/envprovider v1.67.0",
			"file": "go.opentelemetry.io/collector/confmap/provider/fileprovider v1.67.0",
		},
	})
	command.SetArgs(arguments)
	if err := command.Execute(); err != nil {
		return fmt.Errorf("run Collector: %w", err)
	}
	return nil
}
