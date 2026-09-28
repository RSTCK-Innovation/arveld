package auth

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidCredentials reports a failed account or password verification.
var ErrInvalidCredentials = errors.New("invalid administrator credentials")

// VerifiedCredentials carries a successfully verified password snapshot.
// Only auth can construct it; session rotation checks that it is still current.
type VerifiedCredentials struct{ passwordHash string }

// Authenticate verifies both fields, including the password when the email differs.
func (store *Store) Authenticate(ctx context.Context, email, password string) (VerifiedCredentials, error) {
	administrator, err := store.Administrator(ctx)
	if errors.Is(err, ErrAdministratorNotFound) {
		return VerifiedCredentials{}, ErrInvalidCredentials
	}
	if err != nil {
		return VerifiedCredentials{}, err
	}
	match, err := VerifyPassword(password, administrator.PasswordHash)
	if err != nil {
		return VerifiedCredentials{}, err
	}
	if !match || strings.ToLower(strings.TrimSpace(email)) != administrator.Email {
		return VerifiedCredentials{}, ErrInvalidCredentials
	}
	return VerifiedCredentials{passwordHash: administrator.PasswordHash}, nil
}
