package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComponentUpdateReport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		current  string
		releases string
		want     string
		wantErr  bool
	}{
		{
			name:     "numeric versions and stable releases",
			current:  "3.9.0",
			releases: `[{"version":"v3.9.1"},{"version":"v3.10.0"},{"version":"v4.0.0-rc.1"}]`,
			want:     "| prometheus | 3.9.0 | 3.10.0 | Update available |",
		},
		{
			name: "already current", current: "3.10.0",
			releases: `[{"version":"v3.10.0"},{"version":"v3.9.5"}]`,
			want:     "| prometheus | 3.10.0 | 3.10.0 | Up to date |",
		},
		{
			name: "never suggest a downgrade", current: "3.11.0",
			releases: `[{"version":"v3.10.0"}]`,
			want:     "| prometheus | 3.11.0 | 3.10.0 | Pinned version is newer |",
		},
		{
			name: "missing stable release is an error", current: "3.9.0",
			releases: `[{"version":"v4.0.0-rc.1"}]`, wantErr: true,
		},
		{
			name: "missing component is an error", current: "3.9.0",
			releases: `null`, wantErr: true,
		},
		{
			name: "invalid pinned version is an error", current: "invalid",
			releases: `[{"version":"v3.10.0"}]`, wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			lock := filepath.Join(directory, "components.lock.json")
			original := []byte(`{"schema_version":1,"components":{"prometheus":{"version":"` + tc.current + `"},"alertmanager":{"version":"0.34.0"}}}`)
			if err := os.WriteFile(lock, original, 0o600); err != nil {
				t.Fatal(err)
			}
			index := filepath.Join(directory, "releases.json")
			if err := os.WriteFile(index, []byte(`{"prometheus":`+tc.releases+`,"alertmanager":[{"version":"v0.34.1"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), "bash", "../../scripts/check-component-updates.sh")
			command.Env = append(os.Environ(), "COMPONENTS_LOCK_FILE="+lock, "PROMETHEUS_RELEASES_URL=file://"+index)
			output, err := command.CombinedOutput()
			if (err != nil) != tc.wantErr {
				t.Fatalf("report error = %v, want error %v:\n%s", err, tc.wantErr, output)
			}
			if !tc.wantErr && !strings.Contains(string(output), tc.want) {
				t.Errorf("report does not contain %q:\n%s", tc.want, output)
			}
			if !tc.wantErr && !strings.Contains(string(output), "| alertmanager | 0.34.0 | 0.34.1 | Update available |") {
				t.Errorf("report omits Alertmanager update:\n%s", output)
			}
			unchanged, err := os.ReadFile(lock) //nolint:gosec // Test-owned fixture path under t.TempDir().
			if err != nil || string(unchanged) != string(original) {
				t.Fatalf("report modified the component lockfile: %v", err)
			}
		})
	}
}
