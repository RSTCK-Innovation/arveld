package components

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPublishExecutableMakesTemporaryFileReady(t *testing.T) {
	versionDirectory := filepath.Join(
		t.TempDir(),
		"components",
		"prometheus",
		"3.13.2",
	)
	if err := os.MkdirAll(versionDirectory, 0o750); err != nil {
		t.Fatalf("create version directory: %v", err)
	}
	temporaryFile, err := os.CreateTemp(
		versionDirectory,
		".arveld-component-executable-*.partial",
	)
	if err != nil {
		t.Fatalf("create temporary executable: %v", err)
	}
	temporaryPath := temporaryFile.Name()
	executable := []byte("prometheus executable")
	if _, err := temporaryFile.Write(executable); err != nil {
		t.Fatalf("write temporary executable: %v", err)
	}
	if err := temporaryFile.Close(); err != nil {
		t.Fatalf("close temporary executable: %v", err)
	}
	destinationPath := filepath.Join(versionDirectory, "prometheus")

	if err := publishExecutable(temporaryPath, destinationPath); err != nil {
		t.Fatalf("publish executable: %v", err)
	}

	if _, err := os.Stat(temporaryPath); !os.IsNotExist(err) {
		t.Errorf("temporary file stat error = %v, want not exist", err)
	}
	info, err := os.Stat(destinationPath)
	if err != nil {
		t.Fatalf("stat published executable: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Errorf("executable permissions = %o, want 755", info.Mode().Perm())
	}
	published, err := os.ReadFile(destinationPath) //nolint:gosec // path is inside t.TempDir
	if err != nil {
		t.Fatalf("read published executable: %v", err)
	}
	if !bytes.Equal(published, executable) {
		t.Errorf("published executable = %q, want %q", published, executable)
	}
}

func TestPrepareComponentDirectoryCreatesVersionedDestination(t *testing.T) {
	dataDirectory := t.TempDir()
	release := componentRelease{Version: "3.13.2"}

	versionDirectory, destinationPath, err := prepareComponentDirectory(
		dataDirectory,
		prometheusComponent,
		release,
	)
	if err != nil {
		t.Fatalf("prepare component directory: %v", err)
	}

	expectedDirectory := filepath.Join(
		dataDirectory,
		"components",
		"prometheus",
		"3.13.2",
	)
	if versionDirectory != expectedDirectory {
		t.Errorf("version directory = %q, want %q", versionDirectory, expectedDirectory)
	}
	executableName := "prometheus"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	expectedDestination := filepath.Join(expectedDirectory, executableName)
	if destinationPath != expectedDestination {
		t.Errorf("destination path = %q, want %q", destinationPath, expectedDestination)
	}
	info, err := os.Stat(versionDirectory)
	if err != nil {
		t.Fatalf("stat version directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("version path mode = %v, want directory", info.Mode())
	}
}
