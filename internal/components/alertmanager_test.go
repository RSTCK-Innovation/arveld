package components

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAlertmanagerConfigWritesProvidedConfig(t *testing.T) {
	dataDirectory := t.TempDir()
	expectedContent := "route:\n  receiver: arveld\nreceivers:\n  - name: arveld\n  - name: operations\n"

	configPath, err := WriteAlertmanagerConfig(dataDirectory, []byte(expectedContent))
	if err != nil {
		t.Fatalf("write Alertmanager config: %v", err)
	}

	expectedPath := filepath.Join(dataDirectory, "config", "alertmanager.yml")
	if configPath != expectedPath {
		t.Errorf("config path = %q, want %q", configPath, expectedPath)
	}

	content, err := os.ReadFile(configPath) //nolint:gosec // path is returned for the test's temporary directory
	if err != nil {
		t.Fatalf("read Alertmanager config: %v", err)
	}
	if got := string(content); got != expectedContent {
		t.Errorf("config content = %q, want %q", got, expectedContent)
	}
}
