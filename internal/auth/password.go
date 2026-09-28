// Package auth authenticates the local Arveld administrator.
package auth

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

// ValidatePassword checks the shared password policy without modifying the input.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 15 || length > 128 {
		return errors.New("password must contain 15 to 128 characters")
	}
	return nil
}

// HashPassword creates a salted Argon2id hash for persistent password storage.
func HashPassword(password string) (string, error) {
	// OWASP's minimum Argon2id profile: 19 MiB, two iterations, one lane.
	// https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
	params := &argon2id.Params{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return hash, nil
}

// VerifyPassword checks a password against a stored hash produced by HashPassword.
// A mismatch returns false with no error; an unreadable hash returns an error.
// The hash must come from Arveld storage, never directly from an HTTP client.
func VerifyPassword(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}

	return match, nil
}
