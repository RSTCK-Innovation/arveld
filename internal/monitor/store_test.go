package monitor_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMonitorCreationPreservesStoredDefinitions(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := monitor.NewStore(db)
	want := monitor.Monitor{
		Protocol:         "http",
		ID:               "http-homepage",
		Name:             "Public homepage",
		AgentInstanceUID: uid,
		Endpoint:         "https://example.com",
		Method:           "HEAD",
		IntervalSeconds:  60,
		TimeoutSeconds:   10,
	}
	if err := store.Create(ctx, want); err != nil {
		t.Fatal(err)
	}
	duplicate := want
	duplicate.Name = "Replacement homepage"
	duplicate.Endpoint = "https://replacement.example.com"
	if err := store.Create(ctx, duplicate); err == nil {
		t.Fatal("Create() with duplicate ID succeeded, want an error")
	}
	got, err := store.Get(ctx, want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Get() after duplicate creation = %+v, %v; want %+v, nil", got, err, want)
	}

	missingAgent := want
	missingAgent.ID = "http-other-agent"
	missingAgent.AgentInstanceUID = agent.InstanceUID{2}
	if err := store.Create(ctx, missingAgent); err == nil {
		t.Fatal("Create() with missing Agent succeeded, want an error")
	}
	got, err = store.Get(ctx, missingAgent.ID)
	if !errors.Is(err, monitor.ErrNotFound) || !reflect.DeepEqual(got, (monitor.Monitor{})) {
		t.Fatalf("Get() after rejected creation = %+v, %v; want zero value, ErrNotFound", got, err)
	}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{InstanceUID: missingAgent.AgentInstanceUID}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, missingAgent); err != nil {
		t.Fatalf("Create() after registering Agent = %v, want nil", err)
	}
	got, err = store.Get(ctx, missingAgent.ID)
	if err != nil || !reflect.DeepEqual(got, missingAgent) {
		t.Fatalf("Get() after registering Agent = %+v, %v; want %+v, nil", got, err, missingAgent)
	}
}

func TestMonitorStoreReportsDatabaseFailures(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	store := monitor.NewStore(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	value := monitor.Monitor{
		Protocol:         "http",
		ID:               "http-homepage",
		Name:             "Public homepage",
		AgentInstanceUID: agent.InstanceUID{1},
		Endpoint:         "https://example.com",
		Method:           "GET",
		IntervalSeconds:  30,
		TimeoutSeconds:   5,
	}
	if err := store.Create(ctx, value); err == nil {
		t.Fatal("Create() with closed database succeeded, want an error")
	}
	if _, err := store.Update(ctx, value); err == nil || errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("Update() with closed database = %v, want a storage error", err)
	}
	if _, err := store.Delete(ctx, value.ID); err == nil || errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("Delete() with closed database = %v, want a storage error", err)
	}
	got, err := store.Get(ctx, value.ID)
	if err == nil || errors.Is(err, monitor.ErrNotFound) || !reflect.DeepEqual(got, (monitor.Monitor{})) {
		t.Fatalf("Get() with closed database = %+v, %v; want zero value and a storage error", got, err)
	}
}
