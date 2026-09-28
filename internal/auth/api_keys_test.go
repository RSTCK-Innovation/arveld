package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestValidateCreateAPIKey(t *testing.T) {
	for _, permission := range []string{"read", "write"} {
		for _, days := range []int{0, 1, 7, 30, 90, 365} {
			params := auth.CreateAPIKeyParams{Name: "  Deployment 🔧  ", Permission: permission}
			if days != 0 {
				params.ExpiresDays = &days
			}
			got, err := auth.ValidateCreateAPIKey(params)
			if err != nil || got.Name != "Deployment 🔧" || got.Permission != permission || got.ExpiresDays != params.ExpiresDays {
				t.Fatalf("validated key = %+v, %v", got, err)
			}
		}
	}
	for _, name := range []string{"🔑🔑", strings.Repeat("🔑", 80)} {
		if _, err := auth.ValidateCreateAPIKey(auth.CreateAPIKeyParams{Name: name, Permission: "read"}); err != nil {
			t.Fatalf("valid Unicode boundary: %v", err)
		}
	}
	for _, name := range []string{"", " A ", strings.Repeat("🔑", 81), "D\xffployment"} {
		if got, err := auth.ValidateCreateAPIKey(auth.CreateAPIKeyParams{Name: name, Permission: "read"}); err == nil || got != (auth.CreateAPIKeyParams{}) {
			t.Fatalf("invalid name accepted: %+v, %v", got, err)
		}
	}
	for _, permission := range []string{"", "ingest", "READ", " write "} {
		if _, err := auth.ValidateCreateAPIKey(auth.CreateAPIKeyParams{Name: "Deployment", Permission: permission}); err == nil {
			t.Fatalf("invalid permission %q accepted", permission)
		}
	}
	for _, days := range []int{0, -7, 14, 2147483647} {
		if _, err := auth.ValidateCreateAPIKey(auth.CreateAPIKeyParams{Name: "Deployment", Permission: "read", ExpiresDays: &days}); err == nil {
			t.Fatalf("invalid expiry %d accepted", days)
		}
	}
}

func TestAPIKeyCreationPersistsOnlyDigestAndPublicMetadata(t *testing.T) {
	for _, days := range []int{0, 1, 7, 30, 90, 365} {
		t.Run(strconv.Itoa(days)+" days", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keys.db")
			db := testutil.OpenDatabase(t, path)
			store := auth.NewStore(db)
			if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
				t.Fatal(err)
			}
			params := auth.CreateAPIKeyParams{Name: "Deployment 🔧", Permission: "write"}
			if days != 0 {
				params.ExpiresDays = &days
			}
			started := time.Now()
			key, token, err := store.CreateAPIKey(t.Context(), params)
			if err != nil {
				t.Fatal(err)
			}
			if key.ID == "" || key.Name != params.Name || key.Permission != params.Permission || key.Prefix != "arv_"+key.ID || !strings.HasPrefix(token, key.Prefix+"_") || len(token) < len(key.Prefix)+27 || key.RevokedAt != nil {
				t.Fatal("incorrect key metadata or secret shape")
			}
			if key.CreatedAt.Before(started) || key.CreatedAt.After(time.Now()) {
				t.Fatal("incorrect creation timestamp")
			}
			if days == 0 {
				if key.ExpiresAt != nil {
					t.Fatal("perpetual key expires")
				}
			} else if key.ExpiresAt == nil || key.ExpiresAt.Sub(key.CreatedAt) != time.Duration(days)*24*time.Hour {
				t.Fatal("incorrect validity duration")
			}
			second, secondToken, err := store.CreateAPIKey(t.Context(), params)
			if err != nil {
				t.Fatal(err)
			}
			if second.ID == key.ID || strings.TrimPrefix(secondToken, second.Prefix+"_") == strings.TrimPrefix(token, key.Prefix+"_") {
				t.Fatal("key identifiers or secrets were reused")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = testutil.OpenDatabase(t, path)
			var digest []byte
			if err := db.QueryRowContext(t.Context(), "SELECT token_hash FROM api_keys WHERE id = ?", key.ID).Scan(&digest); err != nil {
				t.Fatal(err)
			}
			want := sha256.Sum256([]byte(token))
			if !bytes.Equal(digest, want[:]) || bytes.Equal(digest, []byte(token)) {
				t.Fatal("database must contain only the full token digest")
			}
			keys, err := auth.NewStore(db).ListAPIKeys(t.Context())
			if err != nil || len(keys) != 2 || !reflect.DeepEqual(keys[1], key) {
				t.Fatalf("persisted metadata = %+v, %v", keys, err)
			}
			encoded, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 7 {
				t.Fatalf("public metadata fields = %s", encoded)
			}
			for _, name := range []string{"id", "name", "prefix", "permission", "createdAt", "expiresAt", "revokedAt"} {
				if _, ok := fields[name]; !ok {
					t.Fatalf("missing public field %s", name)
				}
			}
			if permission, err := auth.NewStore(db).AuthenticateAPIKey(t.Context(), token); err != nil || permission != "write" {
				t.Fatalf("persisted authentication = %q, %v", permission, err)
			}
		})
	}
}

func TestAPIKeyListOrderingAndRevocation(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	store := auth.NewStore(db)
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	if keys, err := store.ListAPIKeys(t.Context()); err != nil || keys == nil || len(keys) != 0 {
		t.Fatalf("empty keys = %+v, %v", keys, err)
	}
	for _, fixture := range []struct {
		id      string
		created int64
	}{{"z", 1}, {"b", 2}, {"a", 2}} {
		digest := sha256.Sum256([]byte(fixture.id))
		if _, err := db.ExecContext(t.Context(), `INSERT INTO api_keys (id,name,prefix,permission,token_hash,created_at_ns) VALUES (?,?,?,'read',?,?)`, fixture.id, fixture.id, "arv_"+fixture.id, digest[:], fixture.created); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	if err := store.RevokeAPIKey(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	keys, err := store.ListAPIKeys(t.Context())
	if err != nil || len(keys) != 3 {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
	if keys[0].ID != "a" || keys[1].ID != "b" || keys[2].ID != "z" {
		t.Fatalf("keys are not newest first with ID tie-breaker: %+v", keys)
	}
	if keys[0].RevokedAt == nil || keys[0].RevokedAt.Before(started) || keys[0].RevokedAt.After(time.Now()) || keys[0].ExpiresAt != nil || keys[1].RevokedAt != nil {
		t.Fatal("incorrect optional metadata")
	}
	if err := store.RevokeAPIKey(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	again, err := store.ListAPIKeys(t.Context())
	if err != nil || !reflect.DeepEqual(again, keys) {
		t.Fatalf("repeated revocation changed metadata: %+v, %v", again, err)
	}
	if err := store.RevokeAPIKey(t.Context(), "missing"); !errors.Is(err, auth.ErrAPIKeyNotFound) {
		t.Fatalf("unknown revocation = %v", err)
	}
	for _, token := range []string{"a", "missing", ""} {
		if permission, err := store.AuthenticateAPIKey(t.Context(), token); !errors.Is(err, auth.ErrInvalidAPIKey) || permission != "" {
			t.Fatalf("rejected key = %q, %v", permission, err)
		}
	}
	if permission, err := store.AuthenticateAPIKey(t.Context(), "b"); err != nil || permission != "read" {
		t.Fatalf("unrelated key = %q, %v", permission, err)
	}
}

func TestAPIKeyExpirationBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
		store := auth.NewStore(db)
		if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
			t.Fatal(err)
		}
		days := 1
		_, token, err := store.CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Temporary", Permission: "read", ExpiresDays: &days})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(24*time.Hour - time.Nanosecond)
		if _, err := store.AuthenticateAPIKey(t.Context(), token); err != nil {
			t.Fatalf("key expired early: %v", err)
		}
		time.Sleep(time.Nanosecond)
		if _, err := store.AuthenticateAPIKey(t.Context(), token); !errors.Is(err, auth.ErrInvalidAPIKey) {
			t.Fatalf("key at expiry = %v", err)
		}
	})
}

func TestAPIKeyStoreErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "keys.db"))
	store := auth.NewStore(db)
	if err := store.CreateAdministrator(t.Context(), auth.CreateAdministratorParams{Name: "Camille", Email: "camille@example.com", Password: "original password for testing"}); err != nil {
		t.Fatal(err)
	}
	params := auth.CreateAPIKeyParams{Name: "Deployment", Permission: "read"}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_key BEFORE INSERT ON api_keys BEGIN SELECT RAISE(ABORT, 'key insertion failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if key, token, err := store.CreateAPIKey(t.Context(), params); err == nil || token != "" || key != (auth.APIKey{}) {
		t.Fatalf("failed insert exposed key: %+v, %q, %v", key, token, err)
	}
	if keys, err := store.ListAPIKeys(t.Context()); err != nil || len(keys) != 0 {
		t.Fatalf("failed insert persisted metadata: %+v, %v", keys, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListAPIKeys(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list = %v", err)
	}
	if _, err := store.AuthenticateAPIKey(ctx, "token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled authentication = %v", err)
	}
	if err := store.RevokeAPIKey(ctx, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled revocation = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateAPIKey(t.Context(), params); err == nil {
		t.Fatal("closed insert succeeded")
	}
	if _, err := store.ListAPIKeys(t.Context()); err == nil {
		t.Fatal("closed list succeeded")
	}
	if _, err := store.AuthenticateAPIKey(t.Context(), "token"); err == nil {
		t.Fatal("closed authentication succeeded")
	}
	if err := store.RevokeAPIKey(t.Context(), "id"); err == nil {
		t.Fatal("closed revocation succeeded")
	}
}
