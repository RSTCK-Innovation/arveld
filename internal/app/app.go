package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/database"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

// ComponentRuntime supplies the two supervised processes to the controller.
// URLs are HTTP origins. Run owns both processes until ctx is canceled and
// closes ready once both are initially ready, then waits for their shutdown.
// This is an internal Go dependency, not a user-selectable deployment mode.
type ComponentRuntime struct {
	PrometheusURL   string
	AlertmanagerURL string
	Run             func(ctx context.Context, ready chan<- struct{}) error
}

// Run starts the controller and blocks until it stops or ctx is canceled.
func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	return RunWithComponents(
		ctx,
		config,
		logger,
		ComponentRuntime{
			PrometheusURL:   components.ManagedPrometheusURL,
			AlertmanagerURL: components.ManagedAlertmanagerURL,
			Run: func(ctx context.Context, ready chan<- struct{}) error {
				supervisor := components.NewSupervisor(
					components.SupervisorConfig{
						DataDirectory:   filepath.Dir(config.DatabasePath),
						DownloadTimeout: config.ComponentDownloadTimeout,
						Prometheus: components.PrometheusConfig{
							RetentionTime: config.PrometheusRetentionTime,
							RetentionSize: config.PrometheusRetentionSize,
						},
					},
					logger,
				)

				return supervisor.RunManaged(ctx, ready)
			},
		},
	)
}

// RunWithComponents runs the controller with explicit process dependencies.
// Run assembles the production runtime; integration tests supply isolated engines.
// Component supervision is always started and awaited during shutdown.
func RunWithComponents(
	ctx context.Context,
	config Config,
	logger *slog.Logger,
	runtime ComponentRuntime,
) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if ctx.Err() != nil {
		return nil
	}
	if runtime.Run == nil {
		return errors.New("component runner is required")
	}

	db, err := database.OpenFile(ctx, config.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.ErrorContext(ctx, "close database", "error", err)
		}
	}()
	if err := database.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	agentStore := agent.NewStore(db)
	agentKeyStore := agentauth.NewStore(db)
	monitorStore := monitor.NewStore(db)
	alertRuleStore := alert.NewStore(db)
	notificationStore := notification.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	authStore := auth.NewStore(db)
	sessionStore := auth.NewSessionStore(db)

	// Reset all agent connection states to disconnected on startup.
	// This ensures that the controller does not assume any agent is connected after a restart.
	if err := agentStore.MarkAllDisconnected(ctx); err != nil {
		return fmt.Errorf("reset agent connection states: %w", err)
	}

	logger.InfoContext(ctx, "database ready", "path", config.DatabasePath)
	managedState := &managedComponentsState{}
	checkReadiness := newReadinessChecker(runtime, managedState)
	client, err := prometheus.NewClient(runtime.PrometheusURL, config.PrometheusQueryTimeout)
	if err != nil {
		return fmt.Errorf("create alert publication client: %w", err)
	}
	publisher := alert.NewPublisher(alertRuleStore, client, filepath.Dir(config.DatabasePath))
	if err := publisher.Prepare(); err != nil {
		return fmt.Errorf("prepare alert publication: %w", err)
	}
	notificationPublisher, err := notification.NewPublisher(notificationStore, runtime.AlertmanagerURL, filepath.Dir(config.DatabasePath))
	if err != nil {
		return fmt.Errorf("create notification publisher: %w", err)
	}
	notificationConfig, err := notificationStore.AlertmanagerConfig(ctx)
	if err != nil {
		return fmt.Errorf("prepare notification configuration: %w", err)
	}
	if _, err := components.WriteAlertmanagerConfig(filepath.Dir(config.DatabasePath), notificationConfig); err != nil {
		return fmt.Errorf("write notification configuration: %w", err)
	}
	server, err := newApplicationServer(config, serverDependencies{
		prometheusURL:   runtime.PrometheusURL,
		alertmanagerURL: runtime.AlertmanagerURL,
		agents:          agentStore,
		agentKeys:       agentKeyStore,
		monitors:        monitorStore,
		alertRules:      alertRuleStore,
		notifications:   notificationStore,
		configs:         configStore,
		auth:            authStore,
		sessions:        sessionStore,
		logger:          logger,
		readiness:       checkReadiness,
	})
	if err != nil {
		return fmt.Errorf("create HTTP server: %w", err)
	}

	// Also release a constructed server if opening the listener fails.
	defer func() {
		if err := server.Close(); err != nil {
			logger.ErrorContext(ctx, "close application server", "error", err)
		}
	}()

	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(ctx, "tcp", config.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.HTTPAddress, err)
	}

	stopCleanupAndWait := startSessionCleanup(ctx, sessionStore, logger)
	defer stopCleanupAndWait()

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.http.Serve(listener)
	}()

	logger.InfoContext(
		ctx,
		"controller started",
		"http_address",
		listener.Addr().String(),
	)
	stopManagedAndWait := startManagedComponents(
		ctx,
		logger,
		runtime.Run,
		managedState,
	)
	defer stopManagedAndWait()

	stopPublicationAndWait := startAlertPublication(ctx, publisher, alertRuleStore, client, managedState, logger)
	defer stopPublicationAndWait()
	stopNotificationPublicationAndWait := startNotificationPublication(ctx, notificationPublisher, managedState, logger)
	defer stopNotificationPublicationAndWait()

	return server.wait(ctx, serveErrors, logger)
}
