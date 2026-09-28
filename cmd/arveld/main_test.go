package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunLoadsExplicitConfigFile(t *testing.T) {
	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)
	configPath := filepath.Join(workingDirectory, "invalid.yml")
	if err := os.WriteFile(configPath, []byte("unknown_field: true\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if got, want := run([]string{"--config", configPath}), 1; got != want {
		t.Errorf("run() exit code = %d, want %d", got, want)
	}
	if _, err := os.Stat(filepath.Join("data", "arveld.yml")); !os.IsNotExist(err) {
		t.Errorf("default config stat error = %v, want file not to exist", err)
	}
}
