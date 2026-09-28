// Package components resolves the upstream executables managed by Arveld.
package components

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
)

const supportedManifestSchema = 1

// componentName identifies an upstream executable managed by Arveld.
type componentName string

const (
	// prometheusComponent identifies the Prometheus server.
	prometheusComponent componentName = "prometheus"
	// alertmanagerComponent identifies the Prometheus Alertmanager.
	alertmanagerComponent componentName = "alertmanager"
)

// componentArtifact describes one downloadable archive for a platform.
type componentArtifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// componentRelease contains a component version and its artifact for the current platform.
type componentRelease struct {
	Version  string
	Artifact componentArtifact
}

type manifest struct {
	SchemaVersion int                         `json:"schema_version"`
	Components    map[componentName]component `json:"components"`
}

type component struct {
	Version   string                       `json:"version"`
	Artifacts map[string]componentArtifact `json:"artifacts"`
}

//go:embed components.lock.json
var embeddedManifest []byte

// releaseForCurrentPlatform returns a component release for the compiled OS and architecture.
func releaseForCurrentPlatform(name componentName) (componentRelease, error) {
	var lock manifest
	if err := json.Unmarshal(embeddedManifest, &lock); err != nil {
		return componentRelease{}, fmt.Errorf("decode component manifest: %w", err)
	}
	if lock.SchemaVersion != supportedManifestSchema {
		return componentRelease{}, fmt.Errorf(
			"unsupported component manifest schema %d",
			lock.SchemaVersion,
		)
	}

	lockedComponent, ok := lock.Components[name]
	if !ok {
		return componentRelease{}, fmt.Errorf("component %q is not locked", name)
	}

	platform := runtime.GOOS + "/" + runtime.GOARCH
	artifact, ok := lockedComponent.Artifacts[platform]
	if !ok {
		return componentRelease{}, fmt.Errorf(
			"component %q has no artifact for platform %q",
			name,
			platform,
		)
	}

	return componentRelease{
		Version:  lockedComponent.Version,
		Artifact: artifact,
	}, nil
}
