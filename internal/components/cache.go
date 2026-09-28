package components

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const executableChecksumSuffix = ".sha256"

func cachedExecutableValid(path string, info os.FileInfo) (bool, error) {
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return false, nil
	}

	expectedBytes, err := os.ReadFile(path + executableChecksumSuffix) //nolint:gosec // path belongs to the managed component directory
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read executable checksum: %w", err)
	}
	expected := string(expectedBytes)
	if len(expected) != sha256.Size*2 {
		return false, nil
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return false, nil //nolint:nilerr // malformed metadata invalidates the cache and triggers reinstall
	}

	actual, err := executableSHA256(path)
	if err != nil {
		return false, err
	}

	return actual == expected, nil
}

func removeCachedExecutable(path string) error {
	if err := os.Remove(path + executableChecksumSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove executable checksum: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove executable: %w", err)
	}

	return nil
}

func writeExecutableChecksum(path string) error {
	digest, err := executableSHA256(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(
		path+executableChecksumSuffix,
		[]byte(digest),
		0o600,
	); err != nil {
		return fmt.Errorf("write executable checksum: %w", err)
	}

	return nil
}

func executableSHA256(path string) (digest string, returnErr error) {
	file, err := os.Open(path) //nolint:gosec // path belongs to the managed component directory
	if err != nil {
		return "", fmt.Errorf("open executable for checksum: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close executable after checksum: %w", err),
			)
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("calculate executable checksum: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func publishExecutable(temporaryPath string, destinationPath string) error {
	if err := os.Chmod(temporaryPath, 0o755); err != nil { //nolint:gosec // managed component must be executable
		return fmt.Errorf("make temporary executable runnable: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("publish executable: %w", err)
	}

	return nil
}

func prepareComponentDirectory(
	dataDirectory string,
	name componentName,
	release componentRelease,
) (string, string, error) {
	versionDirectory := filepath.Join(
		dataDirectory,
		"components",
		string(name),
		release.Version,
	)
	if err := os.MkdirAll(versionDirectory, 0o750); err != nil {
		return "", "", fmt.Errorf("create component version directory: %w", err)
	}

	executableName := string(name)
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}

	return versionDirectory, filepath.Join(versionDirectory, executableName), nil
}
