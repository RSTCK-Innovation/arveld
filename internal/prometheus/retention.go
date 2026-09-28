package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Retention reads the running engine's native retention summary.
// An empty summary means that neither a time nor a size limit is enabled.
func (client *Client) Retention(ctx context.Context) (string, error) {
	endpoint, err := url.JoinPath(client.baseURL, "/api/v1/status/runtimeinfo")
	if err != nil {
		return "", fmt.Errorf("build Prometheus runtime endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create Prometheus runtime request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("execute Prometheus runtime request: %w", err)
	}
	const limit = 64 * 1024
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return "", fmt.Errorf("read Prometheus runtime response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(body) > limit {
		return "", fmt.Errorf("unsuccessful or oversized Prometheus runtime response (HTTP %d)", response.StatusCode)
	}
	var envelope struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			StorageRetention *string `json:"storageRetention"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Status != "success" ||
		len(envelope.Warnings) != 0 || envelope.Data.StorageRetention == nil {
		return "", errors.New("invalid, unsuccessful or partial Prometheus retention response")
	}
	return *envelope.Data.StorageRetention, nil
}
