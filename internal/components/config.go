package components

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeComponentConfig(
	dataDirectory string,
	name componentName,
	content string,
) (string, error) {
	configDirectory := filepath.Join(dataDirectory, "config")
	if err := os.MkdirAll(configDirectory, 0o750); err != nil {
		return "", fmt.Errorf("create component config directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(
		configDirectory,
		fmt.Sprintf(".arveld-%s-*.partial", name),
	)
	if err != nil {
		return "", fmt.Errorf("create temporary component %q config: %w", name, err)
	}
	temporaryPath := temporaryFile.Name()
	defer func() {
		_ = temporaryFile.Close()    //nolint:errcheck // best-effort cleanup after the primary operation
		_ = os.Remove(temporaryPath) //nolint:errcheck // renamed files no longer exist at this path
	}()

	if _, err := temporaryFile.WriteString(content); err != nil {
		return "", fmt.Errorf("write temporary component %q config: %w", name, err)
	}
	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary component %q config: %w", name, err)
	}

	configPath := filepath.Join(configDirectory, string(name)+".yml")
	if err := os.Rename(temporaryPath, configPath); err != nil {
		return "", fmt.Errorf("publish component %q config: %w", name, err)
	}

	return configPath, nil
}
