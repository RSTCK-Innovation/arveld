package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerAgentKeyAPILifecycleControlsOpAMP(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/api/v1/agentkeys",
		strings.NewReader(`{"name":"API-created agent"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close creation response: %v", err)
		}
	}()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create agent key status = %d, want 201", response.StatusCode)
	}
	var created struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	connection := dialOpAMPWebSocket(t, url, created.Token)
	uid := agent.InstanceUID{1}
	message := &protobufs.AgentToServer{InstanceUid: uid[:]}
	writeOpAMPWebSocket(t, connection, message)
	readOpAMPWebSocket(t, connection, uid)

	revoke, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, url+"/api/v1/agentkeys/"+created.Key.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	revoke.AddCookie(cookie)
	revoked := doControllerRequest(t, revoke)
	defer func() {
		if err := revoked.Body.Close(); err != nil {
			t.Errorf("close revocation response: %v", err)
		}
	}()
	if revoked.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke agent key status = %d, want 204", revoked.StatusCode)
	}
	writeOpAMPWebSocket(t, connection, message)
	requireClosedOpAMPWebSocket(t, connection)
}
