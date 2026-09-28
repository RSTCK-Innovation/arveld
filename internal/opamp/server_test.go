package opamp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

func TestAcceptConnectionProvidesCallbacks(t *testing.T) {
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/v1/opamp",
		nil,
	)
	handler, _, _, token := newTestConnectionHandler(t)
	request.Header.Set("Authorization", "Bearer "+token)
	response := handler.acceptConnection(request)

	if !response.Accept {
		t.Error("connection was rejected, want accepted")
	}
	if response.ConnectionCallbacks.OnConnected == nil {
		t.Error("OnConnected callback is nil")
	}
	if response.ConnectionCallbacks.OnMessage == nil {
		t.Error("OnMessage callback is nil")
	}
	if response.ConnectionCallbacks.OnConnectionClose == nil {
		t.Error("OnConnectionClose callback is nil")
	}
	if response.ConnectionCallbacks.OnReadMessageError == nil {
		t.Error("OnReadMessageError callback is nil")
	}
	if response.ConnectionCallbacks.OnMessageResponseError == nil {
		t.Error("OnMessageResponseError callback is nil")
	}
}

func TestOnMessageStoresAgent(t *testing.T) {
	handler, store, _, _ := newTestConnectionHandler(t)
	instanceUID := make([]byte, agent.InstanceUIDSize)
	instanceUID[0] = 1

	response := handler.onMessage(
		context.Background(),
		nil,
		&protobufs.AgentToServer{InstanceUid: instanceUID},
	)

	if !bytes.Equal(response.GetInstanceUid(), instanceUID) {
		t.Errorf("response instance UID = %x, want %x", response.GetInstanceUid(), instanceUID)
	}
	if response.GetErrorResponse() != nil {
		t.Errorf("error response = %v, want nil", response.GetErrorResponse())
	}

	agents, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}
	if !bytes.Equal(agents[0].InstanceUID[:], instanceUID) {
		t.Errorf(
			"stored instance UID = %x, want %x",
			agents[0].InstanceUID,
			instanceUID,
		)
	}
	if !agents[0].Connected {
		t.Error("stored agent is disconnected, want connected")
	}
}

func TestOnMessageStoresAgentMetadataAndLastSeenAt(t *testing.T) {
	handler, store, _, _ := newTestConnectionHandler(t)
	now := time.Date(
		2026,
		time.September,
		1,
		14,
		30,
		0,
		0,
		time.UTC,
	)
	handler.now = func() time.Time { return now }
	instanceUID := make([]byte, agent.InstanceUIDSize)
	instanceUID[0] = 1

	response := handler.onMessage(
		t.Context(),
		nil,
		&protobufs.AgentToServer{
			InstanceUid: instanceUID,
			AgentDescription: &protobufs.AgentDescription{
				IdentifyingAttributes: []*protobufs.KeyValue{
					stringAttribute("service.version", "0.159.0"),
				},
				NonIdentifyingAttributes: []*protobufs.KeyValue{
					stringAttribute("host.name", "collector-01"),
				},
			},
		},
	)
	if response.GetErrorResponse() != nil {
		t.Fatalf("error response = %v, want nil", response.GetErrorResponse())
	}

	now = now.Add(time.Minute)
	response = handler.onMessage(
		t.Context(),
		nil,
		&protobufs.AgentToServer{InstanceUid: instanceUID},
	)
	if response.GetErrorResponse() != nil {
		t.Fatalf("second error response = %v, want nil", response.GetErrorResponse())
	}

	agents, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}

	storedAgent := agents[0]
	if storedAgent.Hostname == nil || *storedAgent.Hostname != "collector-01" {
		t.Errorf("Hostname = %v, want collector-01", storedAgent.Hostname)
	}
	if storedAgent.Version == nil || *storedAgent.Version != "0.159.0" {
		t.Errorf("Version = %v, want 0.159.0", storedAgent.Version)
	}
	if storedAgent.LastSeenAt == nil || !storedAgent.LastSeenAt.Equal(now) {
		t.Errorf("LastSeenAt = %v, want %s", storedAgent.LastSeenAt, now)
	}
}

func TestOnMessageRejectsInvalidInstanceUID(t *testing.T) {
	handler, _, _, _ := newTestConnectionHandler(t)
	instanceUID := make([]byte, agent.InstanceUIDSize-1)

	response := handler.onMessage(
		context.Background(),
		nil,
		&protobufs.AgentToServer{InstanceUid: instanceUID},
	)

	if response.GetErrorResponse() == nil {
		t.Fatal("error response = nil, want bad request")
	}
	if response.GetErrorResponse().GetType() != protobufs.ServerErrorResponseType_ServerErrorResponseType_BadRequest {
		t.Errorf("error type = %s, want bad request", response.GetErrorResponse().GetType())
	}
}

func TestOnMessageRejectsInstanceUIDChange(t *testing.T) {
	handler, _, _, _ := newTestConnectionHandler(t)
	connection := &testConnection{id: 1}
	firstUID := make([]byte, agent.InstanceUIDSize)
	firstUID[0] = 1
	secondUID := make([]byte, agent.InstanceUIDSize)
	secondUID[0] = 2

	firstResponse := handler.onMessage(
		context.Background(),
		connection,
		&protobufs.AgentToServer{InstanceUid: firstUID},
	)
	if firstResponse.GetErrorResponse() != nil {
		t.Fatalf("first error response = %v, want nil", firstResponse.GetErrorResponse())
	}

	secondResponse := handler.onMessage(
		context.Background(),
		connection,
		&protobufs.AgentToServer{InstanceUid: secondUID},
	)
	if secondResponse.GetErrorResponse() == nil {
		t.Fatal("second error response = nil, want bad request")
	}
	if secondResponse.GetErrorResponse().GetType() != protobufs.ServerErrorResponseType_ServerErrorResponseType_BadRequest {
		t.Errorf("error type = %s, want bad request", secondResponse.GetErrorResponse().GetType())
	}
}

func TestOnMessageReportsUnavailableStore(t *testing.T) {
	handler, _, _, _ := newTestConnectionHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response := handler.onMessage(
		ctx,
		nil,
		&protobufs.AgentToServer{InstanceUid: make([]byte, agent.InstanceUIDSize)},
	)

	if response.GetErrorResponse() == nil {
		t.Fatal("error response = nil, want unavailable")
	}
	if response.GetErrorResponse().GetType() != protobufs.ServerErrorResponseType_ServerErrorResponseType_Unavailable {
		t.Errorf("error type = %s, want unavailable", response.GetErrorResponse().GetType())
	}
}

func TestAgentDisconnectsAfterLastConnectionCloses(t *testing.T) {
	handler, store, _, token := newTestConnectionHandler(t)
	instanceUID := make([]byte, agent.InstanceUIDSize)
	instanceUID[0] = 1
	firstConnection := &testConnection{id: 1}
	secondConnection := &testConnection{id: 2}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/v1/opamp",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	firstCallbacks := handler.acceptConnection(request).ConnectionCallbacks
	secondCallbacks := handler.acceptConnection(request).ConnectionCallbacks

	firstCallbacks.OnMessage(
		context.Background(),
		firstConnection,
		&protobufs.AgentToServer{InstanceUid: instanceUID},
	)
	secondCallbacks.OnMessage(
		context.Background(),
		secondConnection,
		&protobufs.AgentToServer{InstanceUid: instanceUID},
	)

	firstCallbacks.OnConnectionClose(firstConnection)
	agents, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() after first close error = %v, want nil", err)
	}
	if !agents[0].Connected {
		t.Fatal("agent is disconnected after first close, want connected")
	}

	secondCallbacks.OnConnectionClose(secondConnection)
	agents, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("List() after second close error = %v, want nil", err)
	}
	if agents[0].Connected {
		t.Error("agent is connected after last close, want disconnected")
	}
}

func TestPartialStatusDoesNotReofferConfigurationBeingApplied(t *testing.T) {
	handler, _, _, _ := newTestConnectionHandler(t)
	uid := agent.InstanceUID{3}
	connection := &testConnection{id: 1}
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	initial := handler.onMessage(t.Context(), connection, message).GetRemoteConfig()
	if initial == nil {
		t.Fatal("missing initial configuration")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: initial.GetConfigHash(),
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING,
	}
	if response := handler.onMessage(t.Context(), connection, message); response.GetRemoteConfig() != nil {
		t.Fatal("applying acknowledgement repeated the configuration")
	}
	message.RemoteConfigStatus = nil
	message.Health = &protobufs.ComponentHealth{Healthy: false}
	if response := handler.onMessage(t.Context(), connection, message); response.GetRemoteConfig() != nil {
		t.Fatal("omitted status reoffered the configuration while the Agent was applying it")
	}
	if response := handler.onMessage(t.Context(), &testConnection{id: 2}, message); response.GetRemoteConfig() == nil {
		t.Fatal("another connection inherited the previous connection's reported hash")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{}
	if response := handler.onMessage(t.Context(), connection, message); response.GetRemoteConfig() == nil {
		t.Fatal("an explicit empty report did not replace the previous hash")
	}
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: initial.GetConfigHash(),
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING,
	}
	handler.onMessage(t.Context(), connection, message)
	handler.onConnectionClose(connection)
	message.RemoteConfigStatus = nil
	if response := handler.onMessage(t.Context(), connection, message); response.GetRemoteConfig() == nil {
		t.Fatal("closing a connection retained its previous reported hash")
	}
}

func TestOnMessageRecordsRemoteConfigStatus(t *testing.T) {
	tests := []struct {
		name           string
		protocolStatus protobufs.RemoteConfigStatuses
		wantStatus     remoteconfig.ApplyStatus
		errorMessage   string
	}{
		{
			name: "applying",
			protocolStatus: protobufs.
				RemoteConfigStatuses_RemoteConfigStatuses_APPLYING,
			wantStatus: remoteconfig.ApplyStatusApplying,
		},
		{
			name: "applied",
			protocolStatus: protobufs.
				RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
			wantStatus: remoteconfig.ApplyStatusApplied,
		},
		{
			name: "failed",
			protocolStatus: protobufs.
				RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			wantStatus:   remoteconfig.ApplyStatusFailed,
			errorMessage: "invalid receiver",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _, configStore, _ := newTestConnectionHandler(t)
			now := time.Date(2026, time.September, 2, 13, 0, 0, 0, time.UTC)
			handler.now = func() time.Time { return now }
			agentUID := agent.InstanceUID{1}
			configHash := []byte{1, 2, 3}

			response := handler.onMessage(
				t.Context(),
				nil,
				&protobufs.AgentToServer{
					InstanceUid: agentUID[:],
					Capabilities: uint64(
						protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
					),
					RemoteConfigStatus: &protobufs.RemoteConfigStatus{
						LastRemoteConfigHash: configHash,
						Status:               test.protocolStatus,
						ErrorMessage:         test.errorMessage,
					},
				},
			)
			if response.GetErrorResponse() != nil {
				t.Fatalf("error response = %v, want nil", response.GetErrorResponse())
			}

			got, err := configStore.LatestStatus(t.Context(), agentUID)
			if err != nil {
				t.Fatalf("LatestStatus() error = %v, want nil", err)
			}
			if !bytes.Equal(got.ConfigHash, configHash) {
				t.Errorf("config hash = %x, want %x", got.ConfigHash, configHash)
			}
			if got.Status != test.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, test.wantStatus)
			}
			if got.ErrorMessage != test.errorMessage {
				t.Errorf("error message = %q, want %q", got.ErrorMessage, test.errorMessage)
			}
			if !got.ReportedAt.Equal(now) {
				t.Errorf("reported at = %v, want %v", got.ReportedAt, now)
			}
		})
	}
}

func TestOnMessageKeepsFailedDesiredRemoteConfig(t *testing.T) {
	handler, agentStore, configStore, _ := newTestConnectionHandler(t)
	agentUID := agent.InstanceUID{2}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	_, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  nop:\n"),
	)
	if err != nil {
		t.Fatalf("store first configuration: %v", err)
	}
	secondRevision, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  invalid:\n"),
	)
	if err != nil {
		t.Fatalf("store second configuration: %v", err)
	}

	response := handler.onMessage(
		t.Context(),
		nil,
		&protobufs.AgentToServer{
			InstanceUid: agentUID[:],
			Capabilities: uint64(
				protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
			) | uint64(
				protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
			),
			RemoteConfigStatus: &protobufs.RemoteConfigStatus{
				LastRemoteConfigHash: secondRevision.ConfigHash[:],
				Status: protobufs.
					RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
				ErrorMessage: "unknown receiver type",
			},
		},
	)

	if response.GetErrorResponse() != nil {
		t.Fatalf("error response = %v, want nil", response.GetErrorResponse())
	}
	if response.GetRemoteConfig() != nil {
		t.Fatal("failed target caused a server-initiated retry or rollback")
	}

	desired, err := configStore.Desired(t.Context(), agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != secondRevision.Number {
		t.Errorf(
			"desired revision = %d, want %d",
			desired.Number,
			secondRevision.Number,
		)
	}
	status, err := configStore.Status(t.Context(), agentUID)
	if err != nil || status.LastWorking != nil || status.State != remoteconfig.ApplyStatusFailed {
		t.Fatalf("an unconfirmed previous revision became working: %+v, %v", status, err)
	}
}

func newTestConnectionHandler(
	t *testing.T,
) (*connectionHandler, *agent.Store, *remoteconfig.Store, string) {
	t.Helper()

	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	if err := auth.NewStore(db).CreateAdministrator(t.Context(), auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "a long password for testing",
	}); err != nil {
		t.Fatal(err)
	}
	keyStore := agentauth.NewStore(db)
	_, token, err := keyStore.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Test agent"})
	if err != nil {
		t.Fatal(err)
	}

	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	handler := newConnectionHandler(
		agentStore,
		configStore,
		slog.New(slog.DiscardHandler),
		keyStore,
		time.Now,
	)

	return handler, agentStore, configStore, token
}

func stringAttribute(key, value string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: value},
		},
	}
}

type testConnection struct {
	id int
}

func (*testConnection) Connection() net.Conn {
	return nil
}

func (*testConnection) Send(
	context.Context,
	*protobufs.ServerToAgent,
) error {
	return nil
}

func (*testConnection) Disconnect() error {
	return nil
}
