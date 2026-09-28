package integration

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCLIVersionRequiresNoConfigurationOrState(t *testing.T) {
	executable := buildCLI(t)
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--config=missing.yml", "--version") //nolint:gosec // Test-built executable and fixed arguments, without a shell.
	command.Dir = directory
	command.Env = []string{"PATH="}
	if output, err := command.CombinedOutput(); err != nil || string(output) != "arveld dev\n" {
		t.Fatalf("version without configuration = %q, %v", output, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version created state: %v, %v", entries, err)
	}
}
