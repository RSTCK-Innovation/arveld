package opamp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	opampserver "github.com/open-telemetry/opamp-go/server"
	servertypes "github.com/open-telemetry/opamp-go/server/types"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

// Path is the HTTP path used by the OpAMP protocol.
const Path = "/v1/opamp"

// Server exposes OpAMP through an existing HTTP server.
type Server struct {
	handler           http.Handler
	connContext       opampserver.ConnContext
	connectionHandler *connectionHandler
}

// NewServer creates an OpAMP server backed by agent, configuration and key stores.
// The caller owns the stores and must supply them for authenticated requests.
func NewServer(
	agentStore *agent.Store,
	configStore *remoteconfig.Store,
	logger *slog.Logger,
	keyStore *agentauth.Store,
) (*Server, error) {
	connectionHandler := newConnectionHandler(
		agentStore,
		configStore,
		logger,
		keyStore,
		time.Now,
	)
	implementation := opampserver.New(&slogLogger{logger: logger})
	handler, connContext, err := implementation.Attach(opampserver.Settings{
		Callbacks: servertypes.Callbacks{
			OnConnecting: connectionHandler.acceptConnection,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("attach OpAMP server: %w", err)
	}

	return &Server{
		handler:           http.HandlerFunc(handler),
		connContext:       connContext,
		connectionHandler: connectionHandler,
	}, nil
}

// Handler returns the HTTP handler that accepts OpAMP messages.
func (server *Server) Handler() http.Handler {
	return server.handler
}

// ConnContext adds the network connection to a request context for OpAMP.
func (server *Server) ConnContext(
	ctx context.Context,
	connection net.Conn,
) context.Context {
	return server.connContext(ctx, connection)
}

// Close disconnects every active OpAMP connection.
func (server *Server) Close() error {
	if err := server.connectionHandler.closeConnections(); err != nil {
		return fmt.Errorf("disconnect OpAMP connections: %w", err)
	}

	return nil
}

// Shutdown disconnects every active OpAMP connection and waits for its
// connection-close callback to finish.
func (server *Server) Shutdown(ctx context.Context) error {
	closeErr := server.Close()
	waitErr := server.connectionHandler.waitForConnections(ctx)
	if waitErr != nil {
		waitErr = fmt.Errorf("wait for OpAMP connections: %w", waitErr)
	}

	return errors.Join(closeErr, waitErr)
}
