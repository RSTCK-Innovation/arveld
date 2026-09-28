package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ResetPassword atomically replaces the administrator password and revokes sessions.
func (store *Store) ResetPassword(ctx context.Context, password string) error {
	return store.resetPassword(ctx, password, "")
}

// ChangePasswordParams contains the current and replacement password, preserved exactly.
type ChangePasswordParams struct {
	CurrentPassword string
	Password        string
}

// ValidateChangePassword checks inexpensive input rules before HTTP admission.
func ValidateChangePassword(params ChangePasswordParams) error {
	if params.CurrentPassword == "" || params.Password == params.CurrentPassword {
		return errors.New("current password is required and replacement must differ")
	}
	return ValidatePassword(params.Password)
}

// ChangePassword verifies the password against the request's account snapshot,
// then replaces credentials and revokes sessions only if that snapshot is current.
func (store *Store) ChangePassword(ctx context.Context, administrator Administrator, params ChangePasswordParams) error {
	if err := ValidateChangePassword(params); err != nil {
		return err
	}
	if administrator.PasswordHash == "" {
		return ErrCredentialsChanged
	}
	match, err := VerifyPassword(params.CurrentPassword, administrator.PasswordHash)
	if err != nil {
		return err
	}
	if !match {
		return ErrInvalidCredentials
	}
	return store.resetPassword(ctx, params.Password, administrator.PasswordHash)
}

func (store *Store) resetPassword(ctx context.Context, password, verifiedHash string) (returnErr error) {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back password reset: %w", err))
		}
	}()
	result, err := tx.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = ?
		WHERE id = 1 AND (? = '' OR password_hash = ?)
	`, hash, time.Now().UnixNano(), verifiedHash, verifiedHash)
	if err != nil {
		return fmt.Errorf("replace administrator password: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated administrators: %w", err)
	}
	if count == 0 {
		if verifiedHash != "" {
			return ErrCredentialsChanged
		}
		return ErrAdministratorNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return fmt.Errorf("revoke administrator sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}
