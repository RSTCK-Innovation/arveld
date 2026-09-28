package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
)

func (api *api) createSilence(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DurationSeconds int        `json:"duration_seconds"`
		StartsAt        *time.Time `json:"starts_at"`
		Comment         string     `json:"comment"`
	}
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	input.Comment = strings.TrimSpace(input.Comment)
	if input.DurationSeconds < 1 || input.DurationSeconds > 7*24*60*60 ||
		input.Comment == "" || len(input.Comment) > 1024 || strings.ContainsRune(input.Comment, '\x00') {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	monitorID := r.PathValue("id")
	agentUID := r.PathValue("instance_uid")
	if agentUID != "" {
		uid, err := agent.ParseInstanceUID(agentUID)
		if err != nil {
			fail(w, http.StatusNotFound)
			return
		}
		exists, err := api.agents.Exists(r.Context(), uid)
		if err != nil {
			api.internalError(w, r, "read silence Agent", err)
			return
		}
		if !exists {
			fail(w, http.StatusNotFound)
			return
		}
		agentUID = uid.String()
	} else if _, err := api.monitors.Get(r.Context(), monitorID); err != nil {
		if errors.Is(err, monitor.ErrNotFound) {
			fail(w, http.StatusNotFound)
		} else {
			api.internalError(w, r, "read silence Monitor", err)
		}
		return
	}
	// Database contention must not consume an immediate silence's duration.
	startsAt := time.Now().UTC()
	if input.StartsAt != nil {
		if !input.StartsAt.After(startsAt) {
			fail(w, http.StatusUnprocessableEntity)
			return
		}
		startsAt = input.StartsAt.UTC()
	}
	endsAt := startsAt.Add(time.Duration(input.DurationSeconds) * time.Second)
	if endsAt.Year() > 9999 {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	var id string
	var err error
	if agentUID != "" {
		id, err = api.silences.CreateAgentSilence(r.Context(), agentUID, startsAt, endsAt, input.Comment)
	} else {
		id, err = api.silences.CreateMonitorSilence(r.Context(), monitorID, startsAt, endsAt, input.Comment)
	}
	if err != nil {
		if r.Context().Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(r.Context(), "create notification silence", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		ID               string `json:"id"`
		MonitorID        string `json:"monitor_id,omitempty"`
		AgentInstanceUID string `json:"agent_instance_uid,omitempty"`
	}{ID: id, MonitorID: monitorID, AgentInstanceUID: agentUID})
}

func (api *api) listSilences(w http.ResponseWriter, r *http.Request) {
	var values []notification.Silence
	var err error
	monitorID := r.PathValue("id")
	if monitorID != "" {
		if _, err := api.monitors.Get(r.Context(), monitorID); err != nil {
			if errors.Is(err, monitor.ErrNotFound) {
				fail(w, http.StatusNotFound)
			} else {
				api.internalError(w, r, "read silence Monitor", err)
			}
			return
		}
		values, err = api.silences.ListMonitorSilences(r.Context(), monitorID)
	} else if text := r.PathValue("instance_uid"); text != "" {
		uid, parseErr := agent.ParseInstanceUID(text)
		if parseErr != nil {
			fail(w, http.StatusNotFound)
			return
		}
		exists, readErr := api.agents.Exists(r.Context(), uid)
		if readErr != nil {
			api.internalError(w, r, "read silence Agent", readErr)
			return
		}
		if !exists {
			fail(w, http.StatusNotFound)
			return
		}
		values, err = api.silences.ListAgentSilences(r.Context(), uid.String())
	} else {
		values, err = api.silences.ListSilences(r.Context())
	}
	if err != nil {
		if r.Context().Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(r.Context(), "list notification silences", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Silences []notification.Silence `json:"silences"`
	}{Silences: values})
}

func (api *api) cancelSilence(w http.ResponseWriter, r *http.Request) {
	var err error
	if agentUID := r.PathValue("instance_uid"); agentUID != "" {
		uid, parseErr := agent.ParseInstanceUID(agentUID)
		if parseErr != nil {
			fail(w, http.StatusNotFound)
			return
		}
		exists, readErr := api.agents.Exists(r.Context(), uid)
		if readErr != nil {
			api.internalError(w, r, "read silence Agent", readErr)
			return
		}
		if !exists {
			fail(w, http.StatusNotFound)
			return
		}
		err = api.silences.CancelAgentSilence(r.Context(), uid.String(), r.PathValue("silence_id"))
	} else {
		monitorID := r.PathValue("id")
		if _, readErr := api.monitors.Get(r.Context(), monitorID); readErr != nil {
			if errors.Is(readErr, monitor.ErrNotFound) {
				fail(w, http.StatusNotFound)
			} else {
				api.internalError(w, r, "read silence Monitor", readErr)
			}
			return
		}
		err = api.silences.CancelMonitorSilence(r.Context(), monitorID, r.PathValue("silence_id"))
	}
	if errors.Is(err, notification.ErrSilenceNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		if r.Context().Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(r.Context(), "cancel notification silence", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
