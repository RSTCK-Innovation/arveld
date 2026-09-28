// Package database owns the controller's SQLite connection.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // Register the SQLite driver with database/sql.
)

const driverName = "sqlite"

const sqliteBusyTimeout = 5 * time.Second

// open creates a single-connection SQLite pool for dataSourceName.
func open(ctx context.Context, dataSourceName string) (*sql.DB, error) {
	database, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := database.PingContext(ctx); err != nil {
		pingErr := fmt.Errorf("ping SQLite database: %w", err)

		if err := database.Close(); err != nil {
			return nil, errors.Join(
				pingErr,
				fmt.Errorf("close SQLite database after failed ping: %w", err),
			)
		}

		return nil, pingErr
	}

	return database, nil
}

// OpenFile creates the database directory and opens a SQLite file.
func OpenFile(
	ctx context.Context,
	path string,
) (*sql.DB, error) {
	return openFile(ctx, path, true)
}

// OpenExistingFile opens an existing SQLite file without creating it or its directory.
func OpenExistingFile(ctx context.Context, path string) (*sql.DB, error) {
	return openFile(ctx, path, false)
}

func openFile(ctx context.Context, path string, create bool) (*sql.DB, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve absolute database path %s: %w",
			path,
			err,
		)
	}

	directory := filepath.Dir(absolutePath)

	if create {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory %s: %w", directory, err)
		}
	}

	query := url.Values{}
	if !create {
		query.Set("mode", "rw")
	}
	// Acquire the write lock before reading a transaction's snapshot.
	query.Set("_txlock", "immediate")
	query.Add(
		"_pragma",
		fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeout.Milliseconds()),
	)
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "foreign_keys(ON)")

	dataSourceURL := url.URL{
		Scheme:   "file",
		Path:     absolutePath,
		RawQuery: query.Encode(),
	}

	return open(ctx, dataSourceURL.String())
}
