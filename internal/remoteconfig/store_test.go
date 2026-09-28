package remoteconfig

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/database"
)

func TestStoreSavesConcurrentConfigurations(t *testing.T) {
	for _, separatePool := range []bool{false, true} {
		t.Run(fmt.Sprintf("separate_pool=%t", separatePool), func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "arveld.db")
			db := testutil.OpenDatabase(t, path)
			uid := agent.InstanceUID{1}
			if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
				InstanceUID: uid,
			}); err != nil {
				t.Fatalf("store agent: %v", err)
			}

			store := NewStore(db)
			otherStore := store
			if separatePool {
				otherDB, err := database.OpenFile(ctx, path)
				if err != nil {
					t.Fatalf("open second database pool: %v", err)
				}
				t.Cleanup(func() {
					if err := otherDB.Close(); err != nil {
						t.Errorf("close second database pool: %v", err)
					}
				})
				otherStore = NewStore(otherDB)
			}

			const writes = 24
			type saveResult struct {
				revision Revision
				err      error
			}
			start := make(chan struct{})
			results := make(chan saveResult, writes)
			for index := range writes {
				writer := store
				if index%2 != 0 {
					writer = otherStore
				}
				go func() {
					<-start
					revision, err := writer.Save(ctx, uid, fmt.Appendf(nil, "# config %d\n", index))
					results <- saveResult{revision: revision, err: err}
				}()
			}
			close(start)

			revisions := make(map[int64]Revision, writes)
			for range writes {
				result := <-results
				if result.err != nil {
					t.Errorf("concurrent Save() error = %v, want nil", result.err)
					continue
				}
				if number := result.revision.Number; number < 1 || number > writes {
					t.Errorf("revision number = %d, want 1 through %d", number, writes)
				}
				revisions[result.revision.Number] = result.revision
			}
			if len(revisions) != writes {
				t.Errorf("distinct revisions = %d, want %d", len(revisions), writes)
			}

			var persisted int
			if err := db.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM agent_config_revisions WHERE instance_uid = ?", uid[:],
			).Scan(&persisted); err != nil {
				t.Fatalf("count persisted revisions: %v", err)
			}
			if persisted != writes {
				t.Errorf("persisted revisions = %d, want %d", persisted, writes)
			}
			desired, err := store.Desired(ctx, uid)
			if err != nil {
				t.Fatalf("read desired configuration: %v", err)
			}
			if desired.Number != writes || desired.ConfigHash != revisions[writes].ConfigHash {
				t.Errorf("desired revision = %d with hash %x, want final saved revision", desired.Number, desired.ConfigHash)
			}
		})
	}
}

func TestStorePersistsRevisionConfigHash(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	const wantHash = "b78a3cd731efa5449c24e08abafbba0cd20d4f2eb9612573365b1b1f38b26c53"
	store := NewStore(db)

	saved, err := store.Save(ctx, agentUID, []byte("receivers:\n  otlp:\n"))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if got := hex.EncodeToString(saved.ConfigHash[:]); got != wantHash {
		t.Errorf("saved config hash = %q, want %q", got, wantHash)
	}

	desired, err := NewStore(db).Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if got := hex.EncodeToString(desired.ConfigHash[:]); got != wantHash {
		t.Errorf("desired config hash = %q, want %q", got, wantHash)
	}
}

func TestStoreSavesFirstDesiredRevision(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	content := []byte("receivers:\n  otlp:\n")
	store := NewStore(db)

	saved, err := store.Save(ctx, agentUID, content)
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if saved.Number != 1 {
		t.Errorf("saved revision number = %d, want 1", saved.Number)
	}
	if !bytes.Equal(saved.Content, content) {
		t.Errorf("saved revision content = %q, want %q", saved.Content, content)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
	if !bytes.Equal(desired.Content, content) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, content)
	}
}

func TestStoreSavesNextDesiredRevision(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstContent := []byte("receivers:\n  otlp:\n")
	store := NewStore(db)

	_, err := store.Save(ctx, agentUID, firstContent)
	if err != nil {
		t.Fatalf("First Save() error = %v, want nil", err)
	}

	secondContent := []byte("receivers:\n  otlp:\n  prometheus:\n")

	saved, err := store.Save(ctx, agentUID, secondContent)
	if err != nil {
		t.Fatalf("Second Save() error = %v, want nil", err)
	}
	if saved.Number != 2 {
		t.Errorf("saved revision number = %d, want 2", saved.Number)
	}
	if !bytes.Equal(saved.Content, secondContent) {
		t.Errorf("saved revision content = %q, want %q", saved.Content, secondContent)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 2 {
		t.Errorf("desired revision number = %d, want 2", desired.Number)
	}
	if !bytes.Equal(desired.Content, secondContent) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, secondContent)
	}
}

func TestStoreReusesDesiredRevisionForIdenticalContent(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstContent := []byte("receivers:\n  otlp:\n")
	store := NewStore(db)

	_, err := store.Save(ctx, agentUID, firstContent)
	if err != nil {
		t.Fatalf("First Save() error = %v, want nil", err)
	}

	identicalContent := []byte("receivers:\n  otlp:\n")

	saved, err := store.Save(ctx, agentUID, identicalContent)
	if err != nil {
		t.Fatalf("Second Save() error = %v, want nil", err)
	}
	if saved.Number != 1 {
		t.Errorf("saved revision number = %d, want 1", saved.Number)
	}
	if !bytes.Equal(saved.Content, identicalContent) {
		t.Errorf("saved revision content = %q, want %q", saved.Content, identicalContent)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
	if !bytes.Equal(desired.Content, identicalContent) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, identicalContent)
	}
}

func TestStoreReusesEarlierRevisionForIdenticalContent(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstContent := []byte("receivers:\n  otlp:\n")
	secondContent := []byte("receivers:\n  otlp:\n  prometheus:\n")
	store := NewStore(db)

	if _, err := store.Save(ctx, agentUID, firstContent); err != nil {
		t.Fatalf("first Save() error = %v, want nil", err)
	}
	if _, err := store.Save(ctx, agentUID, secondContent); err != nil {
		t.Fatalf("second Save() error = %v, want nil", err)
	}

	reused, err := store.Save(ctx, agentUID, firstContent)
	if err != nil {
		t.Fatalf("third Save() error = %v, want nil", err)
	}
	if reused.Number != 1 {
		t.Errorf("reused revision number = %d, want 1", reused.Number)
	}
	if !bytes.Equal(reused.Content, firstContent) {
		t.Errorf("reused revision content = %q, want %q", reused.Content, firstContent)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
}

func TestStoreRollsBackDesiredRevision(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstContent := []byte("receivers:\n  otlp:\n")
	secondContent := []byte("receivers:\n  otlp:\n  prometheus:\n")
	store := NewStore(db)

	if _, err := store.Save(ctx, agentUID, firstContent); err != nil {
		t.Fatalf("first Save() error = %v, want nil", err)
	}
	if _, err := store.Save(ctx, agentUID, secondContent); err != nil {
		t.Fatalf("second Save() error = %v, want nil", err)
	}

	rolledBack, err := store.Rollback(ctx, agentUID)
	if err != nil {
		t.Fatalf("Rollback() error = %v, want nil", err)
	}
	if rolledBack.Number != 1 {
		t.Errorf("rolled-back revision number = %d, want 1", rolledBack.Number)
	}
	if !bytes.Equal(rolledBack.Content, firstContent) {
		t.Errorf(
			"rolled-back revision content = %q, want %q",
			rolledBack.Content,
			firstContent,
		)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
	if !bytes.Equal(desired.Content, firstContent) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, firstContent)
	}
}

func TestStoreAlternatesBetweenDesiredAndPreviousRevisions(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstContent := []byte("receivers:\n  otlp:\n")
	secondContent := []byte("receivers:\n  otlp:\n  prometheus:\n")
	store := NewStore(db)

	if _, err := store.Save(ctx, agentUID, firstContent); err != nil {
		t.Fatalf("first Save() error = %v, want nil", err)
	}
	if _, err := store.Save(ctx, agentUID, secondContent); err != nil {
		t.Fatalf("second Save() error = %v, want nil", err)
	}

	firstRollback, err := store.Rollback(ctx, agentUID)
	if err != nil {
		t.Fatalf("first Rollback() error = %v, want nil", err)
	}
	if firstRollback.Number != 1 {
		t.Errorf("first rollback revision number = %d, want 1", firstRollback.Number)
	}

	secondRollback, err := store.Rollback(ctx, agentUID)
	if err != nil {
		t.Fatalf("second Rollback() error = %v, want nil", err)
	}
	if secondRollback.Number != 2 {
		t.Errorf("second rollback revision number = %d, want 2", secondRollback.Number)
	}
	if !bytes.Equal(secondRollback.Content, secondContent) {
		t.Errorf(
			"second rollback revision content = %q, want %q",
			secondRollback.Content,
			secondContent,
		)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 2 {
		t.Errorf("desired revision number = %d, want 2", desired.Number)
	}
}

func TestStoreRollbackReportsNoPreviousRevision(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	content := []byte("receivers:\n  otlp:\n")
	store := NewStore(db)
	if _, err := store.Save(ctx, agentUID, content); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	rolledBack, err := store.Rollback(ctx, agentUID)
	if !errors.Is(err, ErrNoPreviousRevision) {
		t.Fatalf("Rollback() error = %v, want ErrNoPreviousRevision", err)
	}
	if rolledBack.Number != 0 {
		t.Errorf("rolled-back revision number = %d, want 0", rolledBack.Number)
	}
	if rolledBack.Content != nil {
		t.Errorf("rolled-back revision content = %q, want nil", rolledBack.Content)
	}

	desired, err := store.Desired(ctx, agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
	if !bytes.Equal(desired.Content, content) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, content)
	}
}

func TestStoreDesiredReportsNoDesiredRevision(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	desired, err := NewStore(db).Desired(ctx, agentUID)
	if !errors.Is(err, ErrNoDesiredRevision) {
		t.Fatalf("Desired() error = %v, want ErrNoDesiredRevision", err)
	}
	if desired.Number != 0 {
		t.Errorf("desired revision number = %d, want 0", desired.Number)
	}
	if desired.Content != nil {
		t.Errorf("desired revision content = %q, want nil", desired.Content)
	}
}
