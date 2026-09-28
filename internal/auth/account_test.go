package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestValidateCreateAdministrator(t *testing.T) {
	const password = "  a very long password 🔑  " //nolint:gosec // Public fixture verifies preservation of surrounding whitespace.
	got, err := auth.ValidateCreateAdministrator(auth.CreateAdministratorParams{Name: "  Camille  ", Email: " CAMILLE@EXAMPLE.COM ", Password: password})
	if err != nil || got != (auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: password}) {
		t.Fatalf("validated account = %+v, %v", got, err)
	}
	for _, test := range []struct {
		name   string
		params auth.CreateAdministratorParams
	}{
		{"empty account", auth.CreateAdministratorParams{}},
		{"short name", auth.CreateAdministratorParams{Name: "C", Email: "camille@example.com", Password: password}},
		{"long name", auth.CreateAdministratorParams{Name: strings.Repeat("🔑", 61), Email: "camille@example.com", Password: password}},
		{"invalid email", auth.CreateAdministratorParams{Name: "Camille", Email: "invalid", Password: password}},
		{"display name", auth.CreateAdministratorParams{Name: "Camille", Email: "Camille <camille@example.com>", Password: password}},
		{"long email", auth.CreateAdministratorParams{Name: "Camille", Email: strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63), Password: password}},
		{"missing password", auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com"}},
		{"short password", auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "12345678901234"}},
		{"short Unicode password", auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: strings.Repeat("🔑", 14)}},
		{"long password", auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: strings.Repeat("🔑", 129)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := auth.ValidateCreateAdministrator(test.params)
			if err == nil || got != (auth.CreateAdministratorParams{}) {
				t.Fatalf("rejected account = %+v, %v", got, err)
			}
		})
	}
}

func TestNeedsSetupAndExactPasswordPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.db")
	db := testutil.OpenDatabase(t, path)
	store := auth.NewStore(db)
	if required, err := store.NeedsSetup(t.Context()); err != nil || !required {
		t.Fatalf("empty setup = %t, %v", required, err)
	}
	const password = "  a very long password 🔑  " //nolint:gosec // Public fixture verifies preservation of surrounding whitespace.
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: password}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	store = auth.NewStore(db)
	if required, err := store.NeedsSetup(t.Context()); err != nil || required {
		t.Fatalf("persisted setup = %t, %v", required, err)
	}
	account, err := store.Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		password string
		match    bool
	}{{password, true}, {strings.TrimSpace(password), false}} {
		match, err := auth.VerifyPassword(test.password, account.PasswordHash)
		if err != nil || match != test.match {
			t.Fatalf("password preservation = %t, %v; want %t", match, err, test.match)
		}
	}
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Robin", Email: "robin@example.com", Password: "another long password"}); !errors.Is(err, auth.ErrAdministratorExists) {
		t.Fatalf("second creation = %v", err)
	}
	current, err := store.Administrator(t.Context())
	if err != nil || current != account {
		t.Fatalf("second creation changed account: %+v, %v", current, err)
	}
}

func TestAccountStoreErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "account.db"))
	store := auth.NewStore(db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.NeedsSetup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup = %v", err)
	}
	if _, err := store.Administrator(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled account = %v", err)
	}
	params := auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}
	if err := store.CreateAdministrator(ctx, params); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled creation = %v", err)
	}
	if required, err := store.NeedsSetup(t.Context()); err != nil || !required {
		t.Fatalf("failed creation initialized account: %t, %v", required, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if required, err := store.NeedsSetup(t.Context()); err == nil || required {
		t.Fatalf("closed setup = %t, %v", required, err)
	}
	if _, err := store.Administrator(t.Context()); err == nil {
		t.Fatal("closed account read succeeded")
	}
	if err := store.CreateAdministrator(t.Context(), params); err == nil {
		t.Fatal("closed account creation succeeded")
	}
}
