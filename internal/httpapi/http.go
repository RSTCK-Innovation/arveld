package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"unicode/utf8"
)

const maxJSONBodySize int64 = 4 * 1024

// decodeJSONRequest owns its body limit and strict decoding options.
func decodeJSONRequest(w http.ResponseWriter, r *http.Request, target any) int {
	return decodeJSONRequestLimit(w, r, target, maxJSONBodySize)
}

func decodeJSONRequestLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) int {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return http.StatusUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if oversized, _ := errors.AsType[*http.MaxBytesError](err); oversized != nil {
			return http.StatusRequestEntityTooLarge
		}
		return http.StatusBadRequest
	}
	if !utf8.Valid(body) {
		return http.StatusBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return http.StatusBadRequest
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return http.StatusBadRequest
	}
	return 0
}

func (api *api) internalError(w http.ResponseWriter, r *http.Request, message string, err error, attributes ...slog.Attr) {
	if errors.Is(err, context.Canceled) && r.Context().Err() == context.Canceled {
		return
	}
	attributes = append(attributes, slog.String("operation", message), slog.Any("error", err))
	api.logger.LogAttrs(r.Context(), slog.LevelError, "http operation failed", attributes...)
	fail(w, http.StatusInternalServerError)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	fail(w, http.StatusTooManyRequests)
}

func fail(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		fail(w, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n')) //nolint:errcheck // A disconnected client cannot receive a replacement response.
}
