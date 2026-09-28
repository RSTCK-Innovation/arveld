package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestStoreUpsertStoresAndUpdatesAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	uid := InstanceUID{}
	uid[0] = 1
	store := NewStore(db)

	if err := store.Upsert(ctx, UpsertParams{
		InstanceUID: uid,
		Connected:   false,
	}); err != nil {
		t.Fatalf("first Upsert() error = %v, want nil", err)
	}
	if err := store.Upsert(ctx, UpsertParams{
		InstanceUID: uid,
		Connected:   true,
	}); err != nil {
		t.Fatalf("second Upsert() error = %v, want nil", err)
	}

	agents, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}
	if agents[0].InstanceUID != uid {
		t.Errorf("stored UID = %s, want %s", agents[0].InstanceUID, uid)
	}
	if !agents[0].Connected {
		t.Error("stored agent is disconnected, want connected")
	}
}

func TestStoreUpsertStoresAgentMetadata(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	lastSeenAt := time.Date(
		2026,
		time.September,
		1,
		14,
		30,
		0,
		123_000_000,
		time.UTC,
	)
	store := NewStore(db)
	uid := InstanceUID{1}
	hostname := "collector-01"
	version := "0.159.0"

	if err := store.Upsert(ctx, UpsertParams{
		InstanceUID: uid,
		Hostname:    &hostname,
		Version:     &version,
		Connected:   true,
		LastSeenAt:  &lastSeenAt,
	}); err != nil {
		t.Fatalf("Upsert() error = %v, want nil", err)
	}

	agents, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}

	storedAgent := agents[0]
	if storedAgent.Hostname == nil {
		t.Fatal("Hostname = nil, want a value")
	}
	if got, want := *storedAgent.Hostname, "collector-01"; got != want {
		t.Errorf("Hostname = %q, want %q", got, want)
	}
	if storedAgent.Version == nil {
		t.Fatal("Version = nil, want a value")
	}
	if got, want := *storedAgent.Version, "0.159.0"; got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
	if storedAgent.LastSeenAt == nil {
		t.Fatal("LastSeenAt = nil, want a value")
	}
	if !storedAgent.LastSeenAt.Equal(lastSeenAt) {
		t.Errorf("LastSeenAt = %s, want %s", storedAgent.LastSeenAt, lastSeenAt)
	}
}

func TestStoreMarkAllDisconnectedDisconnectsEveryAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	connectedUID := InstanceUID{1}
	disconnectedUID := InstanceUID{2}
	store := NewStore(db)
	if err := store.Upsert(ctx, UpsertParams{
		InstanceUID: connectedUID,
		Connected:   true,
	}); err != nil {
		t.Fatalf("Upsert() connected agent error = %v, want nil", err)
	}
	if err := store.Upsert(ctx, UpsertParams{
		InstanceUID: disconnectedUID,
		Connected:   false,
	}); err != nil {
		t.Fatalf("Upsert() disconnected agent error = %v, want nil", err)
	}

	if err := store.MarkAllDisconnected(ctx); err != nil {
		t.Fatalf("MarkAllDisconnected() error = %v, want nil", err)
	}

	agents, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 2 {
		t.Fatalf("List() returned %d agents, want 2", len(agents))
	}
	for _, storedAgent := range agents {
		if storedAgent.Connected {
			t.Errorf("agent %s is connected, want disconnected", storedAgent.InstanceUID)
		}
	}
}

func TestStoreListReturnsAgents(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	value := make([]byte, InstanceUIDSize)
	value[0] = 1
	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO agents (instance_uid) VALUES (?)",
		value,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	agents, err := NewStore(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}
	if agents[0].InstanceUID[0] != 1 {
		t.Errorf(
			"first UID byte = %d, want 1",
			agents[0].InstanceUID[0],
		)
	}
}

func TestStoreListReturnsEmptyNonNilSlice(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agents, err := NewStore(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if agents == nil {
		t.Fatal("List() returned nil, want an empty slice")
	}
	if len(agents) != 0 {
		t.Errorf("List() returned %d agents, want 0", len(agents))
	}
}
