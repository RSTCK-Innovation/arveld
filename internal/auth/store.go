package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrCredentialsChanged means password verification used an outdated password hash.
	ErrCredentialsChanged = errors.New("administrator credentials changed")
	// ErrAdministratorExists means the instance already has an administrator.
	ErrAdministratorExists = errors.New("administrator already exists")
	// ErrAdministratorNotFound means the instance has not been initialized.
	ErrAdministratorNotFound = errors.New("administrator not found")
)

// Administrator is the stored credential record, not an HTTP response model.
type Administrator struct {
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Store owns the instance administrator and management API keys.
// The caller supplies a migrated database opened by database.OpenFile or
// OpenExistingFile and remains responsible for closing it.
type Store struct {
	db *sql.DB
}

// NewStore uses the caller's configured database.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// NeedsSetup reports whether the instance still needs its first administrator.
func (store *Store) NeedsSetup(ctx context.Context) (bool, error) {
	var required bool
	err := store.db.QueryRowContext(ctx, `
		SELECT NOT EXISTS (SELECT 1 FROM users WHERE id = 1)
	`).Scan(&required)
	if err != nil {
		return false, fmt.Errorf("read administrator setup state: %w", err)
	}

	return required, nil
}

// Administrator returns the stored account or ErrAdministratorNotFound.
func (store *Store) Administrator(ctx context.Context) (Administrator, error) {
	var administrator Administrator
	var createdAt, updatedAt int64
	err := store.db.QueryRowContext(ctx, `
		SELECT name, email, password_hash, created_at, updated_at FROM users WHERE id = 1
	`).Scan(&administrator.Name, &administrator.Email, &administrator.PasswordHash, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Administrator{}, ErrAdministratorNotFound
	}
	if err != nil {
		return Administrator{}, fmt.Errorf("read administrator: %w", err)
	}

	administrator.CreatedAt = time.Unix(0, createdAt).UTC()
	administrator.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return administrator, nil
}
