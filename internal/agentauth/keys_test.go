package agentauth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestCreateKeyPersistsDigestAndPublicMetadata(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	authStore := auth.NewStore(db)
	if err := authStore.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	params := agentauth.CreateKeyParams{Name: "Production agents"}

	started := time.Now()
	key, token, err := store.CreateKey(t.Context(), params)
	if err != nil {
		t.Fatalf("create agent key: %v", err)
	}
	if key.ID == "" || key.Name != params.Name || key.Prefix == "" {
		t.Fatal("created key must have an identifier, the requested name, and a public prefix")
	}
	if key.CreatedAt.Before(started) || key.CreatedAt.After(time.Now()) {
		t.Fatal("key creation timestamp must fall within the creation call")
	}
	if !strings.HasPrefix(token, key.Prefix+"_") || len(token) <= len(key.Prefix)+1 {
		t.Fatal("returned token must contain a secret after its public prefix")
	}

	// Read the storage boundary directly: a successful response alone cannot
	// establish that the credential was persisted as a digest.
	var storedName, storedPrefix string
	var storedHash []byte
	var storedCreatedAt int64
	err = db.QueryRowContext(t.Context(), `
		SELECT name, prefix, token_hash, created_at_ns
		FROM agent_keys WHERE id = ?
	`, key.ID).Scan(&storedName, &storedPrefix, &storedHash, &storedCreatedAt)
	if err != nil {
		t.Fatalf("read persisted agent key: %v", err)
	}
	if storedName != key.Name || storedPrefix != key.Prefix || storedCreatedAt != key.CreatedAt.UnixNano() {
		t.Fatal("persisted public metadata must match the created key")
	}
	if bytes.Equal(storedHash, []byte(token)) {
		t.Fatal("stored credential must not contain the plaintext token")
	}
	wantHash := sha256.Sum256([]byte(token))
	if !bytes.Equal(storedHash, wantHash[:]) {
		t.Fatal("stored credential must be the SHA-256 digest of the complete token")
	}
}

func TestListKeysReturnsPublicMetadataNewestFirst(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	keys, err := store.ListKeys(t.Context())
	if err != nil {
		t.Fatalf("list keys before creation: %v", err)
	}
	if keys == nil || len(keys) != 0 {
		t.Fatal("listing without keys must return a non-nil empty slice")
	}

	// Fixed timestamps and identifiers make chronological ordering and ties
	// independent of clock resolution or randomly generated identifiers.
	fixtures := []struct {
		id          string
		name        string
		createdAtNS int64
	}{
		{id: "z", name: "Older agents", createdAtNS: 10},
		{id: "b", name: "Beta agents", createdAtNS: 20},
		{id: "a", name: "Alpha agents", createdAtNS: 20},
	}
	for _, fixture := range fixtures {
		hash := sha256.Sum256([]byte("test-only-secret-" + fixture.id))
		if _, err := db.ExecContext(t.Context(), `
			INSERT INTO agent_keys (id, name, prefix, token_hash, created_at_ns)
			VALUES (?, ?, ?, ?, ?)
		`, fixture.id, fixture.name, "arv_agent_"+fixture.id, hash[:], fixture.createdAtNS); err != nil {
			t.Fatalf("insert agent key fixture: %v", err)
		}
	}

	keys, err = store.ListKeys(t.Context())
	if err != nil {
		t.Fatalf("list persisted keys: %v", err)
	}
	want := []agentauth.AgentKey{
		{ID: "a", Name: "Alpha agents", Prefix: "arv_agent_a", CreatedAt: time.Unix(0, 20).UTC()},
		{ID: "b", Name: "Beta agents", Prefix: "arv_agent_b", CreatedAt: time.Unix(0, 20).UTC()},
		{ID: "z", Name: "Older agents", Prefix: "arv_agent_z", CreatedAt: time.Unix(0, 10).UTC()},
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("listed public metadata = %+v, want %+v", keys, want)
	}
}

func TestRevokeKeyPersistsRevocationAndPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.db")
	db := testutil.OpenDatabase(t, path)
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	target, _, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Retired agents"})
	if err != nil {
		t.Fatalf("create key to revoke: %v", err)
	}
	if _, _, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Active agents"}); err != nil {
		t.Fatalf("create unrelated key: %v", err)
	}
	before, err := store.ListKeys(t.Context())
	if err != nil {
		t.Fatalf("list keys before revocation: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("keys before revocation = %d, want 2", len(before))
	}
	for _, key := range before {
		if key.RevokedAt != nil {
			t.Fatal("new keys must not have a revocation date")
		}
	}

	started := time.Now()
	if err := store.RevokeKey(t.Context(), target.ID); err != nil {
		t.Fatalf("revoke agent key: %v", err)
	}
	finished := time.Now()

	// Reopen the same database to verify that revocation is persisted.
	if err := db.Close(); err != nil {
		t.Fatalf("close database before reopening: %v", err)
	}
	db = testutil.OpenDatabase(t, path)
	keys, err := agentauth.NewStore(db).ListKeys(t.Context())
	if err != nil {
		t.Fatalf("list keys after reopening: %v", err)
	}
	if len(keys) != len(before) {
		t.Fatalf("keys after revocation = %d, want %d retained keys", len(keys), len(before))
	}
	for index, key := range keys {
		want := before[index]
		if key.ID == target.ID {
			if key.RevokedAt == nil {
				t.Fatal("revoked key must retain its revocation date after reopening")
			}
			if key.RevokedAt.Before(started) || key.RevokedAt.After(finished) {
				t.Fatal("revocation date must fall within the revocation call")
			}
			// The selected key changes only its revocation date.
			want.RevokedAt = key.RevokedAt
		}
		if !reflect.DeepEqual(key, want) {
			t.Fatalf("key metadata after revocation = %+v, want %+v", key, want)
		}
	}
}

func TestRevokeKeyPreservesFirstRevocationDate(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	key, _, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Retired agents"})
	if err != nil {
		t.Fatalf("create agent key: %v", err)
	}
	// Prepare a historical key so the test does not depend on clock resolution.
	firstRevocation := time.Unix(123, 456).UTC()
	if _, err := db.ExecContext(t.Context(), "UPDATE agent_keys SET created_at_ns = ?, revoked_at_ns = ? WHERE id = ?", firstRevocation.Add(-time.Second).UnixNano(), firstRevocation.UnixNano(), key.ID); err != nil {
		t.Fatalf("prepare previously revoked key: %v", err)
	}

	if err := store.RevokeKey(t.Context(), key.ID); err != nil {
		t.Fatalf("repeat revocation: %v", err)
	}
	keys, err := store.ListKeys(t.Context())
	if err != nil {
		t.Fatalf("list revoked key: %v", err)
	}
	if len(keys) != 1 || keys[0].RevokedAt == nil || !keys[0].RevokedAt.Equal(firstRevocation) {
		t.Fatal("repeated revocation must retain the original revocation date")
	}
}

func TestRevokeKeyRejectsUnknownKey(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	store := agentauth.NewStore(db)
	if err := store.RevokeKey(t.Context(), "missing-key"); !errors.Is(err, agentauth.ErrKeyNotFound) {
		t.Fatalf("unknown key revocation = %v, want ErrKeyNotFound", err)
	}
}

func TestAuthenticateKeyIdentifiesPersistedActiveKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.db")
	db := testutil.OpenDatabase(t, path)
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	first, firstToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Production agents"})
	if err != nil {
		t.Fatalf("create first key: %v", err)
	}
	second, secondToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Staging agents"})
	if err != nil {
		t.Fatalf("create second key: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database before reopening: %v", err)
	}
	db = testutil.OpenDatabase(t, path)
	store = agentauth.NewStore(db)

	if id, err := store.AuthenticateKey(t.Context(), firstToken); err != nil || id != first.ID {
		t.Fatalf("first key identity = %q, %v, want %q", id, err, first.ID)
	}
	if id, err := store.AuthenticateKey(t.Context(), secondToken); err != nil || id != second.ID {
		t.Fatalf("second key identity = %q, %v, want %q", id, err, second.ID)
	}
}

func TestAuthenticateKeyRejectsUnknownAndRevokedKeys(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	revoked, revokedToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Retired agents"})
	if err != nil {
		t.Fatalf("create key to revoke: %v", err)
	}
	active, activeToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Active agents"})
	if err != nil {
		t.Fatalf("create unrelated key: %v", err)
	}
	_, managementToken, err := accounts.CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Management", Permission: "write"})
	if err != nil {
		t.Fatalf("create management API key: %v", err)
	}
	if id, err := store.AuthenticateKey(t.Context(), revokedToken); err != nil || id != revoked.ID {
		t.Fatalf("authentication before revocation = %q, %v, want %q", id, err, revoked.ID)
	}
	if err := agentauth.NewStore(db).RevokeKey(t.Context(), revoked.ID); err != nil {
		t.Fatalf("revoke previously authenticated key: %v", err)
	}

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "unknown token", token: "unknown-agent-token"},
		{name: "modified secret", token: activeToken + "changed"},
		{name: "public identifier", token: active.ID},
		{name: "public prefix", token: active.Prefix},
		{name: "revoked token", token: revokedToken},
		{name: "management API key", token: managementToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			if id, err := store.AuthenticateKey(t.Context(), test.token); id != "" || !errors.Is(err, agentauth.ErrInvalidKey) {
				t.Fatalf("rejected authentication = %q, %v, want empty identity and ErrInvalidKey", id, err)
			}
		})
	}
	if id, err := store.AuthenticateKey(t.Context(), activeToken); err != nil || id != active.ID {
		t.Fatalf("unrelated key identity = %q, %v, want %q", id, err, active.ID)
	}
}

func TestCheckKeyActiveUsesCurrentPersistedState(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	if err := auth.NewStore(db).CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	key, token, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Active agents"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.AuthenticateKey(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CheckKeyActive(t.Context(), id); err != nil {
		t.Fatalf("active key recheck: %v", err)
	}
	if err := store.CheckKeyActive(t.Context(), "missing-key"); !errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("missing key recheck = %v, want ErrInvalidKey", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.CheckKeyActive(ctx, id); !errors.Is(err, context.Canceled) || errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("canceled key recheck = %v, want context.Canceled", err)
	}
	if err := store.RevokeKey(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckKeyActive(t.Context(), id); !errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("revoked key recheck = %v, want ErrInvalidKey", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckKeyActive(t.Context(), id); err == nil || errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("closed database recheck = %v, want a storage error", err)
	}
}

func TestAuthenticateKeyPreservesStorageErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	accounts := auth.NewStore(db)
	if err := accounts.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	store := agentauth.NewStore(db)
	_, token, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Active agents"})
	if err != nil {
		t.Fatalf("create agent key: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if id, err := store.AuthenticateKey(ctx, token); id != "" || !errors.Is(err, context.Canceled) || errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("canceled authentication = %q, %v, want empty identity and context.Canceled", id, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if id, err := store.AuthenticateKey(t.Context(), token); id != "" || err == nil || errors.Is(err, agentauth.ErrInvalidKey) {
		t.Fatalf("closed database authentication = %q, %v, want empty identity and a storage error", id, err)
	}
}
