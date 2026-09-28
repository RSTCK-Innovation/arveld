package components

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type stalledDownloadTransport struct{}

func (stalledDownloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(stalledDownloadBody(func([]byte) (int, error) {
			<-request.Context().Done()
			return 0, fmt.Errorf("stalled download: %w", request.Context().Err())
		})),
	}, nil
}

type stalledDownloadBody func([]byte) (int, error)

func (body stalledDownloadBody) Read(content []byte) (int, error) {
	return body(content)
}

func TestDownloadBoundsStalledResponseBody(t *testing.T) {
	for _, timeout := range []time.Duration{2 * time.Minute, 7 * time.Second, 4 * time.Minute} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			installer := newInstaller(t.TempDir(), &http.Client{Transport: stalledDownloadTransport{}}, nil, timeout)
			start := time.Now()
			_, err := installer.ensure(ctx, prometheusComponent)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("download error = %v, want deadline exceeded", err)
			}
			if elapsed := time.Since(start); elapsed != min(timeout, 3*time.Minute) {
				t.Errorf("download duration = %s, want %s", elapsed, min(timeout, 3*time.Minute))
			}
		})
	}
}

const testArchiveSHA256 = "7134e0887d2a169c1e8571c1ca5d47be5e8c291a884edb4603cafa20157c10d2"

func TestCopyVerifiedArtifactCopiesMatchingContent(t *testing.T) {
	content := []byte("prometheus archive")
	artifact := componentArtifact{
		SHA256: testArchiveSHA256,
		Size:   int64(len(content)),
	}
	var destination bytes.Buffer

	err := copyVerifiedArtifact(&destination, bytes.NewReader(content), artifact)
	if err != nil {
		t.Fatalf("copy verified artifact: %v", err)
	}

	if !bytes.Equal(destination.Bytes(), content) {
		t.Errorf("copied content = %q, want %q", destination.Bytes(), content)
	}
}

func TestCopyVerifiedArtifactRejectsUnexpectedSize(t *testing.T) {
	content := []byte("prometheus archive")
	artifact := componentArtifact{
		SHA256: testArchiveSHA256,
		Size:   int64(len(content) - 1),
	}

	err := copyVerifiedArtifact(&bytes.Buffer{}, bytes.NewReader(content), artifact)
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("error = %v, want size error", err)
	}
}

func TestCopyVerifiedArtifactRejectsUnexpectedSHA256(t *testing.T) {
	content := []byte("prometheus archive")
	artifact := componentArtifact{
		SHA256: strings.Repeat("0", 64),
		Size:   int64(len(content)),
	}

	err := copyVerifiedArtifact(&bytes.Buffer{}, bytes.NewReader(content), artifact)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("error = %v, want SHA-256 error", err)
	}
}

func TestCopyVerifiedArtifactRejectsInvalidSHA256(t *testing.T) {
	content := []byte("prometheus archive")
	artifact := componentArtifact{
		SHA256: "00",
		Size:   int64(len(content)),
	}

	err := copyVerifiedArtifact(&bytes.Buffer{}, bytes.NewReader(content), artifact)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 is invalid") {
		t.Fatalf("error = %v, want invalid SHA-256 error", err)
	}
}

func TestDownloadVerifiedArtifactCopiesHTTPResponse(t *testing.T) {
	content := []byte("prometheus archive")
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if _, err := response.Write(content); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()

	artifact := componentArtifact{
		URL:    server.URL,
		SHA256: testArchiveSHA256,
		Size:   int64(len(content)),
	}
	var destination bytes.Buffer

	err := downloadVerifiedArtifact(
		context.Background(),
		server.Client(),
		&destination,
		artifact,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("download verified artifact: %v", err)
	}

	if !bytes.Equal(destination.Bytes(), content) {
		t.Errorf("downloaded content = %q, want %q", destination.Bytes(), content)
	}
}

func TestDownloadArtifactToTemporaryFileReturnsVerifiedFile(t *testing.T) {
	content := []byte("prometheus archive")
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if _, err := response.Write(content); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()

	directory := t.TempDir()
	artifact := componentArtifact{
		URL:    server.URL,
		SHA256: testArchiveSHA256,
		Size:   int64(len(content)),
	}

	path, err := downloadArtifactToTemporaryFile(
		context.Background(),
		server.Client(),
		directory,
		artifact,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("download artifact to temporary file: %v", err)
	}

	if filepath.Dir(path) != directory {
		t.Errorf("temporary file directory = %q, want %q", filepath.Dir(path), directory)
	}
	downloaded, err := os.ReadFile(path) //nolint:gosec // path was created inside t.TempDir
	if err != nil {
		t.Fatalf("read temporary file: %v", err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Errorf("temporary file content = %q, want %q", downloaded, content)
	}
}

func TestDownloadArtifactToTemporaryFileRemovesRejectedFile(t *testing.T) {
	content := []byte("prometheus archive")
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if _, err := response.Write(content); err != nil {
				t.Errorf("write HTTP response: %v", err)
			}
		},
	))
	defer server.Close()

	directory := t.TempDir()
	artifact := componentArtifact{
		URL:    server.URL,
		SHA256: strings.Repeat("0", 64),
		Size:   int64(len(content)),
	}

	_, err := downloadArtifactToTemporaryFile(
		context.Background(),
		server.Client(),
		directory,
		artifact,
		2*time.Minute,
	)
	if err == nil {
		t.Fatal("download error = nil, want rejected artifact error")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read temporary directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temporary directory contains %d entries, want none", len(entries))
	}
}
