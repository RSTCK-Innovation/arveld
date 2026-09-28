package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/web"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 5 * time.Second
)

type applicationServer struct {
	http  *http.Server
	opamp *opamp.Server
}

type serverDependencies struct {
	prometheusURL   string
	alertmanagerURL string
	agents          *agent.Store
	agentKeys       *agentauth.Store
	monitors        *monitor.Store
	alertRules      *alert.Store
	notifications   *notification.Store
	configs         *remoteconfig.Store
	auth            *auth.Store
	sessions        *auth.SessionStore
	logger          *slog.Logger
	readiness       httpapi.ReadinessChecker
}

func newApplicationServer(config Config, deps serverDependencies) (*applicationServer, error) {
	baseURL := deps.prometheusURL
	proxy, err := prometheus.NewOTLPMetricsProxy(baseURL, deps.logger, config.OTLPMetricsTimeout)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus OTLP proxy: %w", err)
	}
	client, err := prometheus.NewClient(baseURL, config.PrometheusQueryTimeout)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus client: %w", err)
	}
	silences, err := notification.NewSilenceClient(deps.alertmanagerURL)
	if err != nil {
		return nil, fmt.Errorf("create Alertmanager silence client: %w", err)
	}
	protocol, err := opamp.NewServer(deps.agents, deps.configs, deps.logger, deps.agentKeys)
	if err != nil {
		return nil, fmt.Errorf("create OpAMP server: %w", err)
	}
	handler := httpapi.NewHandler(httpapi.Config{
		Version:      config.Version,
		SecureCookie: config.SessionCookieSecure,
	}, httpapi.Dependencies{
		Agents:            deps.agents,
		AgentKeys:         deps.agentKeys,
		Monitors:          deps.monitors,
		AlertRules:        deps.alertRules,
		Notifications:     deps.notifications,
		Silences:          silences,
		Configs:           deps.configs,
		Auth:              deps.auth,
		Sessions:          deps.sessions,
		Prometheus:        client,
		Logger:            deps.logger,
		CheckReadiness:    deps.readiness,
		OpAMP:             protocol.Handler(),
		OTLPMetrics:       proxy,
		Frontend:          web.NewHandler(),
		NotifyAgentConfig: protocol.NotifyAgentConfig,
	})
	return &applicationServer{
		http: &http.Server{
			Handler:           handler,
			ConnContext:       protocol.ConnContext,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
		},
		opamp: protocol,
	}, nil
}

func (server *applicationServer) Shutdown(ctx context.Context) error {
	httpErr := server.http.Shutdown(ctx)
	if httpErr != nil {
		httpErr = fmt.Errorf("shut down HTTP server: %w", httpErr)
	}

	opampErr := server.opamp.Shutdown(ctx)
	if opampErr != nil {
		opampErr = fmt.Errorf("shut down OpAMP server: %w", opampErr)
	}

	return errors.Join(httpErr, opampErr)
}

func (server *applicationServer) Close() error {
	httpErr := server.http.Close()
	if httpErr != nil {
		httpErr = fmt.Errorf("close HTTP server: %w", httpErr)
	}

	opampErr := server.opamp.Close()
	if opampErr != nil {
		opampErr = fmt.Errorf("close OpAMP server: %w", opampErr)
	}

	return errors.Join(httpErr, opampErr)
}

// wait drains both transports before returning ownership of their stores to Run.
func (server *applicationServer) wait(ctx context.Context, serveErrors <-chan error, logger *slog.Logger) error {
	var serveErr error
	var servingStopped bool
	select {
	case serveErr = <-serveErrors:
		servingStopped = true
	case <-ctx.Done():
		logger.InfoContext(ctx, "stopping controller")
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}
	if !servingStopped {
		serveErr = <-serveErrors
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if serveErr != nil {
		serveErr = fmt.Errorf("serve HTTP: %w", serveErr)
	}

	return errors.Join(serveErr, shutdownErr)
}
