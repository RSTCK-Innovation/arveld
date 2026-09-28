package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// monitorRoutes binds an explicit protocol, or the complete inventory when empty.
type monitorRoutes struct {
	api      *api
	protocol string
}

func (route monitorRoutes) create(w http.ResponseWriter, r *http.Request) {
	api := route.api
	input, status := decodeMonitorRequest(w, r, route.protocol)
	if status != 0 {
		fail(w, status)
		return
	}
	value, err := monitor.Validate(input)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	value.ID = rand.Text()
	if err := api.monitors.Create(r.Context(), value); err != nil {
		if errors.Is(err, monitor.ErrAgentNotFound) {
			fail(w, http.StatusUnprocessableEntity)
			return
		}
		api.internalError(w, r, "create Monitor", err)
		return
	}
	// Creation is already committed. Publication or delivery failure must not
	// turn a successful creation into an error that invites duplicate retries.
	api.reconcileMonitorAgent(r.Context(), value.AgentInstanceUID)
	w.Header().Set("Location", "/api/v1/monitors/"+route.protocol+"/"+value.ID)
	writeJSON(w, http.StatusCreated, route.response(value))
}

func (route monitorRoutes) list(w http.ResponseWriter, r *http.Request) {
	values, err := route.api.monitors.List(r.Context())
	if err != nil {
		route.api.internalError(w, r, "list Monitors", err)
		return
	}
	response := make([]monitorResponse, 0, len(values))
	for _, value := range values {
		if route.protocol == "" || value.Protocol == route.protocol {
			response = append(response, route.response(value))
		}
	}
	writeMonitorJSON(w, r, struct {
		Monitors []monitorResponse `json:"monitors"`
	}{Monitors: response})
}

func (route monitorRoutes) read(w http.ResponseWriter, r *http.Request) {
	value, err := route.api.monitors.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, monitor.ErrNotFound) || err == nil && route.protocol != "" && value.Protocol != route.protocol {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		route.api.internalError(w, r, "read Monitor", err)
		return
	}
	writeMonitorJSON(w, r, route.response(value))
}

func (route monitorRoutes) delete(w http.ResponseWriter, r *http.Request) {
	api := route.api
	value, err := api.monitors.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, monitor.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "delete Monitor", err)
		return
	}
	// The deletion is durable even when publication or delivery must be retried.
	api.reconcileMonitorAgent(r.Context(), value.AgentInstanceUID)
	w.WriteHeader(http.StatusNoContent)
}

func (route monitorRoutes) update(w http.ResponseWriter, r *http.Request) {
	api := route.api
	stored, err := api.monitors.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, monitor.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read Monitor for update", err)
		return
	}
	input, status := decodeMonitorRequest(w, r, stored.Protocol)
	if status != 0 {
		fail(w, status)
		return
	}
	value, err := monitor.Validate(input)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	value.ID = stored.ID
	previous, err := api.monitors.Update(r.Context(), value)
	switch {
	case errors.Is(err, monitor.ErrNotFound):
		fail(w, http.StatusNotFound)
		return
	case errors.Is(err, monitor.ErrAgentNotFound):
		fail(w, http.StatusUnprocessableEntity)
		return
	case err != nil:
		api.internalError(w, r, "update Monitor", err)
		return
	}
	api.reconcileMonitorAgent(r.Context(), previous)
	if previous != value.AgentInstanceUID {
		api.reconcileMonitorAgent(r.Context(), value.AgentInstanceUID)
	}
	writeJSON(w, http.StatusOK, route.response(value))
}

func writeMonitorJSON(w http.ResponseWriter, r *http.Request, value any) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

// reconcileMonitorAgent publishes the latest persisted set after a product write.
// Failures do not undo committed intent; a later Agent message retries convergence.
func (api *api) reconcileMonitorAgent(ctx context.Context, uid agent.InstanceUID) {
	if err := api.configs.ReconcileAgent(ctx, uid); err != nil {
		api.logger.ErrorContext(ctx, "reconcile Monitor change", "instance_uid", uid.String(), "error", err)
		return
	}
	if err := api.notifyAgentConfig(ctx, uid); err != nil {
		api.logger.ErrorContext(ctx, "notify Agent configuration", "instance_uid", uid.String(), "error", err)
	}
}

// createMonitorRequest holds fields common to the existing creation contracts.
type createMonitorRequest struct {
	Name             string `json:"name"`
	AgentInstanceUID string `json:"agent_instance_uid"`
	Endpoint         string `json:"endpoint"`
	IntervalSeconds  int    `json:"interval_seconds"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
}

// decodeMonitorRequest preserves each route's strict JSON shape. The protocol
// comes from the registered route, never from caller-controlled JSON.
func decodeMonitorRequest(w http.ResponseWriter, r *http.Request, protocol string) (monitor.Monitor, int) {
	var common createMonitorRequest
	value := monitor.Monitor{Protocol: protocol}
	limit := maxJSONBodySize
	if protocol == "http" {
		limit = 512 * 1024
	}
	switch protocol {
	case "http":
		input := struct {
			*createMonitorRequest
			Method string `json:"method"`
			monitor.HTTPOptions
			SkipTLSVerify bool `json:"skip_tls_verify"`
		}{createMonitorRequest: &common}
		if status := decodeJSONRequestLimit(w, r, &input, limit); status != 0 {
			return monitor.Monitor{}, status
		}
		value.Method = input.Method
		value.HTTPOptions = input.HTTPOptions
		value.SkipTLSVerify = input.SkipTLSVerify
	case "tcp":
		if status := decodeJSONRequest(w, r, &common); status != 0 {
			return monitor.Monitor{}, status
		}
	case "icmp":
		input := struct {
			*createMonitorRequest
			PingCount int `json:"ping_count"`
		}{createMonitorRequest: &common}
		if status := decodeJSONRequestLimit(w, r, &input, limit); status != 0 {
			return monitor.Monitor{}, status
		}
		value.PingCount = input.PingCount
	case "dns":
		input := struct {
			*createMonitorRequest
			DNSServer  string `json:"dns_server"`
			RecordType string `json:"record_type"`
			Transport  string `json:"transport"`
		}{createMonitorRequest: &common}
		if status := decodeJSONRequestLimit(w, r, &input, limit); status != 0 {
			return monitor.Monitor{}, status
		}
		value.DNSServer = input.DNSServer
		value.RecordType, value.Transport = input.RecordType, input.Transport
	}
	uid, err := agent.ParseInstanceUID(common.AgentInstanceUID)
	if err != nil {
		return monitor.Monitor{}, http.StatusUnprocessableEntity
	}
	value.Name, value.AgentInstanceUID = common.Name, uid
	value.Endpoint = common.Endpoint
	value.IntervalSeconds, value.TimeoutSeconds = common.IntervalSeconds, common.TimeoutSeconds
	return value, 0
}

type monitorResponse struct {
	monitor.HTTPOptions
	SkipTLSVerify    bool   `json:"skip_tls_verify,omitempty"`
	ID               string `json:"id"`
	Protocol         string `json:"protocol,omitempty"`
	Name             string `json:"name"`
	AgentInstanceUID string `json:"agent_instance_uid"`
	Endpoint         string `json:"endpoint"`
	IntervalSeconds  int    `json:"interval_seconds"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	Method           string `json:"method,omitempty"`
	PingCount        int    `json:"ping_count,omitempty"`
	DNSServer        string `json:"dns_server,omitempty"`
	RecordType       string `json:"record_type,omitempty"`
	Transport        string `json:"transport,omitempty"`
}

func (route monitorRoutes) response(value monitor.Monitor) monitorResponse {
	response := monitorResponse{
		HTTPOptions: value.HTTPOptions, SkipTLSVerify: value.SkipTLSVerify,
		ID: value.ID, Protocol: value.Protocol, Name: value.Name, AgentInstanceUID: value.AgentInstanceUID.String(),
		Endpoint: value.Endpoint, Method: value.Method, IntervalSeconds: value.IntervalSeconds, TimeoutSeconds: value.TimeoutSeconds,
		PingCount: value.PingCount, DNSServer: value.DNSServer, RecordType: value.RecordType, Transport: value.Transport,
	}
	// Existing /monitors/http responses predate the protocol field.
	if route.protocol == "http" {
		response.Protocol = ""
	}
	return response
}
