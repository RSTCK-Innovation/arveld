package components

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestManifestContainsValidArtifactsForEveryManagedPlatform(t *testing.T) {
	var lock manifest
	if err := json.Unmarshal(embeddedManifest, &lock); err != nil {
		t.Fatalf("decode locked manifest: %v", err)
	}
	if lock.SchemaVersion != supportedManifestSchema {
		t.Fatalf("manifest schema = %d, want %d", lock.SchemaVersion, supportedManifestSchema)
	}
	components := []componentName{prometheusComponent, alertmanagerComponent}
	platforms := []string{
		"darwin/amd64", "darwin/arm64",
		"linux/amd64", "linux/arm64",
		"windows/amd64", "windows/arm64",
	}
	if len(lock.Components) != len(components) {
		t.Errorf("locked components = %d, want %d", len(lock.Components), len(components))
	}
	for _, name := range components {
		t.Run(string(name), func(t *testing.T) {
			component, ok := lock.Components[name]
			if !ok || component.Version == "" {
				t.Fatal("managed component has no locked version")
			}
			if len(component.Artifacts) != len(platforms) {
				t.Errorf("locked platforms = %d, want %d", len(component.Artifacts), len(platforms))
			}
			for _, platform := range platforms {
				t.Run(platform, func(t *testing.T) {
					artifact, ok := component.Artifacts[platform]
					if !ok {
						t.Fatal("missing platform artifact")
					}
					extension := ".tar.gz"
					if strings.HasPrefix(platform, "windows/") {
						extension = ".zip"
					}
					wantURL := fmt.Sprintf(
						"https://github.com/prometheus/%s/releases/download/v%s/%s-%s.%s%s",
						name, component.Version, name, component.Version,
						strings.ReplaceAll(platform, "/", "-"), extension,
					)
					if artifact.URL != wantURL {
						t.Errorf("artifact URL = %q, want %q", artifact.URL, wantURL)
					}
					digest, err := hex.DecodeString(artifact.SHA256)
					if err != nil || len(digest) != sha256.Size {
						t.Errorf("artifact SHA-256 = %q, want a 32-byte hex digest", artifact.SHA256)
					}
					if artifact.Size <= 0 {
						t.Errorf("artifact size = %d, want a positive size", artifact.Size)
					}
				})
			}
		})
	}
}

func TestReleaseForCurrentPlatformReturnsPrometheusRelease(t *testing.T) {
	release, err := releaseForCurrentPlatform(prometheusComponent)
	if err != nil {
		t.Fatalf("resolve Prometheus release: %v", err)
	}

	if release.Version == "" {
		t.Error("version is empty")
	}

	platformFragment := "." + runtime.GOOS + "-" + runtime.GOARCH + "."
	if !strings.Contains(release.Artifact.URL, platformFragment) {
		t.Errorf(
			"artifact URL = %q, want platform fragment %q",
			release.Artifact.URL,
			platformFragment,
		)
	}
	if len(release.Artifact.SHA256) != 64 {
		t.Errorf("SHA-256 length = %d, want 64", len(release.Artifact.SHA256))
	}
	if release.Artifact.Size <= 0 {
		t.Errorf("artifact size = %d, want a positive size", release.Artifact.Size)
	}
}
