package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

const (
	defaultConfigPath                  = "data/arveld.yml"
	generatedConfigDirectoryPermission = 0o700
	generatedConfigFilePermission      = 0o600
)

// LoadConfig loads path, or loads and creates the default configuration when path is empty.
func LoadConfig(path string) (Config, error) {
	if path != "" {
		return loadConfigFile(path)
	}

	config, err := loadConfigFile(defaultConfigPath)
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	config = DefaultConfig()
	if err := writeConfigFile(defaultConfigPath, config); err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadConfigFile(defaultConfigPath)
		}

		return Config{}, fmt.Errorf("write default config: %w", err)
	}

	return config, nil
}

func loadConfigFile(path string) (config Config, returnErr error) {
	file, err := os.Open(path) //nolint:gosec // path is selected explicitly by the user
	if err != nil {
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			config = Config{}
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close config file %q: %w", path, err),
			)
		}
	}()

	config = DefaultConfig()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	if err := decoder.Decode(&config); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}

	// Verify there is not a second yaml document in the file
	var extraDocument any
	if err := decoder.Decode(&extraDocument); err == nil {
		return Config{}, fmt.Errorf("decode config file %q: multiple YAML documents are not supported", path)
	} else if !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}

	return config, nil
}

func writeConfigFile(path string, config Config) error {
	return writeConfigFileWithWriter(
		path,
		config,
		func(file *os.File, content []byte) error {
			if _, err := file.Write(content); err != nil {
				return fmt.Errorf("write content: %w", err)
			}

			return nil
		},
	)
}

func writeConfigFileWithWriter(
	path string,
	config Config,
	write func(*os.File, []byte) error,
) (returnErr error) {
	content, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), generatedConfigDirectoryPermission); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(
		filepath.Dir(path),
		"."+filepath.Base(path)+".*.partial",
	)
	if err != nil {
		return fmt.Errorf("create temporary config file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporaryFile.Close(); err != nil {
				returnErr = errors.Join(
					returnErr,
					fmt.Errorf("close temporary config file: %w", err),
				)
			}
		}
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("remove temporary config file: %w", err),
			)
		}
	}()

	if err := temporaryFile.Chmod(generatedConfigFilePermission); err != nil {
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if err := write(temporaryFile, content); err != nil {
		return fmt.Errorf("write temporary config file: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		return fmt.Errorf("sync temporary config file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary config file: %w", err)
	}
	closed = true

	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("publish config file %q: %w", path, err)
	}

	return nil
}
