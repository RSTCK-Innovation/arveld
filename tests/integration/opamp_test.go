package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	opampclient "github.com/open-telemetry/opamp-go/client"
	clienttypes "github.com/open-telemetry/opamp-go/client/types"
	"google.golang.org/protobuf/proto"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

func TestServerAcknowledgesAgentMessage(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	token := createAgentKey(t, db)

	server, err := opamp.NewServer(
		agent.NewStore(db),
		remoteconfig.NewStore(db),
		slog.New(slog.DiscardHandler),
		agentauth.NewStore(db),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close OpAMP server: %v", err)
		}
	})
	mux := http.NewServeMux()
	mux.Handle(opamp.Path, server.Handler())
	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.Config.ConnContext = server.ConnContext
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	instanceUID := []byte("0123456789abcdef")
	requestBody, err := proto.Marshal(&protobufs.AgentToServer{
		InstanceUid: instanceUID,
	})
	if err != nil {
		t.Fatalf("marshal AgentToServer: %v", err)
	}

	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		httpServer.URL+opamp.Path,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("create OpAMP request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform OpAMP request: %v", err)
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read OpAMP response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close OpAMP response: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var message protobufs.ServerToAgent
	if err := proto.Unmarshal(responseBody, &message); err != nil {
		t.Fatalf("unmarshal ServerToAgent: %v", err)
	}
	if !bytes.Equal(message.GetInstanceUid(), instanceUID) {
		t.Errorf("response instance UID = %x, want %x", message.GetInstanceUid(), instanceUID)
	}
	if message.GetErrorResponse() != nil {
		t.Errorf("error response = %v, want nil", message.GetErrorResponse())
	}
}

func TestServerDoesNotReofferDesiredRemoteConfigWithMatchingHash(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	token := createAgentKey(t, db)

	agentStore := agent.NewStore(db)
	agentUID := agent.InstanceUID{1}
	if err := agentStore.Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	configStore := remoteconfig.NewStore(db)
	content := []byte("receivers:\n  otlp:\n")
	if _, err := configStore.Save(ctx, agentUID, content); err != nil {
		t.Fatalf("save desired configuration: %v", err)
	}

	server, err := opamp.NewServer(
		agentStore,
		configStore,
		slog.New(slog.DiscardHandler),
		agentauth.NewStore(db),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close OpAMP server: %v", err)
		}
	})
	mux := http.NewServeMux()
	mux.Handle(opamp.Path, server.Handler())
	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.Config.ConnContext = server.ConnContext
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	requestBody, err := proto.Marshal(&protobufs.AgentToServer{
		InstanceUid: agentUID[:],
		Capabilities: uint64(
			protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
		),
	})
	if err != nil {
		t.Fatalf("marshal AgentToServer: %v", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpServer.URL+opamp.Path,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("create OpAMP request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform OpAMP request: %v", err)
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read OpAMP response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close OpAMP response: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var message protobufs.ServerToAgent
	if err := proto.Unmarshal(responseBody, &message); err != nil {
		t.Fatalf("unmarshal ServerToAgent: %v", err)
	}

	remoteConfig := message.GetRemoteConfig()
	if remoteConfig == nil {
		t.Fatal("remote config = nil, want desired configuration")
	}
	configFile, ok := remoteConfig.GetConfig().GetConfigMap()[""]
	if !ok {
		t.Fatal("remote config has no single-file entry")
	}
	if !bytes.Equal(configFile.GetBody(), content) {
		t.Errorf("remote config body = %q, want %q", configFile.GetBody(), content)
	}

	expectedHash := sha256.Sum256(content)
	if !bytes.Equal(remoteConfig.GetConfigHash(), expectedHash[:]) {
		t.Errorf(
			"remote config hash = %x, want %x",
			remoteConfig.GetConfigHash(),
			expectedHash,
		)
	}

	offersRemoteConfig := uint64(
		protobufs.ServerCapabilities_ServerCapabilities_OffersRemoteConfig,
	)
	if message.GetCapabilities()&offersRemoteConfig == 0 {
		t.Error("server does not advertise remote configuration")
	}

	secondRequestBody, err := proto.Marshal(&protobufs.AgentToServer{
		InstanceUid: agentUID[:],
		Capabilities: uint64(
			protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
		) | uint64(
			protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
		),
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: remoteConfig.GetConfigHash(),
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	})
	if err != nil {
		t.Fatalf("marshal second AgentToServer: %v", err)
	}

	secondRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpServer.URL+opamp.Path,
		bytes.NewReader(secondRequestBody),
	)
	if err != nil {
		t.Fatalf("create second OpAMP request: %v", err)
	}
	secondRequest.Header.Set("Content-Type", "application/x-protobuf")
	secondRequest.Header.Set("Authorization", "Bearer "+token)

	secondResponse, err := http.DefaultClient.Do(secondRequest)
	if err != nil {
		t.Fatalf("perform second OpAMP request: %v", err)
	}
	secondResponseBody, err := io.ReadAll(secondResponse.Body)
	if err != nil {
		t.Fatalf("read second OpAMP response: %v", err)
	}
	if err := secondResponse.Body.Close(); err != nil {
		t.Fatalf("close second OpAMP response: %v", err)
	}
	if secondResponse.StatusCode != http.StatusOK {
		t.Errorf(
			"second status code = %d, want %d",
			secondResponse.StatusCode,
			http.StatusOK,
		)
	}

	var secondMessage protobufs.ServerToAgent
	if err := proto.Unmarshal(secondResponseBody, &secondMessage); err != nil {
		t.Fatalf("unmarshal second ServerToAgent: %v", err)
	}
	if secondMessage.GetErrorResponse() != nil {
		t.Fatalf(
			"second error response = %v, want nil",
			secondMessage.GetErrorResponse(),
		)
	}
	if secondMessage.GetRemoteConfig() != nil {
		t.Errorf(
			"second remote config = %v, want nil for matching hash",
			secondMessage.GetRemoteConfig(),
		)
	}
}

func TestServerShutdownWaitsUntilAgentIsDisconnected(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	token := createAgentKey(t, db)

	store := agent.NewStore(db)
	server, err := opamp.NewServer(
		store,
		remoteconfig.NewStore(db),
		slog.New(slog.DiscardHandler),
		agentauth.NewStore(db),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close OpAMP server: %v", err)
		}
	})
	mux := http.NewServeMux()
	mux.Handle(opamp.Path, server.Handler())
	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.Config.ConnContext = server.ConnContext
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	client := opampclient.NewWebSocket(nil)
	if err := client.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			{
				Key: "service.name",
				Value: &protobufs.AnyValue{
					Value: &protobufs.AnyValue_StringValue{
						StringValue: "otelcol",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("set agent description: %v", err)
	}

	var instanceUID clienttypes.InstanceUid
	copy(instanceUID[:], "0123456789abcdef")
	acknowledged := make(chan struct{}, 1)
	if err := client.Start(ctx, clienttypes.StartSettings{
		OpAMPServerURL: "ws://" + httpServer.Listener.Addr().String() + opamp.Path,
		InstanceUid:    instanceUID,
		Header: http.Header{
			"Authorization": {"Bearer " + token},
		},
		Callbacks: clienttypes.Callbacks{
			OnMessage: func(context.Context, *clienttypes.MessageData) {
				select {
				case acknowledged <- struct{}{}:
				default:
				}
			},
		},
	}); err != nil {
		t.Fatalf("start OpAMP client: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := client.Stop(stopCtx); err != nil {
			t.Errorf("stop OpAMP client: %v", err)
		}
	})

	select {
	case <-acknowledged:
	case <-time.After(2 * time.Second):
		t.Fatal("OpAMP client did not receive an acknowledgement")
	}

	if err := httpServer.Listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	transactionClosed := false
	t.Cleanup(func() {
		if !transactionClosed {
			// Rollback is best-effort cleanup if the test failed before the
			// explicitly checked rollback below.
			_ = tx.Rollback() //nolint:errcheck // best-effort cleanup after test failure
		}
	})
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE agents SET connected = 1 WHERE instance_uid = ?",
		instanceUID[:],
	); err != nil {
		t.Fatalf("lock agent row: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	shutdownErrors := make(chan error, 1)
	go func() {
		shutdownErrors <- server.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-shutdownErrors:
		t.Fatalf("Shutdown() returned before the callback finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}
	transactionClosed = true

	select {
	case err := <-shutdownErrors:
		if err != nil {
			t.Fatalf("Shutdown() error = %v, want nil", err)
		}
	case <-shutdownCtx.Done():
		t.Fatal("Shutdown() did not wait for the connection callback")
	}

	agents, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("List() returned %d agents, want 1", len(agents))
	}
	if agents[0].Connected {
		t.Fatal("agent is connected after Server.Shutdown()")
	}
}
