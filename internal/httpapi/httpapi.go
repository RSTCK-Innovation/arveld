// Package httpapi exposes the controller HTTP API with standard net/http handlers and chi.
package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

// Config contains the controller version and transport settings for this router instance.
type Config struct {
	Version      string
	SecureCookie bool
}

// Dependencies names the caller-owned stores, clients and protocol handlers.
// OpAMP and OTLPMetrics may be nil when those transports are not being assembled.
// AgentKeys is required by agent-key management and OTLP admission.
// All dependencies for registered management operations must be supplied by the caller.
type Dependencies struct {
	Agents         *agent.Store
	AgentKeys      *agentauth.Store
	Monitors       *monitor.Store
	AlertRules     *alert.Store
	Notifications  *notification.Store
	Silences       *notification.SilenceClient
	Configs        *remoteconfig.Store
	Auth           *auth.Store
	Sessions       SessionStore
	Prometheus     *prometheus.Client
	Logger         *slog.Logger
	CheckReadiness ReadinessChecker
	OpAMP          http.Handler
	OTLPMetrics    http.Handler
	// Frontend optionally serves unmatched browser paths; service prefixes stay reserved.
	Frontend http.Handler
	// NotifyAgentConfig delivers a committed target to live Agents.
	// It is required by Monitor creation, update and deletion.
	NotifyAgentConfig func(context.Context, agent.InstanceUID) error
}

// api holds dependencies and admission state shared by one router's handlers.
// Decoded input stays local; only the authenticated account is cached per request.
// The caller owns stores and their lifecycle.
type api struct {
	version           string
	agents            *agent.Store
	agentKeys         *agentauth.Store
	monitors          *monitor.Store
	alertRules        *alert.Store
	notifications     *notification.Store
	silences          *notification.SilenceClient
	configs           *remoteconfig.Store
	notifyAgentConfig func(context.Context, agent.InstanceUID) error
	auth              *auth.Store
	browser           *browserSessions
	prometheus        *prometheus.Client
	logger            *slog.Logger
	checkReadiness    ReadinessChecker
	origins           *http.CrossOriginProtection
	setupAdmission    *admissionGate
	loginAdmission    *admissionGate
	passwordAdmission *admissionGate
	otlp              http.Handler
}

// NewHandler assembles the router in one operation. The caller owns dependencies.
// Access policy (also applied to 404/405):
//
//	/healthz, /readyz, auth/setup, auth/login, auth/logout: public, with origin checks on auth writes.
//	auth/session, account/*, apikeys*, agentkeys*: browser session; account/key access also checks origins.
//	other /api/v1/*: browser session or an explicit management API key.
//	/v1/opamp, /v1/otlp/v1/metrics: separate agent credentials.
func NewHandler(config Config, deps Dependencies) http.Handler {
	api := &api{
		version:           config.Version,
		agents:            deps.Agents,
		agentKeys:         deps.AgentKeys,
		monitors:          deps.Monitors,
		alertRules:        deps.AlertRules,
		notifications:     deps.Notifications,
		silences:          deps.Silences,
		configs:           deps.Configs,
		notifyAgentConfig: deps.NotifyAgentConfig,
		auth:              deps.Auth,
		browser:           &browserSessions{store: deps.Sessions, secure: config.SecureCookie},
		prometheus:        deps.Prometheus,
		logger:            deps.Logger,
		checkReadiness:    deps.CheckReadiness,
		origins:           http.NewCrossOriginProtection(),
		otlp:              deps.OTLPMetrics,
		setupAdmission:    newAdmission(0),
		loginAdmission:    newAdmission(time.Second),
		passwordAdmission: newAdmission(time.Second),
	}
	router := chi.NewRouter()
	router.Use(api.requireManagementAccess)
	csrf := protectOrigins(api.origins)

	router.Get("/healthz", healthz)
	router.Head("/healthz", healthz)
	router.Get("/readyz", api.readyz)
	router.Head("/readyz", api.readyz)
	router.Get("/api/v1/settings/retention", api.readRetention)
	router.Head("/api/v1/settings/retention", api.readRetention)
	authentication := router.With(noStore)
	authentication.Get("/api/v1/auth/setup", api.setupStatus)
	authentication.Head("/api/v1/auth/setup", api.setupStatus)
	authentication.With(csrf).Post("/api/v1/auth/setup", api.createAdministrator)
	authentication.With(csrf).Post("/api/v1/auth/login", api.login)
	authentication.Get("/api/v1/auth/session", api.currentSession)
	authentication.Head("/api/v1/auth/session", api.currentSession)
	authentication.With(csrf).Post("/api/v1/auth/logout", api.logout)

	router.Get("/api/v1/apikeys", api.listAPIKeys)
	router.Head("/api/v1/apikeys", api.listAPIKeys)
	router.Post("/api/v1/apikeys", api.createAPIKey)
	router.Delete("/api/v1/apikeys/{id}", api.revokeAPIKey)
	router.Get("/api/v1/agentkeys", api.listAgentKeys)
	router.Head("/api/v1/agentkeys", api.listAgentKeys)
	router.Post("/api/v1/agentkeys", api.createAgentKey)
	router.Delete("/api/v1/agentkeys/{id}", api.revokeAgentKey)
	router.Put("/api/v1/account/profile", api.updateProfile)
	router.Put("/api/v1/account/password", api.changePassword)
	router.Get("/api/v1/agents", api.listAgents)
	router.Head("/api/v1/agents", api.listAgents)
	for _, protocol := range []string{"http", "tcp", "icmp", "dns"} {
		routes := monitorRoutes{api: api, protocol: protocol}
		router.Post("/api/v1/monitors/"+protocol, routes.create)
		router.Get("/api/v1/monitors/"+protocol, routes.list)
		router.Head("/api/v1/monitors/"+protocol, routes.list)
		router.Get("/api/v1/monitors/"+protocol+"/{id}", routes.read)
		router.Head("/api/v1/monitors/"+protocol+"/{id}", routes.read)
	}
	monitors := monitorRoutes{api: api}
	router.Get("/api/v1/monitors", monitors.list)
	router.Head("/api/v1/monitors", monitors.list)
	router.Get("/api/v1/monitors/{id}", monitors.read)
	router.Head("/api/v1/monitors/{id}", monitors.read)
	router.Delete("/api/v1/monitors/{id}", monitors.delete)
	router.Put("/api/v1/monitors/{id}", monitors.update)
	router.Post("/api/v1/monitors/{id}/alert-rules", api.createAlertRule)
	router.Post("/api/v1/agents/{instance_uid}/alert-rules", api.createAlertRule)
	router.Get("/api/v1/agents/{instance_uid}/alert-rules", api.listAlertRules)
	router.Head("/api/v1/agents/{instance_uid}/alert-rules", api.listAlertRules)
	router.Post("/api/v1/monitors/{id}/silences", api.createSilence)
	router.Post("/api/v1/agents/{instance_uid}/silences", api.createSilence)
	router.Get("/api/v1/monitors/{id}/silences", api.listSilences)
	router.Head("/api/v1/monitors/{id}/silences", api.listSilences)
	router.Get("/api/v1/agents/{instance_uid}/silences", api.listSilences)
	router.Head("/api/v1/agents/{instance_uid}/silences", api.listSilences)
	router.Delete("/api/v1/monitors/{id}/silences/{silence_id}", api.cancelSilence)
	router.Delete("/api/v1/agents/{instance_uid}/silences/{silence_id}", api.cancelSilence)
	router.Get("/api/v1/silences", api.listSilences)
	router.Head("/api/v1/silences", api.listSilences)
	router.Get("/api/v1/monitors/{id}/alert-rules", api.listAlertRules)
	router.Head("/api/v1/monitors/{id}/alert-rules", api.listAlertRules)
	router.Put("/api/v1/alert-rules/{id}", api.updateAlertRule)
	router.Delete("/api/v1/alert-rules/{id}", api.deleteAlertRule)
	router.Get("/api/v1/alert-rules/{id}", api.readAlertRule)
	router.Head("/api/v1/alert-rules/{id}", api.readAlertRule)
	router.Get("/api/v1/alert-rules/{id}/state", api.readAlertRuleState)
	router.Head("/api/v1/alert-rules/{id}/state", api.readAlertRuleState)
	router.Get("/api/v1/incidents", api.listIncidents)
	router.Head("/api/v1/incidents", api.listIncidents)
	router.Get("/api/v1/incidents/{id}", api.readIncident)
	router.Head("/api/v1/incidents/{id}", api.readIncident)
	router.Post("/api/v1/incidents/{id}/acknowledgment", api.acknowledgeIncident)
	router.Post("/api/v1/notifications", api.createNotification)
	router.Get("/api/v1/notifications", api.listNotifications)
	router.Head("/api/v1/notifications", api.listNotifications)
	router.Get("/api/v1/notifications/{id}", api.readNotification)
	router.Head("/api/v1/notifications/{id}", api.readNotification)
	router.Put("/api/v1/notifications/{id}", api.updateNotification)
	router.Delete("/api/v1/notifications/{id}", api.deleteNotification)
	router.Get("/api/v1/metrics/query", api.queryMetrics)
	router.Head("/api/v1/metrics/query", api.queryMetrics)
	router.Post("/api/v1/metrics/query", api.queryMetrics)
	router.Get("/api/v1/metrics/query_range", api.queryRangeMetrics)
	router.Head("/api/v1/metrics/query_range", api.queryRangeMetrics)
	router.Post("/api/v1/metrics/query_range", api.queryRangeMetrics)
	router.Put("/api/v1/agents/{instance_uid}/config", api.putAgentConfig)
	router.Post("/api/v1/agents/{instance_uid}/config/rollback", api.rollbackAgentConfig)
	router.Get("/api/v1/agents/{instance_uid}/config/revisions", api.listAgentConfigRevisions)
	router.Head("/api/v1/agents/{instance_uid}/config/revisions", api.listAgentConfigRevisions)
	router.Get("/api/v1/agents/{instance_uid}/config/revisions/{revision}", api.agentConfigRevision)
	router.Head("/api/v1/agents/{instance_uid}/config/revisions/{revision}", api.agentConfigRevision)
	router.Get("/api/v1/agents/{instance_uid}/config/status", api.agentConfigStatus)
	router.Head("/api/v1/agents/{instance_uid}/config/status", api.agentConfigStatus)

	if deps.OpAMP != nil {
		// Preserve the OpAMP transport's nine supported HTTP methods.
		for _, method := range []string{
			http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
			http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace,
		} {
			router.Method(method, "/v1/opamp", deps.OpAMP)
		}
	}
	if deps.OTLPMetrics != nil {
		router.Post("/v1/otlp/v1/metrics", api.receiveMetrics)
	}
	if deps.Frontend != nil {
		router.NotFound(func(w http.ResponseWriter, r *http.Request) {
			segment, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
			switch strings.ToLower(segment) {
			case "api", "v1", "healthz", "readyz":
				http.NotFound(w, r)
			default:
				deps.Frontend.ServeHTTP(w, r)
			}
		})
	}
	return router
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = io.WriteString(w, `{"status":"healthy"}`) //nolint:errcheck // A disconnected client cannot receive a replacement response.
}

// DependencyReadiness describes whether Arveld's observability dependencies are ready.
type DependencyReadiness struct {
	Prometheus   bool
	Alertmanager bool
}

// ReadinessChecker returns the current readiness of Arveld's dependencies.
type ReadinessChecker func(context.Context) DependencyReadiness

type componentReadinessResponse struct {
	Ready bool `json:"ready"`
}

type readinessComponentsResponse struct {
	Arveld       componentReadinessResponse `json:"arveld"`
	Prometheus   componentReadinessResponse `json:"prometheus"`
	Alertmanager componentReadinessResponse `json:"alertmanager"`
}

type readinessResponse struct {
	Version    string                      `json:"version"`
	Ready      bool                        `json:"ready"`
	Components readinessComponentsResponse `json:"components"`
}

func (api *api) readyz(w http.ResponseWriter, r *http.Request) {
	dependencies := api.checkReadiness(r.Context())
	current := readinessResponse{
		Version: api.version,
		Ready:   dependencies.Prometheus && dependencies.Alertmanager,
		Components: readinessComponentsResponse{
			Arveld: componentReadinessResponse{
				Ready: true,
			},
			Prometheus: componentReadinessResponse{
				Ready: dependencies.Prometheus,
			},
			Alertmanager: componentReadinessResponse{
				Ready: dependencies.Alertmanager,
			},
		},
	}

	status := http.StatusOK
	if !current.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, current)
}
