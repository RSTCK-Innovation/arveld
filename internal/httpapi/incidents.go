package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

func (api *api) listIncidents(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	query := alert.IncidentQuery{Status: values.Get("status"), Severity: values.Get("severity"), OwnerKind: values.Get("owner_kind"), OwnerID: values.Get("owner_id"), Limit: 50}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity)
			return
		}
		query.Limit = limit
	}
	if value := values.Get("before"); value != "" {
		before, err := strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			fail(w, http.StatusUnprocessableEntity)
			return
		}
		query.Before = before
	}
	page, err := api.alertRules.Incidents(r.Context(), query)
	if errors.Is(err, alert.ErrInvalidIncidentQuery) {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		api.internalError(w, r, "list incidents", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (api *api) readIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusNotFound)
		return
	}
	value, err := api.alertRules.Incident(r.Context(), id)
	if errors.Is(err, alert.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read incident", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *api) acknowledgeIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusNotFound)
		return
	}
	actor := "Management API key"
	if administrator, ok := r.Context().Value(administratorContextKey{}).(auth.Administrator); ok {
		actor = administrator.Name
	}
	value, err := api.alertRules.AcknowledgeIncident(r.Context(), id, actor)
	switch {
	case errors.Is(err, alert.ErrNotFound):
		fail(w, http.StatusNotFound)
	case errors.Is(err, alert.ErrIncidentClosed):
		fail(w, http.StatusConflict)
	case err != nil:
		api.internalError(w, r, "acknowledge incident", err)
	default:
		writeJSON(w, http.StatusOK, value)
	}
}
