package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestUpdatingMonitorPreservesIdentityAndPublishesSettings(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	value := monitor.Monitor{
		ID: "homepage", Protocol: "http", Name: "Homepage", AgentInstanceUID: uid,
		Endpoint: "https://example.com/old", Method: http.MethodGet, IntervalSeconds: 30, TimeoutSeconds: 5,
	}
	store := monitor.NewStore(db)
	if err := store.Create(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	configs := remoteconfig.NewStore(db)
	if err := configs.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	before, err := configs.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	const body = `{"name":"  Updated homepage  ","agent_instance_uid":"01000000-0000-0000-0000-000000000000","endpoint":"https://example.com/new","method":"HEAD","interval_seconds":60,"timeout_seconds":10}`
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/monitors/homepage", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update Monitor = %d %s, want 200", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != value.ID || result["protocol"] != "http" || result["name"] != "Updated homepage" {
		t.Fatalf("update lost Monitor identity or normalization: %v", result)
	}
	value.Name, value.Endpoint, value.Method = "Updated homepage", "https://example.com/new", http.MethodHead
	value.IntervalSeconds, value.TimeoutSeconds = 60, 10
	stored, err := store.Get(t.Context(), value.ID)
	if err != nil || !reflect.DeepEqual(stored, value) {
		t.Fatalf("updated definition = %+v, %v; want %+v", stored, err, value)
	}
	after, err := configs.Desired(t.Context(), uid)
	if err != nil || after.ConfigHash == before.ConfigHash ||
		!strings.Contains(string(after.Content), "https://example.com/new") ||
		strings.Contains(string(after.Content), "https://example.com/old") ||
		!strings.Contains(string(after.Content), "hostmetrics:") {
		t.Fatalf("updated settings were not reconciled with the base: %v", err)
	}
	historical, err := configs.RevisionContent(t.Context(), uid, before.Number)
	if err != nil || !bytes.Equal(historical, before.Content) {
		t.Fatal("update rewrote historical configuration")
	}
}

func TestDeletingMonitorPushesPreservedBaseToConnectedAgent(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	uid := agent.InstanceUID{1}
	connection := dialOpAMPWebSocket(t, url, token)
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	writeOpAMPWebSocket(t, connection, message)
	base := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	created := createControllerHTTPMonitor(t, url, cookie, uid)
	withMonitor := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: withMonitor.GetConfigHash(), Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	writeOpAMPWebSocket(t, connection, message)
	readOpAMPWebSocket(t, connection, uid)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, url+"/api/v1/monitors/"+created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete Monitor = %d, want 204", response.StatusCode)
	}
	if _, err := monitor.NewStore(db).Get(t.Context(), created.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("deleted Monitor is still readable: %v", err)
	}
	// The Agent sends nothing: deletion must proactively offer the preserved base.
	pushed := readOpAMPWebSocket(t, connection, uid).GetRemoteConfig()
	if pushed == nil || !bytes.Equal(pushed.GetConfigHash(), base.GetConfigHash()) ||
		!bytes.Equal(pushed.GetConfig().GetConfigMap()[""].GetBody(), base.GetConfig().GetConfigMap()[""].GetBody()) {
		t.Fatal("deletion did not restore the exact base without the Monitor")
	}
	store := remoteconfig.NewStore(db)
	historical, err := store.RevisionContent(t.Context(), uid, 2)
	if err != nil || !bytes.Equal(historical, withMonitor.GetConfig().GetConfigMap()[""].GetBody()) {
		t.Fatal("deletion changed the historical Monitor configuration")
	}
	message.RemoteConfigStatus.LastRemoteConfigHash = pushed.GetConfigHash()
	writeOpAMPWebSocket(t, connection, message)
	if readOpAMPWebSocket(t, connection, uid).GetRemoteConfig() != nil {
		t.Fatal("acknowledged removal was offered again")
	}
}

func TestReassigningMonitorPushesRemovalAndAdditionToBothAgents(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	oldUID, newUID := agent.InstanceUID{1}, agent.InstanceUID{2}
	oldConnection, newConnection := dialOpAMPWebSocket(t, url, token), dialOpAMPWebSocket(t, url, token)
	writeOpAMPWebSocket(t, oldConnection, &protobufs.AgentToServer{
		InstanceUid: oldUID[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	})
	oldBase := readOpAMPWebSocket(t, oldConnection, oldUID).GetRemoteConfig()
	writeOpAMPWebSocket(t, newConnection, &protobufs.AgentToServer{
		InstanceUid: newUID[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	})
	readOpAMPWebSocket(t, newConnection, newUID)
	created := createControllerHTTPMonitor(t, url, cookie, oldUID)
	readOpAMPWebSocket(t, oldConnection, oldUID)
	body := `{"name":"Moved homepage","agent_instance_uid":"02000000-0000-0000-0000-000000000000","endpoint":"https://example.com/health","method":"GET","interval_seconds":30,"timeout_seconds":5}`
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url+"/api/v1/monitors/"+created.ID, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reassign Monitor = %d, want 200", response.StatusCode)
	}
	store := remoteconfig.NewStore(db)
	removed, err := store.Desired(t.Context(), oldUID)
	if err != nil || !bytes.Equal(removed.ConfigHash[:], oldBase.GetConfigHash()) {
		t.Fatalf("old Agent retained the reassigned Monitor: %v", err)
	}
	oldPush := readOpAMPWebSocket(t, oldConnection, oldUID).GetRemoteConfig()
	if oldPush == nil || !bytes.Equal(oldPush.GetConfigHash(), oldBase.GetConfigHash()) {
		t.Fatal("old Agent was not notified of removal")
	}
	newPush := readOpAMPWebSocket(t, newConnection, newUID).GetRemoteConfig()
	if newPush == nil || !strings.Contains(string(newPush.GetConfig().GetConfigMap()[""].GetBody()), "http_check/"+created.ID) {
		t.Fatal("new Agent was not notified of addition")
	}
	stored, err := monitor.NewStore(db).Get(t.Context(), created.ID)
	if err != nil || stored.AgentInstanceUID != newUID || stored.ID != created.ID {
		t.Fatalf("reassignment did not preserve identity: %+v, %v", stored, err)
	}
}
