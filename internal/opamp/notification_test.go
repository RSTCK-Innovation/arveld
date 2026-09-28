package opamp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	servertypes "github.com/open-telemetry/opamp-go/server/types"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNotifyAgentConfigBoundsWholeBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handler, _, _, token := newTestConnectionHandler(t)
		server := &Server{connectionHandler: handler}
		uid := agent.InstanceUID{1}
		for range 3 {
			connection := &notificationTestConnection{closed: make(chan struct{})}
			registerNotificationConnection(t, handler, token, connection, uid)
			// Match the pinned transport: Send only unblocks on Disconnect.
			connection.send = func(ctx context.Context, _ *protobufs.ServerToAgent) error {
				<-connection.closed
				return fmt.Errorf("stalled connection: %w", ctx.Err())
			}
		}
		start := time.Now()
		err := server.NotifyAgentConfig(t.Context(), uid)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("notification error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("notification batch took %s, want one five-second budget", elapsed)
		}
	})
}

func TestNotifyAgentConfigBoundsWaitForBusyConnection(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	if err := auth.NewStore(db).CreateAdministrator(t.Context(), auth.CreateAdministratorParams{
		Name: "Camille", Email: "camille@example.com", Password: "a long password for testing",
	}); err != nil {
		t.Fatal(err)
	}
	keys := agentauth.NewStore(db)
	_, token, err := keys.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Busy Agent"})
	if err != nil {
		t.Fatal(err)
	}
	handler := newConnectionHandler(agent.NewStore(db), remoteconfig.NewStore(db), slog.New(slog.DiscardHandler), keys, time.Now)
	server := &Server{connectionHandler: handler}
	uid := agent.InstanceUID{1}
	connection := &notificationTestConnection{closed: make(chan struct{})}
	callbacks := registerNotificationConnection(t, handler, token, connection, uid)
	reserved, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reserved.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	messageDone := make(chan struct{})
	defer func() { cancel(); <-messageDone }()
	waitCount := db.Stats().WaitCount
	go func() {
		defer close(messageDone)
		callbacks.OnMessage(ctx, connection, &protobufs.AgentToServer{InstanceUid: uid[:]})
	}()
	// Hold the session through the real callback while its key recheck waits for SQLite.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for db.Stats().WaitCount == waitCount {
		select {
		case <-ctx.Done():
			t.Fatal("Agent message did not reach the reserved database connection")
		case <-ticker.C:
		}
	}
	notificationDone := make(chan struct{})
	var notificationErr error
	go func() {
		defer close(notificationDone)
		notificationErr = server.NotifyAgentConfig(t.Context(), uid)
	}()
	select {
	case <-notificationDone:
		if !errors.Is(notificationErr, context.DeadlineExceeded) {
			t.Errorf("busy notification error = %v, want deadline exceeded", notificationErr)
		}
		select {
		case <-connection.closed:
			t.Error("waiting for the session disconnected the active Agent")
		default:
		}
	case <-time.After(6 * time.Second):
		cancel()
		<-notificationDone
		t.Fatal("waiting for a busy connection exceeded the five-second batch budget")
	}
}

func TestNotifyAgentConfigSurvivesCallerCancellation(t *testing.T) {
	for _, phase := range []string{"before notification", "during send"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				handler, _, _, token := newTestConnectionHandler(t)
				server := &Server{connectionHandler: handler}
				uid := agent.InstanceUID{1}
				connection := &notificationTestConnection{closed: make(chan struct{})}
				registerNotificationConnection(t, handler, token, connection, uid)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if phase == "before notification" {
					cancel()
				}
				sent := false
				connection.send = func(ctx context.Context, _ *protobufs.ServerToAgent) error {
					cancel()
					synctest.Wait()
					if err := ctx.Err(); err != nil {
						return fmt.Errorf("notification interrupted by caller: %w", err)
					}
					sent = true
					return nil
				}
				if err := server.NotifyAgentConfig(ctx, uid); err != nil {
					t.Errorf("notification after caller cancellation: %v", err)
				}
				if !sent {
					t.Error("caller cancellation prevented delivery of the committed target")
				}
				synctest.Wait()
				select {
				case <-connection.closed:
					t.Error("caller cancellation disconnected the Agent")
				default:
				}
			})
		})
	}
}

// notificationTestConnection controls the external transport without replacing stores.
type notificationTestConnection struct {
	testConnection
	send      func(context.Context, *protobufs.ServerToAgent) error
	closed    chan struct{}
	closeOnce sync.Once
}

func (connection *notificationTestConnection) Send(ctx context.Context, message *protobufs.ServerToAgent) error {
	if connection.send == nil {
		return nil
	}
	return connection.send(ctx, message)
}

func (connection *notificationTestConnection) Disconnect() error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

func registerNotificationConnection(t *testing.T, handler *connectionHandler, token string, connection servertypes.Connection, uid agent.InstanceUID) servertypes.ConnectionCallbacks {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, Path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response := handler.acceptConnection(request)
	if !response.Accept {
		t.Fatal("notification connection was rejected")
	}
	callbacks := response.ConnectionCallbacks
	callbacks.OnConnected(t.Context(), connection)
	callbacks.OnMessage(t.Context(), connection, &protobufs.AgentToServer{
		InstanceUid: uid[:], Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	})
	return callbacks
}
