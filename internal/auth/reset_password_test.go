package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestPasswordReplacementRevokesSessionsAtomically(t *testing.T) {
	for _, mode := range []string{"change", "reset"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "password.db")
			db := testutil.OpenDatabase(t, path)
			store := auth.NewStore(db)
			const originalPassword = "original password for testing"
			const replacement = "  replacement password 🔑  "
			if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: originalPassword}); err != nil {
				t.Fatal(err)
			}
			original, err := store.Administrator(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			sessions := auth.NewSessionStore(db)
			verified := sessionCredentials(t, store)
			tokens := make([]string, 0, 2)
			for range 2 {
				session, err := sessions.Rotate(t.Context(), "", verified)
				if err != nil {
					t.Fatal(err)
				}
				tokens = append(tokens, session.Token)
			}
			replace := func() error {
				if mode == "change" {
					return store.ChangePassword(t.Context(), original, auth.ChangePasswordParams{CurrentPassword: originalPassword, Password: replacement})
				}
				return store.ResetPassword(t.Context(), replacement)
			}
			// Fail each transaction write separately; neither may publish partial state.
			for _, table := range []string{"users", "sessions"} {
				event := "UPDATE"
				if table == "sessions" {
					event = "DELETE"
				}
				if _, err := db.ExecContext(t.Context(), "CREATE TRIGGER reject_password_write BEFORE "+event+" ON "+table+" BEGIN SELECT RAISE(ABORT, 'password transaction failure'); END;"); err != nil {
					t.Fatal(err)
				}
				if err := replace(); err == nil {
					t.Fatal("password replacement ignored SQL failure")
				}
				current, err := store.Administrator(t.Context())
				if err != nil || current != original {
					t.Fatalf("failed replacement changed account: %+v, %v", current, err)
				}
				for _, token := range tokens {
					if data, found, err := sessions.Find(t.Context(), token); err != nil || !found || !data.Administrator {
						t.Fatalf("rollback lost session %s: %+v, %t, %v", token, data, found, err)
					}
				}
				if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_password_write"); err != nil {
					t.Fatal(err)
				}
			}
			started := time.Now()
			if err := replace(); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = testutil.OpenDatabase(t, path)
			current, err := auth.NewStore(db).Administrator(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if current.Name != original.Name || current.Email != original.Email || !current.CreatedAt.Equal(original.CreatedAt) || !current.UpdatedAt.After(original.UpdatedAt) || current.UpdatedAt.Before(started) || current.UpdatedAt.After(time.Now()) {
				t.Fatal("replacement changed profile or failed to update timestamp")
			}
			for _, test := range []struct {
				password string
				match    bool
			}{{replacement, true}, {strings.TrimSpace(replacement), false}, {originalPassword, false}} {
				match, err := auth.VerifyPassword(test.password, current.PasswordHash)
				if err != nil || match != test.match {
					t.Fatalf("replacement verification = %t, %v; want %t", match, err, test.match)
				}
			}
			var count int
			if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 0 {
				t.Fatalf("sessions after replacement = %d, %v", count, err)
			}
		})
	}
}

func TestPasswordReplacementRejectsMissingAccountAndStaleHash(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "password.db"))
	store := auth.NewStore(db)
	const password = "replacement password for testing"
	if err := store.ResetPassword(t.Context(), password); !errors.Is(err, auth.ErrAdministratorNotFound) {
		t.Fatalf("missing account = %v", err)
	}
	if err := store.ChangePassword(t.Context(), auth.Administrator{}, auth.ChangePasswordParams{CurrentPassword: "old password for testing", Password: password}); !errors.Is(err, auth.ErrCredentialsChanged) {
		t.Fatalf("missing verified account = %v", err)
	}
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: password}); err != nil {
		t.Fatal(err)
	}
	original, err := store.Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ChangePassword(t.Context(), auth.Administrator{}, auth.ChangePasswordParams{CurrentPassword: password, Password: "different replacement password"}); !errors.Is(err, auth.ErrCredentialsChanged) {
		t.Fatalf("empty snapshot = %v", err)
	}
	if err := store.ChangePassword(t.Context(), original, auth.ChangePasswordParams{CurrentPassword: "incorrect password", Password: "different replacement password"}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong current password = %v", err)
	}
	if err := store.ResetPassword(t.Context(), "too short"); err == nil {
		t.Fatal("invalid reset password accepted")
	}
	if err := store.ChangePassword(t.Context(), original, auth.ChangePasswordParams{CurrentPassword: password, Password: "too short"}); err == nil {
		t.Fatal("invalid changed password accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.ResetPassword(ctx, password); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reset = %v", err)
	}
	current, err := store.Administrator(t.Context())
	if err != nil || current != original {
		t.Fatalf("rejected replacement changed account: %+v, %v", current, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetPassword(t.Context(), password); err == nil {
		t.Fatal("closed database reset succeeded")
	}
}
