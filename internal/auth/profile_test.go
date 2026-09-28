package auth_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestValidateUpdateProfile(t *testing.T) {
	for _, test := range []struct {
		name  string
		input auth.UpdateProfileParams
		want  auth.UpdateProfileParams
	}{
		{"normalization", auth.UpdateProfileParams{Name: " \tRobin Smith 🔧\n", Email: " ROBIN@EXAMPLE.COM "}, auth.UpdateProfileParams{Name: "Robin Smith 🔧", Email: "robin@example.com"}},
		{"two runes", auth.UpdateProfileParams{Name: "é🔧", Email: "robin@example.com"}, auth.UpdateProfileParams{Name: "é🔧", Email: "robin@example.com"}},
		{"sixty runes", auth.UpdateProfileParams{Name: strings.Repeat("🔧", 60), Email: "robin@example.com"}, auth.UpdateProfileParams{Name: strings.Repeat("🔧", 60), Email: "robin@example.com"}},
		{"email boundary", auth.UpdateProfileParams{Name: "Robin", Email: strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)}, auth.UpdateProfileParams{Name: "Robin", Email: strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := auth.ValidateUpdateProfile(test.input)
			if err != nil || got != test.want {
				t.Fatalf("ValidateUpdateProfile() = %+v, %v; want %+v", got, err, test.want)
			}
		})
	}
	for _, test := range []struct{ name, nameValue, email string }{
		{"missing name", "", "robin@example.com"},
		{"short trimmed name", " L ", "robin@example.com"},
		{"long Unicode name", strings.Repeat("🔧", 61), "robin@example.com"},
		{"invalid UTF-8", "R\xffbin", "robin@example.com"},
		{"missing email", "Robin", ""},
		{"invalid email", "Robin", "invalid"},
		{"display name email", "Robin", "Robin <robin@example.com>"},
		{"multiple addresses", "Robin", "robin@example.com, other@example.com"},
		{"long email", "Robin", strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 62)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := auth.ValidateUpdateProfile(auth.UpdateProfileParams{Name: test.nameValue, Email: test.email})
			if err == nil || got != (auth.UpdateProfileParams{}) {
				t.Fatalf("rejected profile = %+v, %v; want empty result and error", got, err)
			}
		})
	}
}

func TestUpdateProfilePreservesCredentialsAndSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	db := testutil.OpenDatabase(t, path)
	store := auth.NewStore(db)
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	original, err := store.Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.NewSessionStore(db).Rotate(t.Context(), "", sessionCredentials(t, store))
	if err != nil {
		t.Fatal(err)
	}
	params := auth.UpdateProfileParams{Name: "Robin Smith 🔧", Email: "robin@example.com"}
	started := time.Now()
	if err := store.UpdateProfile(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	updated, err := auth.NewStore(db).Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != params.Name || updated.Email != params.Email || updated.PasswordHash != original.PasswordHash || !updated.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("profile update changed unexpected fields: %+v", updated)
	}
	if updated.UpdatedAt.Before(started) || updated.UpdatedAt.After(time.Now()) || !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatal("profile update did not record its timestamp")
	}
	if got, found, err := auth.NewSessionStore(db).Find(t.Context(), session.Token); err != nil || !found || !got.Administrator || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("existing session = %+v, %t, %v", got, found, err)
	}
}

func TestUpdateProfileErrorsPreserveAccount(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "profile.db"))
	store := auth.NewStore(db)
	params := auth.UpdateProfileParams{Name: "Robin", Email: "robin@example.com"}
	if err := store.UpdateProfile(t.Context(), params); !errors.Is(err, auth.ErrAdministratorNotFound) {
		t.Fatalf("missing account error = %v", err)
	}
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	original, err := store.Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_profile_update BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'profile write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProfile(t.Context(), params); err == nil {
		t.Fatal("profile update ignored SQL failure")
	}
	current, err := store.Administrator(t.Context())
	if err != nil || current != original {
		t.Fatalf("failed update changed account: %+v, %v", current, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProfile(t.Context(), params); err == nil {
		t.Fatal("profile update ignored closed database")
	}
}
