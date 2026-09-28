package agentauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrKeyNotFound indicates that the requested agent key does not exist.
var ErrKeyNotFound = errors.New("agent key not found")

// ErrInvalidKey means the token is unknown or revoked.
var ErrInvalidKey = errors.New("invalid agent key")

// AgentKey contains public metadata for an agent key, never a token or its hash.
type AgentKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}

// CreateKeyParams describes the name of a new agent key.
type CreateKeyParams struct {
	Name string
}

// ValidateCreateKey trims the name and checks its Unicode length.
func ValidateCreateKey(params CreateKeyParams) (CreateKeyParams, error) {
	params.Name = strings.TrimSpace(params.Name)
	length := utf8.RuneCountInString(params.Name)
	if !utf8.ValidString(params.Name) || length < 2 || length > 80 {
		return CreateKeyParams{}, errors.New("agent key name must contain 2 to 80 characters")
	}
	return params, nil
}

// CreateKey persists a validated key and returns its public metadata and complete token.
// Only the token's SHA-256 digest is stored; a failed insert never returns a token.
func (store *Store) CreateKey(ctx context.Context, params CreateKeyParams) (AgentKey, string, error) {
	now := time.Now().UTC()
	key := AgentKey{
		ID:        rand.Text(),
		Name:      params.Name,
		CreatedAt: now,
	}
	key.Prefix = "arv_agent_" + key.ID
	// The secret is generated independently of the public identifier.
	token := key.Prefix + "_" + rand.Text()
	hash := sha256.Sum256([]byte(token))
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO agent_keys (id, name, prefix, token_hash, created_at_ns)
		VALUES (?, ?, ?, ?, ?)
	`, key.ID, key.Name, key.Prefix, hash[:], key.CreatedAt.UnixNano())
	if err != nil {
		return AgentKey{}, "", fmt.Errorf("insert agent key: %w", err)
	}
	return key, token, nil
}

// AuthenticateKey returns the public identifier of an active key matching the complete token.
func (store *Store) AuthenticateKey(ctx context.Context, token string) (string, error) {
	hash := sha256.Sum256([]byte(token))
	var id string
	err := store.db.QueryRowContext(ctx, `
		SELECT id FROM agent_keys
		WHERE token_hash = ? AND revoked_at_ns IS NULL
	`, hash[:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidKey
	}
	if err != nil {
		return "", fmt.Errorf("authenticate agent key: %w", err)
	}
	return id, nil
}

// CheckKeyActive rechecks a previously authenticated key by its public identifier.
// It does not authenticate a token and must not be used to admit new connections.
func (store *Store) CheckKeyActive(ctx context.Context, id string) error {
	var active bool
	err := store.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM agent_keys WHERE id = ? AND revoked_at_ns IS NULL
		)
	`, id).Scan(&active)
	if err != nil {
		return fmt.Errorf("check active agent key: %w", err)
	}
	if !active {
		return ErrInvalidKey
	}
	return nil
}

// ListKeys returns public metadata for every key, newest first with ID as the tie-breaker.
func (store *Store) ListKeys(ctx context.Context) (keys []AgentKey, returnErr error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, name, prefix, created_at_ns, revoked_at_ns
		FROM agent_keys ORDER BY created_at_ns DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query agent keys: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close agent key rows: %w", err))
		}
	}()

	keys = make([]AgentKey, 0)
	for rows.Next() {
		var key AgentKey
		var createdAt int64
		var revokedAt sql.NullInt64
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &createdAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan agent key: %w", err)
		}
		key.CreatedAt = time.Unix(0, createdAt).UTC()
		if revokedAt.Valid {
			timestamp := time.Unix(0, revokedAt.Int64).UTC()
			key.RevokedAt = &timestamp
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent keys: %w", err)
	}
	return keys, nil
}

// RevokeKey records the first revocation date and retains the key's metadata.
func (store *Store) RevokeKey(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE agent_keys SET revoked_at_ns = COALESCE(revoked_at_ns, ?) WHERE id = ?
	`, time.Now().UTC().UnixNano(), id)
	if err != nil {
		return fmt.Errorf("revoke agent key: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count revoked agent keys: %w", err)
	}
	if count == 0 {
		return ErrKeyNotFound
	}
	return nil
}
