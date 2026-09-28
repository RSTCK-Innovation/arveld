package auth

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

// ErrAPIKeyNotFound indicates that the requested key does not exist.
var ErrAPIKeyNotFound = errors.New("API key not found")

// ErrInvalidAPIKey means the token is unknown, expired, or revoked.
var ErrInvalidAPIKey = errors.New("invalid API key")

// APIKey contains public management-key metadata, never a token or its hash.
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Permission string     `json:"permission"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

// CreateAPIKeyParams describes the name, permission, and validity of a new API key.
type CreateAPIKeyParams struct {
	Name        string
	Permission  string
	ExpiresDays *int // nil means no expiration.
}

// AuthenticateAPIKey returns the permission of a matching active key.
func (store *Store) AuthenticateAPIKey(ctx context.Context, token string) (string, error) {
	hash := sha256.Sum256([]byte(token))
	var permission string
	err := store.db.QueryRowContext(ctx, `
		SELECT permission FROM api_keys
		WHERE token_hash = ? AND revoked_at_ns IS NULL
		  AND (expires_at_ns IS NULL OR expires_at_ns > ?)
	`, hash[:], time.Now().UTC().UnixNano()).Scan(&permission)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidAPIKey
	}
	if err != nil {
		return "", fmt.Errorf("authenticate API key: %w", err)
	}
	return permission, nil
}

// ListAPIKeys returns public metadata for every key, newest first with ID as the tie-breaker.
func (store *Store) ListAPIKeys(ctx context.Context) (keys []APIKey, returnErr error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, name, prefix, permission, created_at_ns, expires_at_ns, revoked_at_ns
		FROM api_keys ORDER BY created_at_ns DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query API keys: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close API key rows: %w", err))
		}
	}()

	keys = make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		var createdAt int64
		var expiresAt, revokedAt sql.NullInt64
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.Permission, &createdAt, &expiresAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		key.CreatedAt = time.Unix(0, createdAt).UTC()
		if expiresAt.Valid {
			timestamp := time.Unix(0, expiresAt.Int64).UTC()
			key.ExpiresAt = &timestamp
		}
		if revokedAt.Valid {
			timestamp := time.Unix(0, revokedAt.Int64).UTC()
			key.RevokedAt = &timestamp
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return keys, nil
}

// RevokeAPIKey records the first revocation date and retains the key's metadata.
func (store *Store) RevokeAPIKey(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, `
		UPDATE api_keys SET revoked_at_ns = COALESCE(revoked_at_ns, ?) WHERE id = ?
	`, time.Now().UTC().UnixNano(), id)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count revoked API keys: %w", err)
	}
	if count == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// ValidateCreateAPIKey normalizes the name and validates supported permissions and durations.
func ValidateCreateAPIKey(params CreateAPIKeyParams) (CreateAPIKeyParams, error) {
	params.Name = strings.TrimSpace(params.Name)
	length := utf8.RuneCountInString(params.Name)
	if !utf8.ValidString(params.Name) || length < 2 || length > 80 {
		return CreateAPIKeyParams{}, errors.New("API key name must contain 2 to 80 characters")
	}
	if params.Permission != "read" && params.Permission != "write" {
		return CreateAPIKeyParams{}, errors.New("API key permission must be read or write")
	}
	if params.ExpiresDays != nil {
		switch *params.ExpiresDays {
		case 1, 7, 30, 90, 365:
		default:
			return CreateAPIKeyParams{}, errors.New("API key validity must be 1, 7, 30, 90, 365 days, or null")
		}
	}
	return params, nil
}

// CreateAPIKey persists a validated key and returns its token only after a successful insert.
// Only the token's SHA-256 digest is stored; the plaintext cannot be retrieved later.
func (store *Store) CreateAPIKey(ctx context.Context, params CreateAPIKeyParams) (APIKey, string, error) {
	now := time.Now().UTC()
	key := APIKey{
		ID: rand.Text(), Name: params.Name, Permission: params.Permission,
		CreatedAt: now,
	}
	var expiresAtNS *int64
	if params.ExpiresDays != nil {
		expiresAt := now.Add(time.Duration(*params.ExpiresDays) * 24 * time.Hour)
		key.ExpiresAt = &expiresAt
		nanoseconds := expiresAt.UnixNano()
		expiresAtNS = &nanoseconds
	}
	key.Prefix = "arv_" + key.ID
	// The secret is generated independently of the public identifier.
	token := key.Prefix + "_" + rand.Text()
	hash := sha256.Sum256([]byte(token))
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, name, prefix, permission, token_hash, created_at_ns, expires_at_ns)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, key.ID, key.Name, key.Prefix, key.Permission, hash[:], key.CreatedAt.UnixNano(), expiresAtNS)
	if err != nil {
		return APIKey{}, "", fmt.Errorf("insert API key: %w", err)
	}
	return key, token, nil
}
