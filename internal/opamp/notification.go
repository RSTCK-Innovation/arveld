package opamp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	servertypes "github.com/open-telemetry/opamp-go/server/types"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

const configurationSendTimeout = 5 * time.Second

// configSession serializes response selection and sending with proactive pushes.
// keyID and webSocket are fixed at admission; gate protects acceptsRemoteConfig.
type configSession struct {
	gate                chan struct{}
	keyID               string
	webSocket           bool
	acceptsRemoteConfig bool
}

func (session *configSession) lock(ctx context.Context) error {
	select {
	case session.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			session.unlock()
			return fmt.Errorf("wait for OpAMP configuration session: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for OpAMP configuration session: %w", ctx.Err())
	}
}

func (session *configSession) unlock() {
	<-session.gate
}

// NotifyAgentConfig offers the latest committed target to eligible live WebSockets.
// It does not compile or change desired intent; plain HTTP Agents use their next poll.
// Delivery owns one timeout independent of management-request cancellation.
func (server *Server) NotifyAgentConfig(ctx context.Context, uid agent.InstanceUID) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), configurationSendTimeout)
	defer cancel()
	handler := server.connectionHandler
	handler.mu.Lock()
	connections := make(map[servertypes.Connection]*configSession)
	for connection, session := range handler.configSessions {
		if assigned, ok := handler.agentsByConnection[connection]; ok && assigned == uid && session.webSocket {
			connections[connection] = session
		}
	}
	handler.mu.Unlock()
	var notifyErr error
	for connection, session := range connections {
		if err := ctx.Err(); err != nil {
			return errors.Join(notifyErr, fmt.Errorf("notify Agent configuration: %w", err))
		}
		if err := handler.notifyConnection(ctx, connection, session, uid); err != nil {
			notifyErr = errors.Join(notifyErr, err)
		}
	}
	return notifyErr
}

func (handler *connectionHandler) notifyConnection(ctx context.Context, connection servertypes.Connection, session *configSession, uid agent.InstanceUID) error {
	if err := session.lock(ctx); err != nil {
		return err
	}
	defer session.unlock()
	handler.mu.Lock()
	live := !handler.closing && handler.configSessions[connection] == session && handler.agentsByConnection[connection] == uid
	handler.mu.Unlock()
	if !live || !session.acceptsRemoteConfig {
		return nil
	}
	if err := handler.keyStore.CheckKeyActive(ctx, session.keyID); err != nil {
		handler.disconnectNotificationConnection(ctx, connection)
		return fmt.Errorf("authorize OpAMP notification: %w", err)
	}
	// Read after acquiring the session lock: queued notifications must never
	// send an older captured artifact after another notification sent a newer one.
	status, err := handler.configStore.Status(ctx, uid)
	if err != nil {
		return fmt.Errorf("read OpAMP notification target: %w", err)
	}
	offer := remoteConfigOffer(status, handler.reportedConfig(connection, nil))
	if offer == nil {
		return nil
	}
	return handler.sendMessage(ctx, connection, &protobufs.ServerToAgent{
		InstanceUid:  uid[:],
		Capabilities: uint64(protobufs.ServerCapabilities_ServerCapabilities_OffersRemoteConfig),
		RemoteConfig: offer,
	})
}

// sendMessage is called only while holding the connection's configSession gate.
// The pinned upstream Send ignores context cancellation, so cancellation must
// also close the connection to unblock the write. No database lock is held here.
func (handler *connectionHandler) sendMessage(ctx context.Context, connection servertypes.Connection, message *protobufs.ServerToAgent) error {
	ctx, cancel := context.WithTimeout(ctx, configurationSendTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send OpAMP message: %w", err)
	}
	stop := context.AfterFunc(ctx, func() {
		handler.disconnectNotificationConnection(ctx, connection)
	})
	defer stop()
	if err := connection.Send(ctx, message); err != nil {
		handler.disconnectNotificationConnection(ctx, connection)
		return fmt.Errorf("send OpAMP message: %w", err)
	}
	return nil
}

func (handler *connectionHandler) disconnectNotificationConnection(ctx context.Context, connection servertypes.Connection) {
	if err := connection.Disconnect(); err != nil {
		handler.logger.ErrorContext(ctx, "disconnect OpAMP notification connection", "error", err)
	}
}
