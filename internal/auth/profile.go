package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// UpdateProfileParams contains the administrator's editable profile fields.
type UpdateProfileParams struct {
	Name  string
	Email string
}

// ValidateUpdateProfile normalizes and validates the administrator's profile.
func ValidateUpdateProfile(params UpdateProfileParams) (UpdateProfileParams, error) {
	params.Name = strings.TrimSpace(params.Name)
	params.Email = strings.ToLower(strings.TrimSpace(params.Email))
	nameLength := utf8.RuneCountInString(params.Name)
	if !utf8.ValidString(params.Name) || nameLength < 2 || nameLength > 60 {
		return UpdateProfileParams{}, errors.New("name must contain 2 to 60 characters")
	}
	address, err := mail.ParseAddress(params.Email)
	if len(params.Email) > 254 || err != nil || address.Address != params.Email {
		return UpdateProfileParams{}, errors.New("email must be a single valid address")
	}
	return params, nil
}

// UpdateProfile persists a validated profile without changing the password or sessions.
func (store *Store) UpdateProfile(ctx context.Context, params UpdateProfileParams) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE users SET name = ?, email = ?, updated_at = ? WHERE id = 1
	`, params.Name, params.Email, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("update administrator profile: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated administrators: %w", err)
	}
	if count == 0 {
		return ErrAdministratorNotFound
	}
	return nil
}
