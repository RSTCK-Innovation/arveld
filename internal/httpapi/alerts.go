package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// alertRuleRequest excludes server-owned identity and resource ownership.
type alertRuleRequest struct {
	Condition       string   `json:"condition"`
	Threshold       *float64 `json:"threshold,omitempty"`
	ForSeconds      int      `json:"for_seconds"`
	Severity        string   `json:"severity"`
	NotificationIDs []string `json:"notification_ids"`
}

type alertRuleResponse struct {
	ID               string   `json:"id"`
	MonitorID        string   `json:"monitor_id,omitempty"`
	AgentInstanceUID string   `json:"agent_instance_uid,omitempty"`
	Condition        string   `json:"condition"`
	Threshold        *float64 `json:"threshold,omitempty"`
	ForSeconds       int      `json:"for_seconds"`
	Severity         string   `json:"severity"`
	NotificationIDs  []string `json:"notification_ids"`
}

func (api *api) createAlertRule(w http.ResponseWriter, r *http.Request) {
	var input alertRuleRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	value := alert.Rule{
		ID: rand.Text(), MonitorID: r.PathValue("id"),
		Condition: input.Condition, Threshold: input.Threshold, ForSeconds: input.ForSeconds, Severity: input.Severity, NotificationIDs: input.NotificationIDs,
	}
	if text := r.PathValue("instance_uid"); text != "" {
		uid, err := agent.ParseInstanceUID(text)
		if err != nil {
			fail(w, http.StatusNotFound)
			return
		}
		value.AgentInstanceUID = &uid
	}
	if err := api.alertRules.Create(r.Context(), value); err != nil {
		switch {
		case errors.Is(err, alert.ErrInvalidRule):
			fail(w, http.StatusUnprocessableEntity)
		case errors.Is(err, alert.ErrMonitorNotFound), errors.Is(err, alert.ErrAgentNotFound):
			fail(w, http.StatusNotFound)
		default:
			api.internalError(w, r, "create alert rule", err)
		}
		return
	}
	w.Header().Set("Location", "/api/v1/alert-rules/"+value.ID)
	writeJSON(w, http.StatusCreated, alertRuleJSON(value))
}

func (api *api) readAlertRule(w http.ResponseWriter, r *http.Request) {
	value, err := api.alertRules.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, alert.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read alert rule", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, alertRuleJSON(value))
}

func alertRuleJSON(value alert.Rule) alertRuleResponse {
	response := alertRuleResponse{
		ID: value.ID, MonitorID: value.MonitorID,
		Condition: value.Condition, Threshold: value.Threshold, ForSeconds: value.ForSeconds, Severity: value.Severity, NotificationIDs: append([]string{}, slices.Sorted(slices.Values(value.NotificationIDs))...),
	}
	if value.AgentInstanceUID != nil {
		response.AgentInstanceUID = value.AgentInstanceUID.String()
	}
	return response
}

func (api *api) readAlertRuleState(w http.ResponseWriter, r *http.Request) {
	state, err := alert.ReadState(r.Context(), api.alertRules, api.prometheus, r.PathValue("id"))
	if errors.Is(err, alert.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		if r.Context().Err() == context.Canceled {
			return
		}
		if errors.Is(err, alert.ErrEngine) {
			api.logger.ErrorContext(r.Context(), "read native alert rule state", "error", err)
			fail(w, http.StatusBadGateway)
		} else {
			api.internalError(w, r, "read alert rule state", err)
		}
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (api *api) listAlertRules(w http.ResponseWriter, r *http.Request) {
	var values []alert.Rule
	var err error
	if text := r.PathValue("instance_uid"); text != "" {
		uid, parseErr := agent.ParseInstanceUID(text)
		if parseErr != nil {
			fail(w, http.StatusNotFound)
			return
		}
		values, err = api.alertRules.ListAgent(r.Context(), uid)
	} else {
		_, err = api.monitors.Get(r.Context(), r.PathValue("id"))
		if err == nil {
			values, err = api.alertRules.List(r.Context(), r.PathValue("id"))
		}
	}
	if errors.Is(err, monitor.ErrNotFound) || errors.Is(err, alert.ErrAgentNotFound) {
		fail(w, http.StatusNotFound)
		return
	}

	if err != nil {
		api.internalError(w, r, "list alert rules", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	response := make([]alertRuleResponse, 0, len(values))
	for _, value := range values {
		response = append(response, alertRuleJSON(value))
	}
	writeJSON(w, http.StatusOK, struct {
		Rules []alertRuleResponse `json:"rules"`
	}{Rules: response})
}

func (api *api) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	var input alertRuleRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	value, err := api.alertRules.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, alert.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read alert rule", err)
		return
	}
	value.Condition, value.Threshold, value.ForSeconds, value.Severity, value.NotificationIDs = input.Condition, input.Threshold, input.ForSeconds, input.Severity, input.NotificationIDs
	if err := api.alertRules.Update(r.Context(), value); err != nil {
		switch {
		case errors.Is(err, alert.ErrInvalidRule):
			fail(w, http.StatusUnprocessableEntity)
		case errors.Is(err, alert.ErrNotFound):
			fail(w, http.StatusNotFound)
		default:
			api.internalError(w, r, "update alert rule", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, alertRuleJSON(value))
}

func (api *api) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	err := api.alertRules.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, alert.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "delete alert rule", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
