package components

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
)

const maxExtractedExecutableSize int64 = 512 * 1024 * 1024

type archiveReader interface {
	io.Reader
	io.ReaderAt
}

func extractExecutable(
	destination io.Writer,
	source archiveReader,
	name componentName,
	artifact componentArtifact,
) error {
	artifactURL, err := url.Parse(artifact.URL)
	if err != nil {
		return fmt.Errorf("parse artifact URL: %w", err)
	}
	archiveName := path.Base(artifactURL.Path)

	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		expectedPath := path.Join(
			strings.TrimSuffix(archiveName, ".tar.gz"),
			string(name),
		)
		return extractTarGzExecutable(destination, source, expectedPath)
	case strings.HasSuffix(archiveName, ".zip"):
		expectedPath := path.Join(
			strings.TrimSuffix(archiveName, ".zip"),
			string(name)+".exe",
		)
		return extractZipExecutable(
			destination,
			source,
			artifact.Size,
			expectedPath,
		)
	default:
		return fmt.Errorf("artifact URL %q has unsupported archive format", artifact.URL)
	}
}

func extractZipExecutable(
	destination io.Writer,
	source io.ReaderAt,
	archiveSize int64,
	expectedPath string,
) error {
	zipReader, err := zip.NewReader(source, archiveSize)
	if err != nil {
		return fmt.Errorf("open ZIP archive: %w", err)
	}

	for _, entry := range zipReader.File {
		if entry.Name != expectedPath {
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("archive executable %q is not a regular file", expectedPath)
		}
		if entry.UncompressedSize64 > uint64(maxExtractedExecutableSize) {
			return fmt.Errorf(
				"archive executable %q exceeds the %d-byte size limit",
				expectedPath,
				maxExtractedExecutableSize,
			)
		}

		entryReader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open executable %q: %w", expectedPath, err)
		}
		bytesWritten, copyErr := io.Copy(
			destination,
			io.LimitReader(entryReader, maxExtractedExecutableSize+1),
		)
		closeErr := entryReader.Close()
		if copyErr != nil {
			return fmt.Errorf("extract executable %q: %w", expectedPath, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close executable %q: %w", expectedPath, closeErr)
		}
		if bytesWritten > maxExtractedExecutableSize {
			return fmt.Errorf(
				"archive executable %q exceeds the %d-byte size limit",
				expectedPath,
				maxExtractedExecutableSize,
			)
		}
		expectedSize := int64(entry.UncompressedSize64)
		if bytesWritten != expectedSize {
			return fmt.Errorf(
				"executable %q contains %d bytes, want %d",
				expectedPath,
				bytesWritten,
				expectedSize,
			)
		}

		return nil
	}

	return fmt.Errorf("archive does not contain executable %q", expectedPath)
}

func extractTarGzExecutable(
	destination io.Writer,
	source io.Reader,
	expectedPath string,
) error {
	gzipReader, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer func() {
		_ = gzipReader.Close() //nolint:errcheck // the source owner handles its own close error
	}()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("archive does not contain executable %q", expectedPath)
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		if header.Name != expectedPath {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive executable %q is not a regular file", expectedPath)
		}
		if header.Size > maxExtractedExecutableSize {
			return fmt.Errorf(
				"archive executable %q exceeds the %d-byte size limit",
				expectedPath,
				maxExtractedExecutableSize,
			)
		}
		if _, err := io.CopyN(destination, tarReader, header.Size); err != nil {
			return fmt.Errorf("extract executable %q: %w", expectedPath, err)
		}

		return nil
	}
}

func extractExecutableToTemporaryFile(
	directory string,
	source archiveReader,
	name componentName,
	artifact componentArtifact,
) (string, error) {
	temporaryFile, err := os.CreateTemp(
		directory,
		".arveld-component-executable-*.partial",
	)
	if err != nil {
		return "", fmt.Errorf("create temporary executable file: %w", err)
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

	if err := extractExecutable(
		temporaryFile,
		source,
		name,
		artifact,
	); err != nil {
		return "", fmt.Errorf("extract executable to temporary file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary executable file: %w", err)
	}

	keepTemporaryFile = true
	return temporaryPath, nil
}
