package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type rejectingTransport struct{}

func (rejectingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected HTTP request")
}

func TestInstallReleasePublishesVerifiedExecutable(t *testing.T) {
	executable := []byte("prometheus executable")
	executablePath := "prometheus-3.13.2.linux-amd64/prometheus"
	archive := makeTestTarGz(t, []testTarEntry{
		{name: executablePath, content: executable},
	})
	checksum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if _, err := response.Write(archive); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()
	dataDirectory := t.TempDir()
	release := componentRelease{
		Version: "3.13.2",
		Artifact: componentArtifact{
			URL:    server.URL + "/prometheus-3.13.2.linux-amd64.tar.gz",
			SHA256: hex.EncodeToString(checksum[:]),
			Size:   int64(len(archive)),
		},
	}

	installer := newInstaller(dataDirectory, server.Client(), discardLogger(), 2*time.Minute)
	installedPath, err := installer.installRelease(
		context.Background(), prometheusComponent, release,
	)
	if err != nil {
		t.Fatalf("install release: %v", err)
	}

	executableName := "prometheus"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	expectedPath := filepath.Join(
		dataDirectory,
		"components",
		"prometheus",
		"3.13.2",
		executableName,
	)
	if installedPath != expectedPath {
		t.Errorf("installed path = %q, want %q", installedPath, expectedPath)
	}
	installed, err := os.ReadFile(installedPath) //nolint:gosec // path is inside t.TempDir
	if err != nil {
		t.Fatalf("read installed executable: %v", err)
	}
	if !bytes.Equal(installed, executable) {
		t.Errorf("installed executable = %q, want %q", installed, executable)
	}
	entries, err := os.ReadDir(filepath.Dir(installedPath))
	if err != nil {
		t.Fatalf("read component version directory: %v", err)
	}
	if len(entries) != 2 ||
		entries[0].Name() != executableName ||
		entries[1].Name() != executableName+executableChecksumSuffix {
		t.Errorf(
			"version directory entries = %v, want executable and checksum",
			entries,
		)
	}
}

func TestInstallReleaseLogsDownloadProgress(t *testing.T) {
	executablePath := "prometheus-3.13.2.linux-amd64/prometheus"
	archive := makeTestTarGz(t, []testTarEntry{
		{name: executablePath, content: []byte("prometheus executable")},
	})
	checksum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if _, err := response.Write(archive); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()
	release := componentRelease{
		Version: "3.13.2",
		Artifact: componentArtifact{
			URL:    server.URL + "/prometheus-3.13.2.linux-amd64.tar.gz",
			SHA256: hex.EncodeToString(checksum[:]),
			Size:   int64(len(archive)),
		},
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	installer := newInstaller(t.TempDir(), server.Client(), logger, 2*time.Minute)
	_, err := installer.installRelease(
		context.Background(), prometheusComponent, release,
	)
	if err != nil {
		t.Fatalf("install release: %v", err)
	}

	for _, expected := range []string{
		`msg="downloading managed component"`,
		"component=prometheus",
		"version=3.13.2",
		`msg="managed component installed"`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Errorf("logs = %q, want %q", logs.String(), expected)
		}
	}
}

func TestInstallReleaseReusesExistingExecutable(t *testing.T) {
	executable := []byte("prometheus executable")
	archive := makeTestTarGz(t, []testTarEntry{
		{
			name:    "prometheus-3.13.2.linux-amd64/prometheus",
			content: executable,
		},
	})
	checksum := sha256.Sum256(archive)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			if _, err := response.Write(archive); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()
	release := componentRelease{
		Version: "3.13.2",
		Artifact: componentArtifact{
			URL:    server.URL + "/prometheus-3.13.2.linux-amd64.tar.gz",
			SHA256: hex.EncodeToString(checksum[:]),
			Size:   int64(len(archive)),
		},
	}
	dataDirectory := t.TempDir()

	installer := newInstaller(dataDirectory, server.Client(), discardLogger(), 2*time.Minute)
	firstPath, err := installer.installRelease(
		context.Background(), prometheusComponent, release,
	)
	if err != nil {
		t.Fatalf("install release first time: %v", err)
	}
	secondPath, err := installer.installRelease(
		context.Background(), prometheusComponent, release,
	)
	if err != nil {
		t.Fatalf("install release second time: %v", err)
	}

	if secondPath != firstPath {
		t.Errorf("second path = %q, want %q", secondPath, firstPath)
	}
	if requests.Load() != 1 {
		t.Errorf("HTTP requests = %d, want 1", requests.Load())
	}
}

func TestInstallReleaseReinstallsInvalidExecutable(t *testing.T) {
	tests := []struct {
		name   string
		damage func(*testing.T, string)
	}{
		{
			name: "corrupted contents",
			damage: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile( //nolint:gosec // corrupted test fixture must remain executable
					path,
					[]byte("corrupted"),
					0o755,
				); err != nil {
					t.Fatalf("corrupt installed executable: %v", err)
				}
			},
		},
		{
			name: "missing executable permission",
			damage: func(t *testing.T, path string) {
				t.Helper()
				if runtime.GOOS == "windows" {
					t.Skip("Windows does not use Unix executable permission bits")
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatalf("remove executable permission: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executable := []byte("prometheus executable")
			archive := makeTestTarGz(t, []testTarEntry{
				{
					name:    "prometheus-3.13.2.linux-amd64/prometheus",
					content: executable,
				},
			})
			checksum := sha256.Sum256(archive)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(
				func(response http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					if _, err := response.Write(archive); err != nil {
						t.Errorf("write HTTP response: %v", err)
					}
				},
			))
			defer server.Close()
			release := componentRelease{
				Version: "3.13.2",
				Artifact: componentArtifact{
					URL:    server.URL + "/prometheus-3.13.2.linux-amd64.tar.gz",
					SHA256: hex.EncodeToString(checksum[:]),
					Size:   int64(len(archive)),
				},
			}
			dataDirectory := t.TempDir()

			installer := newInstaller(dataDirectory, server.Client(), discardLogger(), 2*time.Minute)
			installedPath, err := installer.installRelease(
				context.Background(), prometheusComponent, release,
			)
			if err != nil {
				t.Fatalf("install release first time: %v", err)
			}
			test.damage(t, installedPath)

			if _, err := installer.installRelease(
				context.Background(), prometheusComponent, release,
			); err != nil {
				t.Fatalf("reinstall invalid executable: %v", err)
			}

			if got, want := requests.Load(), int32(2); got != want {
				t.Errorf("HTTP requests = %d, want %d", got, want)
			}
			restored, err := os.ReadFile(installedPath) //nolint:gosec // path is inside t.TempDir
			if err != nil {
				t.Fatalf("read restored executable: %v", err)
			}
			if !bytes.Equal(restored, executable) {
				t.Errorf("restored executable = %q, want %q", restored, executable)
			}
		})
	}
}

func TestInstallerEnsureUsesLockedRelease(t *testing.T) {
	release, err := releaseForCurrentPlatform(prometheusComponent)
	if err != nil {
		t.Fatalf("resolve locked Prometheus release: %v", err)
	}
	dataDirectory := t.TempDir()
	_, expectedPath, err := prepareComponentDirectory(
		dataDirectory,
		prometheusComponent,
		release,
	)
	if err != nil {
		t.Fatalf("prepare component directory: %v", err)
	}
	executable := []byte("prometheus executable")
	if err := os.WriteFile( //nolint:gosec // test fixture must represent a runnable executable
		expectedPath,
		executable,
		0o755,
	); err != nil {
		t.Fatalf("write existing executable: %v", err)
	}
	checksum := sha256.Sum256(executable)
	if err := os.WriteFile(
		expectedPath+executableChecksumSuffix,
		[]byte(hex.EncodeToString(checksum[:])),
		0o600,
	); err != nil {
		t.Fatalf("write existing executable checksum: %v", err)
	}
	installer := newInstaller(
		dataDirectory,
		&http.Client{Transport: rejectingTransport{}},
		nil,
		2*time.Minute,
	)

	installedPath, err := installer.ensure(context.Background(), prometheusComponent)
	if err != nil {
		t.Fatalf("ensure Prometheus: %v", err)
	}

	if installedPath != expectedPath {
		t.Errorf("installed path = %q, want %q", installedPath, expectedPath)
	}
}
