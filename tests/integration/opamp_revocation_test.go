package integration

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerDisconnectsRevokedOpAMPKeyBeforeProcessingNextMessage(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	keys := agentauth.NewStore(db)
	key, token, err := keys.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Agent to revoke"})
	if err != nil {
		t.Fatal(err)
	}
	otherToken := createAgentKey(t, db)
	url, _ := startController(t, config)
	connection := dialOpAMPWebSocket(t, url, token)
	otherConnection := dialOpAMPWebSocket(t, url, otherToken)
	uid, otherUID := agent.InstanceUID{1}, agent.InstanceUID{2}
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		AgentDescription: &protobufs.AgentDescription{
			NonIdentifyingAttributes: []*protobufs.KeyValue{{
				Key: "host.name",
				Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{
					StringValue: "original-host",
				}},
			}},
		},
	}
	writeOpAMPWebSocket(t, connection, message)
	readOpAMPWebSocket(t, connection, uid)
	writeOpAMPWebSocket(t, otherConnection, &protobufs.AgentToServer{InstanceUid: otherUID[:]})
	readOpAMPWebSocket(t, otherConnection, otherUID)
	agents := agent.NewStore(db)
	before, err := agents.List(t.Context())
	if err != nil || len(before) != 2 || before[0].LastSeenAt == nil || !before[0].Connected || !before[1].Connected {
		t.Fatalf("agents before revocation = %+v, %v, want two connected agents", before, err)
	}
	configs := remoteconfig.NewStore(db)
	revision, err := configs.Save(t.Context(), uid, []byte("receivers: {}"))
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.RevokeKey(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	message.AgentDescription.NonIdentifyingAttributes[0].Value = &protobufs.AnyValue{
		Value: &protobufs.AnyValue_StringValue{StringValue: "must-not-be-stored"},
	}
	message.Capabilities = uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig)
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: revision.ConfigHash[:],
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	writeOpAMPWebSocket(t, connection, message)
	requireClosedOpAMPWebSocket(t, connection)

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		after, err := agents.List(t.Context())
		if err != nil || len(after) != 2 {
			t.Fatalf("agents after revocation = %+v, %v", after, err)
		}
		if !after[0].Connected {
			if after[0].Hostname == nil || *after[0].Hostname != "original-host" || after[0].LastSeenAt == nil || !after[0].LastSeenAt.Equal(*before[0].LastSeenAt) {
				t.Fatal("revoked message changed agent metadata or last-seen timestamp")
			}
			if !after[1].Connected {
				t.Fatal("revocation disconnected the other key's agent")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("revoked agent was not marked disconnected")
		case <-ticker.C:
		}
	}
	if _, err := configs.LatestStatus(t.Context(), uid); !errors.Is(err, remoteconfig.ErrNoReportedStatus) {
		t.Fatalf("remote configuration report after revocation = %v, want no report", err)
	}
	writeOpAMPWebSocket(t, otherConnection, &protobufs.AgentToServer{InstanceUid: otherUID[:]})
	readOpAMPWebSocket(t, otherConnection, otherUID)
}

func TestOpAMPDisconnectsWhenKeyRevalidationStorageFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	// Keep agent writes available while closing only the key reader's connection pool.
	keyDB := testutil.OpenDatabase(t, path)
	server, err := opamp.NewServer(agent.NewStore(db), remoteconfig.NewStore(db), testLogger(), agentauth.NewStore(keyDB))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shut down OpAMP server: %v", err)
		}
	})
	httpServer := httptest.NewUnstartedServer(server.Handler())
	httpServer.Config.ConnContext = server.ConnContext
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	connection := dialOpAMPWebSocket(t, httpServer.URL, token)
	uid := agent.InstanceUID{1}
	writeOpAMPWebSocket(t, connection, &protobufs.AgentToServer{InstanceUid: uid[:]})
	readOpAMPWebSocket(t, connection, uid)
	if err := keyDB.Close(); err != nil {
		t.Fatal(err)
	}
	// This message would change agent metadata if revalidation failed open.
	writeOpAMPWebSocket(t, connection, &protobufs.AgentToServer{
		InstanceUid: uid[:],
		AgentDescription: &protobufs.AgentDescription{
			NonIdentifyingAttributes: []*protobufs.KeyValue{{
				Key: "host.name",
				Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{
					StringValue: "must-not-be-stored",
				}},
			}},
		},
	})
	requireClosedOpAMPWebSocket(t, connection)
	agents, err := agent.NewStore(db).List(t.Context())
	if err != nil || len(agents) != 1 || agents[0].InstanceUID != uid || agents[0].Hostname != nil {
		t.Fatalf("agents after key storage failure = %+v, %v, want only the original agent", agents, err)
	}
}

func dialOpAMPWebSocket(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	connection, response, err := dialer.DialContext(t.Context(), "ws://"+strings.TrimPrefix(url, "http://")+opamp.Path, http.Header{
		"Authorization": {"Bearer " + token},
	})
	if response != nil {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close handshake response: %v", err)
		}
	}
	if err != nil {
		t.Fatalf("connect OpAMP WebSocket: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close test WebSocket: %v", err)
		}
	})
	return connection
}

func writeOpAMPWebSocket(t *testing.T, connection *websocket.Conn, message *protobufs.AgentToServer) {
	t.Helper()
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// OpAMP WebSocket frames prefix the generated Protobuf payload with a zero byte.
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, data...)); err != nil {
		t.Fatalf("send OpAMP message: %v", err)
	}
}

func readOpAMPWebSocket(t *testing.T, connection *websocket.Conn, uid agent.InstanceUID) *protobufs.ServerToAgent {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	typeID, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read OpAMP response: %v", err)
	}
	if typeID != websocket.BinaryMessage || len(data) == 0 || data[0] != 0 {
		t.Fatal("response is not an OpAMP binary frame")
	}
	var response protobufs.ServerToAgent
	if err := proto.Unmarshal(data[1:], &response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.GetInstanceUid(), uid[:]) || response.GetErrorResponse() != nil {
		t.Fatalf("OpAMP response = %v, want acknowledgement for %s", &response, uid)
	}
	return &response
}

func requireClosedOpAMPWebSocket(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, err := connection.ReadMessage()
	if err == nil {
		t.Fatal("received an OpAMP response, want the connection closed before processing the message")
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		t.Fatalf("connection remained open until timeout: %v", err)
	}
}
