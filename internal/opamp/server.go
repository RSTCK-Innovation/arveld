package opamp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-telemetry/opamp-go/protobufs"
	servertypes "github.com/open-telemetry/opamp-go/server/types"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

const connectionStateUpdateTimeout = 5 * time.Second

var errConnectionInstanceUIDChanged = errors.New(
	"connection instance UID changed",
)

type connectionHandler struct {
	agentStore         *agent.Store
	configStore        *remoteconfig.Store
	logger             *slog.Logger
	keyStore           *agentauth.Store
	now                func() time.Time
	mu                 sync.Mutex
	connectionsDone    sync.WaitGroup
	connections        map[servertypes.Connection]struct{}
	configSessions     map[servertypes.Connection]*configSession
	agentsByConnection map[servertypes.Connection]agent.InstanceUID
	reportedConfigs    map[servertypes.Connection]reportedConfigState
	connectionCounts   map[agent.InstanceUID]int
	closing            bool
}

func newConnectionHandler(
	agentStore *agent.Store,
	configStore *remoteconfig.Store,
	logger *slog.Logger,
	keyStore *agentauth.Store,
	now func() time.Time,
) *connectionHandler {
	return &connectionHandler{
		agentStore:         agentStore,
		configStore:        configStore,
		logger:             logger,
		keyStore:           keyStore,
		now:                now,
		connections:        make(map[servertypes.Connection]struct{}),
		configSessions:     make(map[servertypes.Connection]*configSession),
		agentsByConnection: make(map[servertypes.Connection]agent.InstanceUID),
		reportedConfigs:    make(map[servertypes.Connection]reportedConfigState),
		connectionCounts:   make(map[agent.InstanceUID]int),
	}
}

func (handler *connectionHandler) acceptConnection(
	request *http.Request,
) servertypes.ConnectionResponse {
	headers := request.Header.Values("Authorization")
	var keyID string
	err := agentauth.ErrInvalidKey
	if len(headers) == 1 {
		parts := strings.Fields(headers[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			keyID, err = handler.keyStore.AuthenticateKey(request.Context(), parts[1])
		}
	}
	if errors.Is(err, agentauth.ErrInvalidKey) {
		return servertypes.ConnectionResponse{
			Accept:         false,
			HTTPStatusCode: http.StatusUnauthorized,
			HTTPResponseHeader: map[string]string{
				"WWW-Authenticate": "Bearer",
			},
		}
	}
	if err != nil {
		handler.logger.ErrorContext(request.Context(), "authenticate OpAMP agent key", "error", err)
		return servertypes.ConnectionResponse{
			Accept:         false,
			HTTPStatusCode: http.StatusInternalServerError,
		}
	}

	session := &configSession{gate: make(chan struct{}, 1), keyID: keyID, webSocket: websocket.IsWebSocketUpgrade(request)}
	callbacks := servertypes.ConnectionCallbacks{
		OnConnected: func(ctx context.Context, connection servertypes.Connection) {
			handler.onConnected(ctx, connection, session)
		},
		OnMessage: func(ctx context.Context, connection servertypes.Connection, message *protobufs.AgentToServer) *protobufs.ServerToAgent {
			if err := session.lock(ctx); err != nil {
				handler.logger.ErrorContext(ctx, "wait for OpAMP response", "error", err)
				return nil
			}
			defer session.unlock()
			// Capture only the authenticated key ID, and use the message's context.
			if err := handler.keyStore.CheckKeyActive(ctx, keyID); err != nil {
				if !errors.Is(err, agentauth.ErrInvalidKey) {
					handler.logger.ErrorContext(ctx, "recheck OpAMP agent key", "error", err)
				}
				if err := connection.Disconnect(); err != nil {
					handler.logger.ErrorContext(ctx, "disconnect unauthorized OpAMP connection", "error", err)
				}
				return nil
			}
			session.acceptsRemoteConfig = message.GetCapabilities()&uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) != 0
			response := handler.onMessage(ctx, connection, message)
			if !session.webSocket {
				return response
			}
			// Keep selection and sending under the same lock as proactive pushes.
			if err := handler.sendMessage(ctx, connection, response); err != nil {
				handler.logger.ErrorContext(ctx, "send OpAMP response", "error", err)
			}
			return nil
		},
		OnConnectionClose: handler.onConnectionClose,
	}
	callbacks.SetDefaults()

	return servertypes.ConnectionResponse{
		Accept:              true,
		ConnectionCallbacks: callbacks,
	}
}

func (handler *connectionHandler) onConnected(
	ctx context.Context,
	connection servertypes.Connection,
	session *configSession,
) {
	handler.mu.Lock()
	if !handler.closing {
		handler.connections[connection] = struct{}{}
		handler.configSessions[connection] = session
		handler.connectionsDone.Add(1)
		handler.mu.Unlock()
		return
	}
	handler.mu.Unlock()

	if err := connection.Disconnect(); err != nil {
		handler.logger.ErrorContext(
			ctx,
			"disconnect OpAMP connection during shutdown",
			"error",
			err,
		)
	}
}

func (handler *connectionHandler) onMessage(
	ctx context.Context,
	connection servertypes.Connection,
	message *protobufs.AgentToServer,
) *protobufs.ServerToAgent {
	response := &protobufs.ServerToAgent{
		InstanceUid: bytes.Clone(message.GetInstanceUid()),
	}

	uid, err := agent.NewInstanceUID(message.GetInstanceUid())
	if err != nil {
		handler.logger.WarnContext(ctx, "reject OpAMP message", "error", err)
		response.ErrorResponse = &protobufs.ServerErrorResponse{
			Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_BadRequest,
			ErrorMessage: "invalid instance UID",
		}

		return response
	}

	description := message.GetAgentDescription()
	lastSeenAt := handler.now()
	params := agent.UpsertParams{
		InstanceUID: uid,
		Hostname: attributeString(
			description.GetNonIdentifyingAttributes(),
			"host.name",
		),
		Version: attributeString(
			description.GetIdentifyingAttributes(),
			"service.version",
		),
		Connected:  true,
		LastSeenAt: &lastSeenAt,
	}
	if err := handler.markConnected(ctx, connection, params); err != nil {
		if errors.Is(err, errConnectionInstanceUIDChanged) {
			handler.logger.WarnContext(
				ctx,
				"reject OpAMP message",
				"error",
				err,
			)
			response.ErrorResponse = &protobufs.ServerErrorResponse{
				Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_BadRequest,
				ErrorMessage: "connection instance UID changed",
			}

			return response
		}

		handler.logger.ErrorContext(
			ctx,
			"store OpAMP agent",
			"instance_uid",
			uid.String(),
			"error",
			err,
		)
		response.ErrorResponse = &protobufs.ServerErrorResponse{
			Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_Unavailable,
			ErrorMessage: "server unavailable",
		}

		return response
	}

	acceptsRemoteConfig := uint64(
		protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
	)
	if message.GetCapabilities()&acceptsRemoteConfig != 0 {
		// Publish the artifact before matching this message's report against it.
		if err := handler.configStore.ReconcileAgent(ctx, uid); err != nil {
			handler.logger.ErrorContext(
				ctx,
				"reconcile desired OpAMP configuration",
				"instance_uid",
				uid.String(),
				"error",
				err,
			)
			response.ErrorResponse = &protobufs.ServerErrorResponse{
				Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_Unavailable,
				ErrorMessage: "server unavailable",
			}

			return response
		}
	}

	if err := handler.recordRemoteConfigStatus(
		ctx,
		uid,
		message,
		lastSeenAt,
	); err != nil {
		handler.logger.ErrorContext(
			ctx,
			"store OpAMP remote configuration status",
			"instance_uid",
			uid.String(),
			"error",
			err,
		)
		response.ErrorResponse = &protobufs.ServerErrorResponse{
			Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_Unavailable,
			ErrorMessage: "server unavailable",
		}

		return response
	}

	reported := handler.reportedConfig(connection, message.GetRemoteConfigStatus())
	if message.GetCapabilities()&acceptsRemoteConfig != 0 {
		status, err := handler.configStore.Status(ctx, uid)
		if err != nil {
			handler.logger.ErrorContext(
				ctx,
				"read desired OpAMP configuration",
				"instance_uid",
				uid.String(),
				"error",
				err,
			)
			response.ErrorResponse = &protobufs.ServerErrorResponse{
				Type:         protobufs.ServerErrorResponseType_ServerErrorResponseType_Unavailable,
				ErrorMessage: "server unavailable",
			}

			return response
		}

		response.RemoteConfig = remoteConfigOffer(status, reported)
	}

	response.Capabilities = uint64(
		protobufs.ServerCapabilities_ServerCapabilities_OffersRemoteConfig,
	)

	return response
}

func (handler *connectionHandler) markConnected(
	ctx context.Context,
	connection servertypes.Connection,
	params agent.UpsertParams,
) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if currentUID, ok := handler.agentsByConnection[connection]; ok {
		if currentUID != params.InstanceUID {
			return errConnectionInstanceUIDChanged
		}
	}

	if err := handler.agentStore.Upsert(ctx, params); err != nil {
		return fmt.Errorf("upsert connected agent: %w", err)
	}

	if _, ok := handler.agentsByConnection[connection]; !ok {
		handler.agentsByConnection[connection] = params.InstanceUID
		handler.connectionCounts[params.InstanceUID]++
	}

	return nil
}

func attributeString(
	attributes []*protobufs.KeyValue,
	key string,
) *string {
	for _, attribute := range attributes {
		if attribute.GetKey() != key {
			continue
		}

		value, ok := attribute.GetValue().GetValue().(*protobufs.AnyValue_StringValue)
		if !ok {
			return nil
		}

		return &value.StringValue
	}

	return nil
}

func (handler *connectionHandler) onConnectionClose(
	connection servertypes.Connection,
) {
	handler.mu.Lock()
	_, tracked := handler.connections[connection]
	delete(handler.connections, connection)
	delete(handler.configSessions, connection)
	delete(handler.reportedConfigs, connection)
	if tracked {
		defer handler.connectionsDone.Done()
	}
	defer handler.mu.Unlock()

	uid, ok := handler.agentsByConnection[connection]
	if !ok {
		return
	}

	delete(handler.agentsByConnection, connection)
	remainingConnections := handler.connectionCounts[uid] - 1
	if remainingConnections > 0 {
		handler.connectionCounts[uid] = remainingConnections
		return
	}
	delete(handler.connectionCounts, uid)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		connectionStateUpdateTimeout,
	)
	defer cancel()

	if err := handler.agentStore.Upsert(ctx, agent.UpsertParams{
		InstanceUID: uid,
		Connected:   false,
	}); err != nil {
		handler.logger.ErrorContext(
			ctx,
			"mark OpAMP agent disconnected",
			"instance_uid",
			uid.String(),
			"error",
			err,
		)
	}
}

func (handler *connectionHandler) closeConnections() error {
	handler.mu.Lock()
	handler.closing = true
	connections := make(
		[]servertypes.Connection,
		0,
		len(handler.connections),
	)
	for connection := range handler.connections {
		connections = append(connections, connection)
	}
	handler.mu.Unlock()

	var closeErr error
	for _, connection := range connections {
		if err := connection.Disconnect(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}

	return closeErr
}

func (handler *connectionHandler) waitForConnections(
	ctx context.Context,
) error {
	done := make(chan struct{})
	go func() {
		handler.connectionsDone.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for connections: %w", ctx.Err())
	}
}
