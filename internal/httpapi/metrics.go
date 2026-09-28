package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

// metricsParameters accepts legacy GET queries and bounded POST forms.
func metricsParameters(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	if r.Method != http.MethodPost {
		return r.URL.Query(), true
	}
	mediaType := r.Header.Get("Content-Type")
	if end := strings.IndexAny(mediaType, "; "); end >= 0 {
		mediaType = mediaType[:end]
	}
	if mediaType != "application/x-www-form-urlencoded" {
		fail(w, http.StatusUnsupportedMediaType)
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	if err := r.ParseForm(); err != nil {
		if oversized, _ := errors.AsType[*http.MaxBytesError](err); oversized != nil {
			fail(w, http.StatusRequestEntityTooLarge)
		} else {
			fail(w, http.StatusBadRequest)
		}
		return nil, false
	}
	return r.PostForm, true
}

func (api *api) queryMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parameters, ok := metricsParameters(w, r)
	if !ok {
		return
	}
	expression := parameters.Get("query")
	if expression == "" {
		fail(w, http.StatusBadRequest)
		return
	}
	upstream, err := api.prometheus.Query(ctx, expression, parameters.Get("lookback_delta"))
	if err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(ctx, "query Prometheus", "query_kind", "instant", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	defer func() {
		if err := upstream.Body.Close(); err != nil && ctx.Err() != context.Canceled {
			api.logger.ErrorContext(ctx, "close Prometheus query response", "query_kind", "instant", "error", err)
		}
	}()
	api.writeMetricsResponse(w, r, upstream, "instant")
}

func (api *api) queryRangeMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parameters, ok := metricsParameters(w, r)
	if !ok {
		return
	}
	query := prometheus.RangeQuery{
		Expression:    parameters.Get("query"),
		Start:         parameters.Get("start"),
		End:           parameters.Get("end"),
		Step:          parameters.Get("step"),
		LookbackDelta: parameters.Get("lookback_delta"),
	}
	if query.Expression == "" || query.Start == "" || query.End == "" || query.Step == "" {
		fail(w, http.StatusBadRequest)
		return
	}
	upstream, err := api.prometheus.QueryRange(ctx, query)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		api.logger.ErrorContext(ctx, "query Prometheus", "query_kind", "range", "error", err)
		fail(w, http.StatusBadGateway)
		return
	}
	defer func() {
		if err := upstream.Body.Close(); err != nil && ctx.Err() != context.Canceled {
			api.logger.ErrorContext(ctx, "close Prometheus query response", "query_kind", "range", "error", err)
		}
	}()
	api.writeMetricsResponse(w, r, upstream, "range")
}

// writeMetricsResponse streams the upstream body; the caller closes it.
func (api *api) writeMetricsResponse(w http.ResponseWriter, r *http.Request, upstream *http.Response, kind string) {
	ctx := r.Context()
	if contentType := upstream.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(upstream.StatusCode)
	if _, err := io.Copy(w, upstream.Body); err != nil && ctx.Err() != context.Canceled {
		api.logger.ErrorContext(ctx, "write Prometheus query response", "query_kind", kind, "error", err)
	}
}

func (api *api) receiveMetrics(w http.ResponseWriter, r *http.Request) {
	headers := r.Header.Values("Authorization")
	err := agentauth.ErrInvalidKey
	if len(headers) == 1 {
		parts := strings.Fields(headers[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			_, err = api.agentKeys.AuthenticateKey(r.Context(), parts[1])
		}
	}
	if errors.Is(err, agentauth.ErrInvalidKey) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, http.StatusUnauthorized)
		return
	}
	if err != nil {
		api.internalError(w, r, "authenticate agent key", err)
		return
	}
	api.otlp.ServeHTTP(w, r)
}
