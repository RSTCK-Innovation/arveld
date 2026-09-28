package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migrate updates db to the current schema version.
func Migrate(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db, embeddedMigrations)
}

func migrate(ctx context.Context, db *sql.DB, files fs.FS) error {
	migrationPaths, err := readMigrationPaths(files)
	if err != nil {
		return fmt.Errorf("read migration paths: %w", err)
	}
	if err := validateMigrationPaths(migrationPaths); err != nil {
		return fmt.Errorf("validate migration paths: %w", err)
	}

	version, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if version > len(migrationPaths) {
		return fmt.Errorf("unsupported schema version: %d", version)
	}

	for version < len(migrationPaths) {
		path := migrationPaths[version]
		statement, err := readMigration(files, path)
		if err != nil {
			return err
		}

		nextVersion := version + 1
		if err := applyMigration(
			ctx,
			db,
			statement,
			nextVersion,
		); err != nil {
			return err
		}

		version = nextVersion
	}

	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	row := db.QueryRowContext(ctx, "PRAGMA user_version")

	if err := row.Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}

	return version, nil
}

func readMigrationPaths(files fs.FS) ([]string, error) {
	dirEntries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return nil, fmt.Errorf(
			"read migration dir: %w",
			err,
		)
	}

	paths := make([]string, 0, len(dirEntries))
	for _, item := range dirEntries {
		if item.IsDir() || path.Ext(item.Name()) != ".sql" {
			continue
		}

		paths = append(paths, path.Join("migrations", item.Name()))
	}

	return paths, nil
}

func validateMigrationPaths(paths []string) error {
	for index, migrationPath := range paths {
		wantPrefix := fmt.Sprintf("%04d_", index+1)
		name := path.Base(migrationPath)
		if !strings.HasPrefix(name, wantPrefix) {
			return fmt.Errorf(
				"migration %q has invalid sequence: want prefix %q",
				migrationPath,
				wantPrefix,
			)
		}
	}

	return nil
}

func readMigration(files fs.FS, path string) (string, error) {
	contents, err := fs.ReadFile(files, path)
	if err != nil {
		return "", fmt.Errorf(
			"read migration %s: %w",
			path,
			err,
		)
	}

	return string(contents), nil
}

func applyMigration(
	ctx context.Context,
	db *sql.DB,
	statement string,
	version int,
) (returnErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", version, err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back migration: %w", err))
		}
	}()

	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("execute migration %d: %w", version, err)
	}

	versionStatement := fmt.Sprintf("PRAGMA user_version = %d", version)
	if _, err := tx.ExecContext(ctx, versionStatement); err != nil {
		return fmt.Errorf("set schema version to %d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", version, err)
	}

	return nil
}
