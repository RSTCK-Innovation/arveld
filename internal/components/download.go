package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func downloadArtifactToTemporaryFile(
	ctx context.Context,
	client *http.Client,
	directory string,
	artifact componentArtifact,
	downloadTimeout time.Duration,
) (string, error) {
	temporaryFile, err := os.CreateTemp(
		directory,
		".arveld-component-*.partial",
	)
	if err != nil {
		return "", fmt.Errorf("create temporary artifact file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	keepTemporaryFile := false
	defer func() {
		if keepTemporaryFile {
			return
		}
		_ = temporaryFile.Close()    //nolint:errcheck // best-effort cleanup after the primary error
		_ = os.Remove(temporaryPath) //nolint:errcheck // best-effort cleanup preserves the primary error
	}()

	if err := downloadVerifiedArtifact(
		ctx,
		client,
		temporaryFile,
		artifact,
		downloadTimeout,
	); err != nil {
		return "", fmt.Errorf("download artifact to temporary file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary artifact file: %w", err)
	}

	keepTemporaryFile = true
	return temporaryPath, nil
}

func downloadVerifiedArtifact(
	ctx context.Context,
	client *http.Client,
	destination io.Writer,
	artifact componentArtifact,
	downloadTimeout time.Duration,
) error {
	if downloadTimeout <= 0 {
		return errors.New("component download timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		artifact.URL,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create artifact download request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer func() {
		_ = response.Body.Close() //nolint:errcheck // copy reports body read errors; close releases the connection
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download artifact: unexpected HTTP status %s", response.Status)
	}
	if err := copyVerifiedArtifact(destination, response.Body, artifact); err != nil {
		return fmt.Errorf("verify downloaded artifact: %w", err)
	}

	return nil
}

func copyVerifiedArtifact(
	destination io.Writer,
	source io.Reader,
	artifact componentArtifact,
) error {
	if artifact.Size <= 0 {
		return fmt.Errorf("artifact size must be positive: %d", artifact.Size)
	}

	expectedSHA256, err := hex.DecodeString(artifact.SHA256)
	if err != nil {
		return fmt.Errorf("artifact SHA-256 is invalid: %w", err)
	}
	if len(expectedSHA256) != sha256.Size {
		return fmt.Errorf(
			"artifact SHA-256 is invalid: decoded length is %d bytes, want %d",
			len(expectedSHA256),
			sha256.Size,
		)
	}

	hasher := sha256.New()
	bytesWritten, err := io.Copy(
		io.MultiWriter(destination, hasher),
		io.LimitReader(source, artifact.Size+1),
	)
	if err != nil {
		return fmt.Errorf("copy artifact: %w", err)
	}
	if bytesWritten != artifact.Size {
		return fmt.Errorf(
			"artifact size is %d bytes, want %d",
			bytesWritten,
			artifact.Size,
		)
	}
	if !bytes.Equal(hasher.Sum(nil), expectedSHA256) {
		return errors.New("artifact SHA-256 does not match manifest")
	}

	return nil
}
