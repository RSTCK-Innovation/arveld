package components

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// installer ensures that locked upstream components exist on disk.
type installer struct {
	dataDirectory   string
	downloadTimeout time.Duration
	client          *http.Client
	logger          *slog.Logger
}

// newInstaller creates an installer rooted in dataDirectory.
// downloadTimeout must be positive and limits each archive transfer, including its body.
func newInstaller(
	dataDirectory string,
	client *http.Client,
	logger *slog.Logger,
	downloadTimeout time.Duration,
) *installer {
	if client == nil {
		client = http.DefaultClient
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &installer{
		dataDirectory:   dataDirectory,
		client:          client,
		logger:          logger,
		downloadTimeout: downloadTimeout,
	}
}

// ensure returns the installed executable path for a locked component.
func (installer *installer) ensure(
	ctx context.Context,
	name componentName,
) (string, error) {
	release, err := releaseForCurrentPlatform(name)
	if err != nil {
		return "", fmt.Errorf("resolve locked component release: %w", err)
	}

	installedPath, err := installer.installRelease(ctx, name, release)
	if err != nil {
		return "", fmt.Errorf("ensure component %q: %w", name, err)
	}

	return installedPath, nil
}

func (installer *installer) installRelease(
	ctx context.Context,
	name componentName,
	release componentRelease,
) (string, error) {
	versionDirectory, destinationPath, err := prepareComponentDirectory(
		installer.dataDirectory,
		name,
		release,
	)
	if err != nil {
		return "", fmt.Errorf("prepare component installation: %w", err)
	}
	installedInfo, statErr := os.Lstat(destinationPath)
	switch {
	case statErr == nil:
		if !installedInfo.Mode().IsRegular() {
			return "", fmt.Errorf(
				"installed component path %q is not a regular file",
				destinationPath,
			)
		}
		valid, err := cachedExecutableValid(destinationPath, installedInfo)
		if err != nil {
			return "", fmt.Errorf("validate cached component: %w", err)
		}
		if valid {
			return destinationPath, nil
		}
		installer.logger.WarnContext(
			ctx,
			"cached managed component is invalid; reinstalling",
			"component",
			name,
			"version",
			release.Version,
		)
		if err := removeCachedExecutable(destinationPath); err != nil {
			return "", fmt.Errorf("remove invalid cached component: %w", err)
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return "", fmt.Errorf("inspect installed component: %w", statErr)
	}

	installer.logger.InfoContext(
		ctx,
		"downloading managed component",
		"component",
		name,
		"version",
		release.Version,
	)
	archivePath, err := downloadArtifactToTemporaryFile(
		ctx,
		installer.client,
		versionDirectory,
		release.Artifact,
		installer.downloadTimeout,
	)
	if err != nil {
		return "", fmt.Errorf("download component release: %w", err)
	}
	defer func() {
		_ = os.Remove(archivePath) //nolint:errcheck // downloaded archives are disposable cleanup
	}()

	archiveFile, err := os.Open(archivePath) //nolint:gosec // path was created by os.CreateTemp in versionDirectory
	if err != nil {
		return "", fmt.Errorf("open downloaded component archive: %w", err)
	}
	temporaryPath, extractErr := extractExecutableToTemporaryFile(
		versionDirectory,
		archiveFile,
		name,
		release.Artifact,
	)
	closeErr := archiveFile.Close()
	if extractErr != nil {
		return "", fmt.Errorf("extract component release: %w", extractErr)
	}
	defer func() {
		_ = os.Remove(temporaryPath) //nolint:errcheck // published files no longer exist at this path
	}()
	if closeErr != nil {
		return "", fmt.Errorf("close downloaded component archive: %w", closeErr)
	}

	if err := publishExecutable(temporaryPath, destinationPath); err != nil {
		return "", fmt.Errorf("publish component release: %w", err)
	}
	if err := writeExecutableChecksum(destinationPath); err != nil {
		return "", fmt.Errorf("write installed component checksum: %w", err)
	}
	installer.logger.InfoContext(
		ctx,
		"managed component installed",
		"component",
		name,
		"version",
		release.Version,
	)

	return destinationPath, nil
}
