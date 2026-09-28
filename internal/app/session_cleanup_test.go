package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

func TestSessionCleanupRemovesExpiredRowsAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
		store := auth.NewSessionStore(db)
		commit := func(token string, expiry time.Time) {
			t.Helper()
			if _, err := db.ExecContext(ctx, "INSERT INTO sessions (token, data, expiry_ns) VALUES (?, ?, ?)", token, []byte("session data"), expiry.UnixNano()); err != nil {
				t.Fatalf("commit session: %v", err)
			}
		}
		assertExpiredRows := func(want int64) {
			t.Helper()
			// Observe the loop's persisted effect without performing another sweep.
			var remaining int64
			err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE expiry_ns <= ?", time.Now().UnixNano()).Scan(&remaining)
			if err != nil || remaining != want {
				t.Fatalf("remaining expired rows = %d, %v, want %d, nil", remaining, err, want)
			}
			var data []byte
			err = db.QueryRowContext(ctx, "SELECT data FROM sessions WHERE token = 'active'").Scan(&data)
			if err != nil || string(data) != "session data" {
				t.Fatalf("active session after cleanup = %q, %v", data, err)
			}
		}
		commit("active", time.Now().Add(time.Hour))
		commit("startup", time.Now().Add(-time.Hour))
		stop := startSessionCleanup(ctx, store, testLogger())
		defer stop()
		synctest.Wait()
		assertExpiredRows(0)

		commit("later", time.Now().Add(time.Minute))
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		assertExpiredRows(1)
		time.Sleep(time.Minute)
		synctest.Wait()
		assertExpiredRows(0)

		// One failed sweep must not stop subsequent attempts.
		commit("retry", time.Now().Add(-time.Minute))
		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER reject_cleanup BEFORE DELETE ON sessions
			BEGIN SELECT RAISE(ABORT, 'test cleanup failure'); END;
		`); err != nil {
			t.Fatalf("install cleanup failure: %v", err)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assertExpiredRows(1)
		if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_cleanup"); err != nil {
			t.Fatalf("remove cleanup failure: %v", err)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assertExpiredRows(0)

		// Stop must cancel an in-flight operation waiting for the sole SQL connection.
		commit("after-stop", time.Now().Add(-time.Minute))
		connection, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("reserve database connection: %v", err)
		}
		defer func() {
			if err := connection.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
				t.Errorf("close reserved connection: %v", err)
			}
		}()
		waits := db.Stats().WaitCount
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if db.Stats().WaitCount == waits {
			t.Fatal("scheduled cleanup did not wait for SQLite")
		}
		stop()
		if err := connection.Close(); err != nil {
			t.Fatalf("release database connection: %v", err)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assertExpiredRows(1)
	})
}
