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

func TestMonitorsSurviveReopenAndPreserveAssignments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	uid := agent.InstanceUID{1}
	other := agent.InstanceUID{2}
	for _, id := range []agent.InstanceUID{uid, other} {
		if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: id}); err != nil {
			t.Fatal(err)
		}
	}
	want := []monitor.Monitor{
		{ID: "0-http", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid, Endpoint: "https://example.com/$ready", Method: "HEAD", IntervalSeconds: 30, TimeoutSeconds: 5},
		{ID: "a-tcp", Name: "Database", Protocol: "tcp", AgentInstanceUID: uid, Endpoint: "localhost:5432", IntervalSeconds: 30, TimeoutSeconds: 5},
		{ID: "b-icmp", Name: "Router", Protocol: "icmp", AgentInstanceUID: uid, Endpoint: "::1", PingCount: 3, IntervalSeconds: 30, TimeoutSeconds: 5},
		{ID: "c-dns", Name: "Resolution", Protocol: "dns", AgentInstanceUID: other, Endpoint: "example.com", DNSServer: "1.1.1.1:53", RecordType: "A", Transport: "udp", IntervalSeconds: 30, TimeoutSeconds: 5},
	}
	store := monitor.NewStore(db)
	for _, value := range want {
		if err := store.Create(t.Context(), value); err != nil {
			t.Fatal(err)
		}
		value.Name = "Replacement"
		if err := store.Create(t.Context(), value); err == nil {
			t.Fatal("duplicate ID overwrote an existing Monitor")
		}
	}
	missing := want[0]
	missing.ID = "missing-agent"
	missing.AgentInstanceUID = agent.InstanceUID{3}
	if err := store.Create(t.Context(), missing); !errors.Is(err, monitor.ErrAgentNotFound) {
		t.Fatalf("missing Agent: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	store = monitor.NewStore(db)
	for _, value := range want {
		got, err := store.Get(t.Context(), value.ID)
		if err != nil || !reflect.DeepEqual(got, value) {
			t.Fatalf("reopened Monitor = %+v, %v, want %+v", got, err, value)
		}
	}
	all, err := store.List(t.Context())
	if err != nil || !reflect.DeepEqual(all, want) {
		t.Fatalf("inventory = %+v, %v", all, err)
	}
	assigned, err := monitor.ListForAgent(t.Context(), db, uid)
	if err != nil || !reflect.DeepEqual(assigned, want[:3]) {
		t.Fatalf("assigned Monitors = %+v, %v", assigned, err)
	}
	if _, err := store.Get(t.Context(), missing.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("rejected Monitor was persisted: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(t.Context()); err == nil {
		t.Fatal("closed database returned a successful inventory")
	}
}
