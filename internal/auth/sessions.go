package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SessionLifetime is fixed at creation; reads do not extend a session.
const SessionLifetime = 24 * time.Hour

// Session is an opaque token and its persisted identity and expiration.
// Token is returned to the browser, while only its digest is stored in SQLite.
type Session struct {
	Token         string
	ExpiresAt     time.Time
	Administrator bool
}

type sessionIdentity struct {
	AdministratorID int `json:"administrator_id"`
}

// SessionStore owns token generation, encoding and persistence in SQLite.
// The caller supplies a migrated database opened by database.OpenFile or
// OpenExistingFile and owns its closure and periodic session cleanup.
type SessionStore struct{ db *sql.DB }

// NewSessionStore uses the caller's configured database without starting cleanup.
func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

// Find accepts a raw cookie token and loads an unexpired session without renewing it.
// Missing sessions are not errors; malformed persisted identities are errors.
func (store *SessionStore) Find(ctx context.Context, token string) (Session, bool, error) {
	var data []byte
	var expiry int64
	err := store.db.QueryRowContext(ctx, `SELECT data, expiry_ns FROM sessions WHERE token = ? AND expiry_ns > ?`, hashSessionToken(token), time.Now().UnixNano()).Scan(&data, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("find session: %w", err)
	}
	var identity sessionIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return Session{}, false, fmt.Errorf("decode session identity: %w", err)
	}
	return Session{Token: token, ExpiresAt: time.Unix(0, expiry), Administrator: identity.AdministratorID == 1}, true, nil
}

// Rotate creates a session only while the verified credentials are current.
// Insertion and deletion of the previous raw token commit together, including
// across processes sharing the database. A failed write returns no new token.
func (store *SessionStore) Rotate(ctx context.Context, previous string, verified VerifiedCredentials) (session Session, returnErr error) {
	if verified.passwordHash == "" {
		return Session{}, ErrCredentialsChanged
	}
	data, err := json.Marshal(sessionIdentity{AdministratorID: 1})
	if err != nil {
		return Session{}, fmt.Errorf("encode session identity: %w", err)
	}
	next := Session{Token: rand.Text(), ExpiresAt: time.Now().Add(SessionLifetime), Administrator: true}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin session rotation: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back session rotation: %w", err))
		}
	}()
	result, err := tx.ExecContext(ctx, `
  INSERT INTO sessions (token, data, expiry_ns)
  SELECT ?, ?, ? FROM users WHERE id = 1 AND password_hash = ?
 `, hashSessionToken(next.Token), data, next.ExpiresAt.UnixNano(), verified.passwordHash)
	if err != nil {
		return Session{}, fmt.Errorf("persist session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Session{}, fmt.Errorf("count persisted sessions: %w", err)
	}
	if count == 0 {
		return Session{}, ErrCredentialsChanged
	}
	if previous != "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", hashSessionToken(previous)); err != nil {
			return Session{}, fmt.Errorf("delete previous session: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit session rotation: %w", err)
	}
	return next, nil
}

// Delete removes a raw token's session. Deleting a missing session succeeds.
func (store *SessionStore) Delete(ctx context.Context, token string) error {
	if _, err := store.db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", hashSessionToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes expired sessions and returns the number of deleted rows.
func (store *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := store.db.ExecContext(ctx, "DELETE FROM sessions WHERE expiry_ns <= ?", time.Now().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted sessions: %w", err)
	}
	return removed, nil
}

func hashSessionToken(token string) string {
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
