package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAuthenticatePreservesCredentialChecks(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "login.db"))
	store := auth.NewStore(db)
	const password = "  original password for testing  "
	if _, err := store.Authenticate(t.Context(), "camille@example.com", password); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("missing administrator = %v", err)
	}
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: password}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		email, password string
		valid           bool
	}{
		{" CAMILLE@EXAMPLE.COM ", password, true},
		{"other@example.com", password, false},
		{"camille@example.com", "original password for testing", false},
		{"camille@example.com", "wrong password", false},
	} {
		_, err := store.Authenticate(t.Context(), test.email, test.password)
		if test.valid && err != nil {
			t.Fatalf("valid credentials rejected: %v", err)
		}
		if !test.valid && !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("invalid credentials = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Authenticate(ctx, "camille@example.com", password); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled verification = %v", err)
	}
	// An unreadable stored hash must still be checked for a mismatching email.
	if _, err := db.ExecContext(t.Context(), "UPDATE users SET password_hash = 'unreadable'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(t.Context(), "other@example.com", password); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("password verification was skipped for a mismatching email: %v", err)
	}
}
