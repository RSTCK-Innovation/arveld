package httpapi

import (
	"context"
	"net/http"
)

func (api *api) readRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	retention, err := api.prometheus.Retention(ctx)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(ctx, "read Prometheus retention", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		StorageRetention string `json:"storage_retention"`
	}{StorageRetention: retention})
}
