package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func sessionCredentials(t *testing.T, accounts *auth.Store) auth.VerifiedCredentials {
	t.Helper()
	account, err := accounts.Administrator(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := accounts.Authenticate(t.Context(), account.Email, "original password for testing")
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func sessionDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func TestSessionStorePersistsBrowserSessions(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "sessions.db")
	db := testutil.OpenDatabase(t, path)
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(ctx, auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	verified := sessionCredentials(t, accounts)
	store := auth.NewSessionStore(db)
	if got, found, err := store.Find(ctx, "missing"); err != nil || found || got != (auth.Session{}) {
		t.Fatalf("missing session = %+v, %t, %v", got, found, err)
	}
	started := time.Now()
	session, err := store.Rotate(ctx, "", verified)
	if err != nil {
		t.Fatal(err)
	}
	if session.Token == "" || !session.Administrator || session.ExpiresAt.Before(started.Add(auth.SessionLifetime)) || session.ExpiresAt.After(time.Now().Add(auth.SessionLifetime)) {
		t.Fatalf("created session = %+v", session)
	}
	var digest string
	var data []byte
	if err := db.QueryRowContext(ctx, "SELECT token,data FROM sessions").Scan(&digest, &data); err != nil {
		t.Fatal(err)
	}
	if digest != sessionDigest(session.Token) || string(data) != `{"administrator_id":1}` {
		t.Fatal("storage must contain the token digest and identity only")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	store = auth.NewSessionStore(db)
	if got, found, err := store.Find(ctx, session.Token); err != nil || !found || !got.Administrator || got.Token != session.Token || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("reopened session = %+v, %t, %v", got, found, err)
	}
	other, err := store.Rotate(ctx, "", verified)
	if err != nil {
		t.Fatal(err)
	}
	if other.Token == session.Token {
		t.Fatal("independent sessions reused a token")
	}
	for range 2 {
		if err := store.Delete(ctx, other.Token); err != nil {
			t.Fatal(err)
		}
	}
	if got, found, err := store.Find(ctx, other.Token); err != nil || found || got != (auth.Session{}) {
		t.Fatalf("deleted session = %+v, %t, %v", got, found, err)
	}
	if _, found, err := store.Find(ctx, session.Token); err != nil || !found {
		t.Fatalf("unrelated session = %t, %v", found, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.Find(canceled, session.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
	if got, err := store.Rotate(canceled, "", verified); !errors.Is(err, context.Canceled) || got.Token != "" {
		t.Fatalf("canceled rotation = %+v, %v", got, err)
	}
	if err := store.Delete(canceled, session.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled deletion = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Find(ctx, session.Token); err == nil || found {
		t.Fatalf("closed read = %t, %v", found, err)
	}
	if got, err := store.Rotate(ctx, "", verified); err == nil || got.Token != "" {
		t.Fatalf("closed rotation = %+v, %v", got, err)
	}
	if err := store.Delete(ctx, session.Token); err == nil {
		t.Fatal("closed deletion succeeded")
	}
}

func TestPasswordChangeRejectsSessionsFromPreviousCredentials(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "sessions.db")
	db := testutil.OpenDatabase(t, path)
	accounts := auth.NewStore(db)
	const password = "original password for testing"
	if err := accounts.CreateAdministrator(ctx, auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: password}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := accounts.Administrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	verified := sessionCredentials(t, accounts)
	// Reset through a second connection, even to the same plaintext password.
	other := testutil.OpenDatabase(t, path)
	if err := auth.NewStore(other).ResetPassword(ctx, password); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessionStore(db)
	for _, credentials := range []auth.VerifiedCredentials{verified, {}} {
		session, err := sessions.Rotate(ctx, "", credentials)
		if !errors.Is(err, auth.ErrCredentialsChanged) || session.Token != "" {
			t.Fatalf("stale rotation = %+v, %v", session, err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale session count = %d, %v", count, err)
	}
	if err := accounts.ChangePassword(ctx, snapshot, auth.ChangePasswordParams{CurrentPassword: password, Password: "another replacement password"}); !errors.Is(err, auth.ErrCredentialsChanged) {
		t.Fatalf("stale password change = %v", err)
	}
	if _, err := sessions.Rotate(ctx, "", sessionCredentials(t, accounts)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRotationRollsBackWhenPreviousTokenCannotBeDeleted(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "rotation.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(ctx, auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	verified := sessionCredentials(t, accounts)
	store := auth.NewSessionStore(db)
	previous, err := store.Rotate(ctx, "", verified)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_rotation BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'deletion failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Rotate(ctx, previous.Token, verified); err == nil || got.Token != "" {
		t.Fatalf("failed rotation returned %+v, %v", got, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback session count = %d, %v", count, err)
	}
	if _, found, err := store.Find(ctx, previous.Token); err != nil || !found {
		t.Fatalf("previous session lost = %t, %v", found, err)
	}
}

func TestSessionExpirationAndCleanupBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "expiry.db"))
		store := auth.NewSessionStore(db)
		now := time.Now()
		for _, row := range []struct {
			token  string
			expiry time.Time
		}{
			{"expired", now.Add(-time.Nanosecond)}, {"at-deadline", now}, {"active", now.Add(time.Nanosecond)},
		} {
			if _, err := db.ExecContext(t.Context(), "INSERT INTO sessions (token,data,expiry_ns) VALUES (?,?,?)", sessionDigest(row.token), []byte(`{"administrator_id":1}`), row.expiry.UnixNano()); err != nil {
				t.Fatal(err)
			}
		}
		for _, token := range []string{"expired", "at-deadline"} {
			if data, found, err := store.Find(t.Context(), token); err != nil || found || data != (auth.Session{}) {
				t.Fatalf("expired session %s = %+v, %t, %v", token, data, found, err)
			}
		}
		if data, found, err := store.Find(t.Context(), "active"); err != nil || !found || !data.Administrator {
			t.Fatalf("active session = %+v, %t, %v", data, found, err)
		}
		if removed, err := store.DeleteExpired(t.Context()); err != nil || removed != 2 {
			t.Fatalf("cleanup = %d, %v; want 2", removed, err)
		}
		if removed, err := store.DeleteExpired(t.Context()); err != nil || removed != 0 {
			t.Fatalf("repeated cleanup = %d, %v", removed, err)
		}
		time.Sleep(time.Nanosecond)
		if removed, err := store.DeleteExpired(t.Context()); err != nil || removed != 1 {
			t.Fatalf("boundary cleanup = %d, %v; want 1", removed, err)
		}
	})
}

func TestSessionCleanupErrorsPreserveRows(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "cleanup.db"))
	store := auth.NewSessionStore(db)
	if _, err := db.ExecContext(t.Context(), "INSERT INTO sessions (token,data,expiry_ns) VALUES ('expired',?,0)", []byte("identity")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_cleanup BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'cleanup failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.DeleteExpired(t.Context()); err == nil || removed != 0 {
		t.Fatalf("failed cleanup = %d, %v", removed, err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER reject_cleanup"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.DeleteExpired(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cleanup = %v", err)
	}
	if removed, err := store.DeleteExpired(t.Context()); err != nil || removed != 1 {
		t.Fatalf("cleanup recovery = %d, %v", removed, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.DeleteExpired(t.Context()); err == nil || removed != 0 {
		t.Fatalf("closed cleanup = %d, %v", removed, err)
	}
}

func TestSessionRotationReplacesOnlyPreviousToken(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "rotation.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	verified := sessionCredentials(t, accounts)
	store := auth.NewSessionStore(db)
	previous, err := store.Rotate(t.Context(), "", verified)
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := store.Rotate(t.Context(), "", verified)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := store.Rotate(t.Context(), previous.Token, verified)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Token == replacement.Token {
		t.Fatal("rotation reused the previous token")
	}
	if _, found, err := store.Find(t.Context(), previous.Token); err != nil || found {
		t.Fatalf("previous session = %t, %v", found, err)
	}
	for _, session := range []auth.Session{replacement, unrelated} {
		got, found, err := store.Find(t.Context(), session.Token)
		if err != nil || !found || !got.Administrator || !got.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatalf("session = %+v, %t, %v", got, found, err)
		}
	}
}
