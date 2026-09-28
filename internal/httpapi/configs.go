package httpapi

import (
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

const configMediaType = "application/x-yaml"

const maxConfigSizeBytes int64 = 1024 * 1024

type configRevisionResponse struct {
	Revision int64 `json:"revision"`
}

type configHistoryItem struct {
	Revision  int64      `json:"revision"`
	CreatedAt *time.Time `json:"created_at"`
}

func (api *api) listAgentConfigRevisions(w http.ResponseWriter, r *http.Request) {
	uid, err := agent.ParseInstanceUID(r.PathValue("instance_uid"))
	if err != nil {
		http.Error(w, "invalid instance UID", http.StatusBadRequest)
		return
	}
	revisions, err := api.configs.ListRevisions(r.Context(), uid)
	if err != nil {
		api.internalError(w, r, "list agent configuration revisions", err, slog.String("instance_uid", uid.String()))
		return
	}
	items := make([]configHistoryItem, 0, len(revisions))
	for _, revision := range revisions {
		items = append(items, configHistoryItem{Revision: revision.Number, CreatedAt: revision.CreatedAt})
	}
	writeJSON(w, http.StatusOK, struct {
		Revisions []configHistoryItem `json:"revisions"`
	}{Revisions: items})
}

func (api *api) agentConfigRevision(w http.ResponseWriter, r *http.Request) {
	uid, err := agent.ParseInstanceUID(r.PathValue("instance_uid"))
	if err != nil {
		http.Error(w, "invalid instance UID", http.StatusBadRequest)
		return
	}
	number, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || number < 1 {
		http.Error(w, "invalid revision", http.StatusBadRequest)
		return
	}
	content, err := api.configs.RevisionContent(r.Context(), uid, number)
	if errors.Is(err, remoteconfig.ErrRevisionNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read agent configuration revision", err, slog.String("instance_uid", uid.String()))
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", configMediaType)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", configMediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content) //nolint:errcheck,gosec // G705: YAML is served with application/x-yaml and nosniff; write failures cannot replace a committed response.
}

type desiredConfigStatusResponse struct {
	Revision   int64  `json:"revision"`
	ConfigHash string `json:"config_hash"`
}

type reportedConfigStatusResponse struct {
	Revision     *int64                   `json:"revision"`
	ConfigHash   string                   `json:"config_hash"`
	Status       remoteconfig.ApplyStatus `json:"status"`
	ErrorMessage string                   `json:"error_message"`
	ReportedAt   time.Time                `json:"reported_at"`
}

type configStatusResponse struct {
	State       remoteconfig.ApplyStatus      `json:"state"`
	Desired     desiredConfigStatusResponse   `json:"desired"`
	Reported    *reportedConfigStatusResponse `json:"reported"`
	LastFailure *reportedConfigStatusResponse `json:"last_failure,omitempty"`
	LastWorking *reportedConfigStatusResponse `json:"last_working,omitempty"`
}

func (api *api) putAgentConfig(w http.ResponseWriter, r *http.Request) {
	agentUID, err := agent.ParseInstanceUID(
		r.PathValue("instance_uid"),
	)
	if err != nil {
		http.Error(w, "invalid instance UID", http.StatusBadRequest)
		return
	}

	mediaType, _, err := mime.ParseMediaType(
		r.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != configMediaType {
		fail(w, http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConfigSizeBytes)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		if maxBytesError, _ := errors.AsType[*http.MaxBytesError](err); maxBytesError != nil {
			fail(w, http.StatusRequestEntityTooLarge)
			return
		}

		api.logger.ErrorContext(
			r.Context(),
			"read agent configuration",
			"instance_uid",
			agentUID.String(),
			"error",
			err,
		)
		fail(w, http.StatusBadRequest)
		return
	}

	revision, err := api.configs.Save(r.Context(), agentUID, content)
	if err != nil {
		api.internalError(w, r, "store agent configuration", err, slog.String("instance_uid", agentUID.String()))
		return
	}

	writeJSON(w, http.StatusOK, configRevisionResponse{
		Revision: revision.Number,
	})
}

func (api *api) rollbackAgentConfig(w http.ResponseWriter, r *http.Request) {
	agentUID, err := agent.ParseInstanceUID(
		r.PathValue("instance_uid"),
	)
	if err != nil {
		http.Error(w, "invalid instance UID", http.StatusBadRequest)
		return
	}

	revision, err := api.configs.Rollback(r.Context(), agentUID)
	if err != nil {
		if errors.Is(err, remoteconfig.ErrNoPreviousRevision) {
			fail(w, http.StatusConflict)
			return
		}

		api.internalError(w, r, "roll back agent configuration", err, slog.String("instance_uid", agentUID.String()))
		return
	}

	writeJSON(w, http.StatusOK, configRevisionResponse{
		Revision: revision.Number,
	})
}

func (api *api) agentConfigStatus(w http.ResponseWriter, r *http.Request) {
	agentUID, err := agent.ParseInstanceUID(
		r.PathValue("instance_uid"),
	)
	if err != nil {
		http.Error(w, "invalid instance UID", http.StatusBadRequest)
		return
	}

	status, err := api.configs.Status(r.Context(), agentUID)
	if errors.Is(err, remoteconfig.ErrNoDesiredRevision) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read remote configuration status", err, slog.String("instance_uid", agentUID.String()))
		return
	}
	writeJSON(w, http.StatusOK, configStatusResponse{
		State:    status.State,
		Desired:  desiredConfigStatusResponse{Revision: status.Desired.Number, ConfigHash: hex.EncodeToString(status.Desired.ConfigHash[:])},
		Reported: configReportResponse(status.Reported), LastFailure: configReportResponse(status.LastFailure),
		LastWorking: configReportResponse(status.LastWorking),
	})
}

func configReportResponse(report *remoteconfig.StatusReport) *reportedConfigStatusResponse {
	if report == nil {
		return nil
	}
	return &reportedConfigStatusResponse{Revision: report.Revision, ConfigHash: hex.EncodeToString(report.ConfigHash), Status: report.Status, ErrorMessage: report.ErrorMessage, ReportedAt: report.ReportedAt}
}
