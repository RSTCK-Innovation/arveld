package integration

import (
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestRunMarksStoredAgentsDisconnectedOnStartup(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	store := agent.NewStore(db)
	uid := agent.InstanceUID{1}
	if err := store.Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid, Connected: true}); err != nil {
		t.Fatal(err)
	}
	_, stop := startController(t, config)
	agents, err := store.List(t.Context())
	if err != nil || len(agents) != 1 || agents[0].Connected {
		t.Fatalf("startup agents = %+v, %v", agents, err)
	}
	stop()
}
