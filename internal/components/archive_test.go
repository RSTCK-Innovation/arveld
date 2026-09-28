package components

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTarEntry struct {
	name    string
	content []byte
}

type testZipEntry struct {
	name    string
	content []byte
}

func TestExtractTarGzExecutableCopiesExpectedFile(t *testing.T) {
	executablePath := "prometheus-3.13.2.linux-amd64/prometheus"
	executable := []byte("prometheus executable")
	archive := makeTestTarGz(t, []testTarEntry{
		{name: "prometheus-3.13.2.linux-amd64/NOTICE", content: []byte("notice")},
		{name: executablePath, content: executable},
	})
	var destination bytes.Buffer

	err := extractTarGzExecutable(
		&destination,
		bytes.NewReader(archive),
		executablePath,
	)
	if err != nil {
		t.Fatalf("extract tar.gz executable: %v", err)
	}

	if !bytes.Equal(destination.Bytes(), executable) {
		t.Errorf("executable content = %q, want %q", destination.Bytes(), executable)
	}
}

func TestExtractTarGzExecutableRejectsOversizedFile(t *testing.T) {
	executablePath := "prometheus-3.13.2.linux-amd64/prometheus"
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     executablePath,
		Mode:     0o600,
		Size:     maxExtractedExecutableSize + 1,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write oversized tar header: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	err := extractTarGzExecutable(
		&bytes.Buffer{},
		bytes.NewReader(archive.Bytes()),
		executablePath,
	)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v, want size limit error", err)
	}
}

func makeTestTarGz(t *testing.T, entries []testTarEntry) []byte {
	t.Helper()

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0o600,
			Size:     int64(len(entry.content)),
			Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write(entry.content); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return archive.Bytes()
}

func TestExtractZipExecutableCopiesExpectedFile(t *testing.T) {
	executablePath := "prometheus-3.13.2.windows-amd64/prometheus.exe"
	executable := []byte("prometheus executable")
	archive := makeTestZip(t, []testZipEntry{
		{name: "prometheus-3.13.2.windows-amd64/NOTICE", content: []byte("notice")},
		{name: executablePath, content: executable},
	})
	var destination bytes.Buffer

	err := extractZipExecutable(
		&destination,
		bytes.NewReader(archive),
		int64(len(archive)),
		executablePath,
	)
	if err != nil {
		t.Fatalf("extract ZIP executable: %v", err)
	}

	if !bytes.Equal(destination.Bytes(), executable) {
		t.Errorf("executable content = %q, want %q", destination.Bytes(), executable)
	}
}

func TestExtractExecutableChoosesArchiveFormat(t *testing.T) {
	executable := []byte("managed executable")
	tarPath := "prometheus-3.13.2.linux-amd64/prometheus"
	zipPath := "alertmanager-0.34.0.windows-arm64/alertmanager.exe"
	tests := []struct {
		name      string
		component componentName
		url       string
		archive   []byte
	}{
		{
			name:      "tar.gz",
			component: prometheusComponent,
			url:       "https://example.test/prometheus-3.13.2.linux-amd64.tar.gz",
			archive: makeTestTarGz(t, []testTarEntry{
				{name: tarPath, content: executable},
			}),
		},
		{
			name:      "ZIP",
			component: alertmanagerComponent,
			url:       "https://example.test/alertmanager-0.34.0.windows-arm64.zip",
			archive: makeTestZip(t, []testZipEntry{
				{name: zipPath, content: executable},
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var destination bytes.Buffer
			artifact := componentArtifact{
				URL:  test.url,
				Size: int64(len(test.archive)),
			}

			err := extractExecutable(
				&destination,
				bytes.NewReader(test.archive),
				test.component,
				artifact,
			)
			if err != nil {
				t.Fatalf("extract executable: %v", err)
			}

			if !bytes.Equal(destination.Bytes(), executable) {
				t.Errorf(
					"executable content = %q, want %q",
					destination.Bytes(),
					executable,
				)
			}
		})
	}
}

func TestExtractExecutableToTemporaryFileReturnsCompleteFile(t *testing.T) {
	executable := []byte("prometheus executable")
	executablePath := "prometheus-3.13.2.linux-amd64/prometheus"
	archive := makeTestTarGz(t, []testTarEntry{
		{name: executablePath, content: executable},
	})
	artifact := componentArtifact{
		URL:  "https://example.test/prometheus-3.13.2.linux-amd64.tar.gz",
		Size: int64(len(archive)),
	}
	directory := t.TempDir()

	temporaryPath, err := extractExecutableToTemporaryFile(
		directory,
		bytes.NewReader(archive),
		prometheusComponent,
		artifact,
	)
	if err != nil {
		t.Fatalf("extract executable to temporary file: %v", err)
	}

	if filepath.Dir(temporaryPath) != directory {
		t.Errorf(
			"temporary file directory = %q, want %q",
			filepath.Dir(temporaryPath),
			directory,
		)
	}
	if !strings.HasSuffix(temporaryPath, ".partial") {
		t.Errorf("temporary file path = %q, want .partial suffix", temporaryPath)
	}
	extracted, err := os.ReadFile(temporaryPath) //nolint:gosec // path was created inside t.TempDir
	if err != nil {
		t.Fatalf("read temporary executable: %v", err)
	}
	if !bytes.Equal(extracted, executable) {
		t.Errorf("temporary executable = %q, want %q", extracted, executable)
	}
}

func makeTestZip(t *testing.T, entries []testZipEntry) []byte {
	t.Helper()

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	for _, entry := range entries {
		entryWriter, err := zipWriter.Create(entry.name)
		if err != nil {
			t.Fatalf("create ZIP entry: %v", err)
		}
		if _, err := entryWriter.Write(entry.content); err != nil {
			t.Fatalf("write ZIP content: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close ZIP writer: %v", err)
	}

	return archive.Bytes()
}
