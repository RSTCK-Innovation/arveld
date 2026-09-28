package auth

import (
	"context"
	"fmt"
	"time"
)

// CreateAdministratorParams contains validated account creation input.
// Password is hashed before persistence and is never stored as plaintext.
type CreateAdministratorParams struct {
	Name     string
	Email    string
	Password string
}

// ValidateCreateAdministrator normalizes the profile and validates creation input.
// Password is preserved exactly, including leading and trailing whitespace.
func ValidateCreateAdministrator(params CreateAdministratorParams) (CreateAdministratorParams, error) {
	profile, err := ValidateUpdateProfile(UpdateProfileParams{Name: params.Name, Email: params.Email})
	if err != nil {
		return CreateAdministratorParams{}, err
	}
	params.Name, params.Email = profile.Name, profile.Email
	if err := ValidatePassword(params.Password); err != nil {
		return CreateAdministratorParams{}, err
	}

	return params, nil
}

// CreateAdministrator stores the first administrator without replacing an existing one.
func (store *Store) CreateAdministrator(ctx context.Context, params CreateAdministratorParams) error {
	required, err := store.NeedsSetup(ctx)
	if err != nil {
		return err
	}
	if !required {
		return ErrAdministratorExists
	}

	hash, err := HashPassword(params.Password)
	if err != nil {
		return fmt.Errorf("create administrator: %w", err)
	}

	now := time.Now().UnixNano()
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO users (id, name, email, password_hash, created_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING
	`, params.Name, params.Email, hash, now, now)
	if err != nil {
		return fmt.Errorf("insert administrator: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read administrator insert count: %w", err)
	}
	if count == 0 {
		return ErrAdministratorExists
	}

	return nil
}
