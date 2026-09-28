package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishImagesUsesReleaseTags(t *testing.T) {
	for _, version := range []string{"v0.1.0-rc.3", "v12.34.56-rc.78"} {
		t.Run(version, func(t *testing.T) {
			directory, command := releasePublisherFixture(t, version, "")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("publish images: %v\n%s", err, output)
			}
			compose := readReleaseFixture(t, filepath.Join(directory, "release/assets/compose.yaml"))
			images := readReleaseFixture(t, filepath.Join(directory, "release/assets/images.txt"))
			outputs := readReleaseFixture(t, filepath.Join(directory, "outputs"))
			calls := readReleaseFixture(t, filepath.Join(directory, "docker-calls"))
			for _, component := range []struct {
				name   string
				prefix string
				hex    string
			}{
				{name: "arveld", prefix: "controller", hex: "a"},
				{name: "arveld-agent", prefix: "agent", hex: "b"},
			} {
				image := "ghcr.io/rstck-innovation/" + component.name
				digest := "sha256:" + strings.Repeat(component.hex, 64)
				if want := "    image: " + image + ":" + version + "\n"; !strings.Contains(compose, want) {
					t.Errorf("release Compose must select %s by release tag, got:\n%s", component.name, compose)
				}
				if want := image + ":" + version + "@" + digest + "\n"; !strings.Contains(images, want) {
					t.Errorf("image inventory must link the release tag to its digest, got:\n%s", images)
				}
				if !strings.Contains(outputs, component.prefix+"_name="+image+"\n") ||
					!strings.Contains(outputs, component.prefix+"_digest="+digest+"\n") {
					t.Errorf("attestation outputs must retain the image name and digest, got:\n%s", outputs)
				}
				if !strings.Contains(calls, "buildx imagetools create --tag "+image+":"+version+" ") {
					t.Errorf("release image tag was not published, got:\n%s", calls)
				}
			}
			for _, setting := range []string{"${ARVELD_HTTP_PORT:-8080}", "${ARVELD_SESSION_COOKIE_SECURE:-false}", "ARVELD_AGENT_TOKEN"} {
				if !strings.Contains(compose, setting) {
					t.Errorf("publication must preserve deployment setting %s", setting)
				}
			}
			verify := exec.CommandContext(t.Context(), "sha256sum", "--check", "SHA256SUMS")
			verify.Dir = filepath.Join(directory, "release/assets")
			if output, err := verify.CombinedOutput(); err != nil {
				t.Fatalf("verify release asset checksums: %v\n%s", err, output)
			}
		})
	}
}

func TestPublishImagesRefusesExistingReleaseOrAPIError(t *testing.T) {
	for _, state := range []string{"existing", "error"} {
		t.Run(state, func(t *testing.T) {
			directory, command := releasePublisherFixture(t, "v0.1.0-rc.3", state)
			if output, err := command.CombinedOutput(); err == nil {
				t.Fatalf("publication must fail when release lookup returns %s, got:\n%s", state, output)
			}
			if _, err := os.Stat(filepath.Join(directory, "docker-calls")); !os.IsNotExist(err) {
				t.Fatalf("publication must stop before any registry operation: %v", err)
			}
		})
	}
}

// Run the real publisher with registry clients replaced, without Docker or network access.
func releasePublisherFixture(t *testing.T, version, state string) (string, *exec.Cmd) {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"bin", "docker", "internal/components", "release/assets"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"docker/compose.release.yaml", "internal/components/components.lock.json"} {
		contents := readReleaseFixture(t, filepath.Join("../..", name))
		writeReleaseFixture(t, filepath.Join(directory, name), contents)
	}
	writeReleaseFixture(t, filepath.Join(directory, "bin/gh"), `#!/usr/bin/env bash
set -euo pipefail
case "$RELEASE_TEST_STATE" in
  existing) echo "$VERSION" ;;
  error) exit 1 ;;
esac
`)
	writeReleaseFixture(t, filepath.Join(directory, "bin/docker"), `#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "$RELEASE_TEST_DOCKER_CALLS"
case "$1" in
  load|tag|push) ;;
  buildx)
    case "$2 $3" in
      'imagetools create') ;;
      'imagetools inspect')
        case "$4" in
          */arveld-agent:*) hex=b ;;
          */arveld:*) hex=a ;;
          *) exit 1 ;;
        esac
        printf '{"digest":"sha256:%s"}\n' "$(printf "$hex%.0s" {1..64})"
        ;;
      *) exit 1 ;;
    esac
    ;;
  *) exit 1 ;;
esac
`)
	script, err := filepath.Abs("../../scripts/publish-images.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "bash", script) //nolint:gosec // Fixed repository script, resolved before entering the fixture directory.
	command.Dir = directory
	command.Env = append(os.Environ(),
		"PATH="+filepath.Join(directory, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"VERSION="+version, "GH_REPO=RSTCK-Innovation/arveld",
		"GITHUB_OUTPUT="+filepath.Join(directory, "outputs"),
		"RELEASE_TEST_STATE="+state,
		"RELEASE_TEST_DOCKER_CALLS="+filepath.Join(directory, "docker-calls"),
	)
	return directory, command
}

func readReleaseFixture(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name) //nolint:gosec // Repository or test-owned fixture path.
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func writeReleaseFixture(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o700); err != nil { //nolint:gosec // Test-owned executable fixture.
		t.Fatal(err)
	}
}
