package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestStoreCreatesOnlyOneAdministrator(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "arveld.db")
	firstDB := testutil.OpenDatabase(t, path)
	secondDB := testutil.OpenDatabase(t, path)

	if _, err := NewStore(firstDB).Administrator(ctx); !errors.Is(err, ErrAdministratorNotFound) {
		t.Fatalf("Administrator() error = %v, want ErrAdministratorNotFound", err)
	}

	params := CreateAdministratorParams{
		Name:     "Camille",
		Email:    "camille@example.com",
		Password: "a long password for the local administrator",
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []*sql.DB{firstDB, secondDB} {
		go func() {
			<-start
			results <- NewStore(db).CreateAdministrator(ctx, params)
		}()
	}
	close(start)

	var created, rejected int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrAdministratorExists):
			rejected++
		default:
			t.Errorf("CreateAdministrator() unexpected error: %v", err)
		}
	}
	if created != 1 || rejected != 1 {
		t.Fatalf("creation outcomes = %d created, %d rejected, want 1 of each", created, rejected)
	}

	// Reopen the file after both writers have closed to verify durable storage.
	if err := firstDB.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	if err := secondDB.Close(); err != nil {
		t.Fatalf("close second database: %v", err)
	}
	store := NewStore(testutil.OpenDatabase(t, path))
	if err := store.CreateAdministrator(ctx, CreateAdministratorParams{
		Name:     "Another administrator",
		Email:    "another@example.com",
		Password: "another long password that must not replace the original",
	}); !errors.Is(err, ErrAdministratorExists) {
		t.Fatalf("later CreateAdministrator() error = %v, want ErrAdministratorExists", err)
	}

	administrator, err := store.Administrator(ctx)
	if err != nil {
		t.Fatalf("Administrator() error = %v", err)
	}
	if administrator.Name != params.Name || administrator.Email != params.Email {
		t.Error("Administrator() did not preserve the original profile")
	}
	if administrator.PasswordHash == "" || administrator.PasswordHash == params.Password {
		t.Fatal("Administrator() must contain a password hash, not the original password")
	}
	match, err := VerifyPassword(params.Password, administrator.PasswordHash)
	if err != nil {
		t.Fatalf("verify stored password: %v", err)
	}
	if !match {
		t.Error("stored password hash does not match the original password")
	}
}

func TestAdministratorTracksCreationAndUpdates(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "timestamps.db"))
	store := NewStore(db)
	started := time.Now()
	if err := store.CreateAdministrator(ctx, CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "original password for testing",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := store.Administrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedAt.Before(started) || created.CreatedAt.After(time.Now()) || !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Fatal("creation must record the same current timestamp in both fields")
	}
	previous := created
	for _, change := range []struct {
		name  string
		apply func() error
	}{
		{"profile", func() error {
			return store.UpdateProfile(ctx, UpdateProfileParams{Name: "Robin", Email: "robin@example.com"})
		}},
		{"password change", func() error {
			return store.ChangePassword(ctx, created, ChangePasswordParams{CurrentPassword: "original password for testing", Password: "changed password for testing"})
		}},
		{"password reset", func() error {
			return store.ResetPassword(ctx, "reset password for testing")
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			started := time.Now()
			if err := change.apply(); err != nil {
				t.Fatal(err)
			}
			current, err := store.Administrator(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !current.CreatedAt.Equal(created.CreatedAt) {
				t.Fatal("an update changed the creation timestamp")
			}
			if !current.UpdatedAt.After(previous.UpdatedAt) || current.UpdatedAt.Before(started) || current.UpdatedAt.After(time.Now()) {
				t.Fatal("an update did not record its current timestamp")
			}
			previous = current
		})
	}
	if err := store.ChangePassword(ctx, created, ChangePasswordParams{CurrentPassword: "original password for testing", Password: "rejected password for testing"}); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale password change = %v, want changed credentials", err)
	}
	current, err := store.Administrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current != previous {
		t.Fatal("a rejected password change modified the account or its timestamps")
	}
}
