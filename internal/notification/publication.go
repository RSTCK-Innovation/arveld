package notification

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Alertmanager uses MD5 for a non-security configuration fingerprint.
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/RSTCK-Innovation/arveld/internal/components"
)

// Publisher reconciles stored channels with the owned Alertmanager configuration.
// The application owns its lifetime and calls Sync serially.
type Publisher struct {
	store         *Store
	dataDirectory string
	baseURL       string
	httpClient    *http.Client
}

// NewPublisher binds publication to one managed engine's HTTP origin.
func NewPublisher(store *Store, baseURL, dataDirectory string) (*Publisher, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("invalid Alertmanager URL: expected an HTTP origin")
	}
	return &Publisher{
		store: store, dataDirectory: dataDirectory, baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Sync writes generated changes, reloads, then verifies the native state.
// An unchanged file is not proof of successful loading: failed reloads retry.
func (publisher *Publisher) Sync(ctx context.Context) error {
	content, err := publisher.store.AlertmanagerConfig(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(publisher.dataDirectory, "config", "alertmanager.yml")
	previous, err := os.ReadFile(path) //nolint:gosec // Path is derived from the controller-owned data directory.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read published notification configuration: %w", err)
	}
	if !bytes.Equal(content, previous) {
		if _, err := components.WriteAlertmanagerConfig(publisher.dataDirectory, content); err != nil {
			return fmt.Errorf("write notification configuration: %w", err)
		}
	} else {
		loaded, err := publisher.loaded(ctx, content)
		if err != nil {
			return err
		}
		if loaded {
			return nil
		}
	}
	if _, err := publisher.request(ctx, http.MethodPost, "/-/reload"); err != nil {
		return fmt.Errorf("reload notification configuration: %w", err)
	}
	loaded, err := publisher.loaded(ctx, content)
	if err != nil {
		return err
	}
	if !loaded {
		return errors.New("desired notification configuration is not loaded in Alertmanager")
	}
	return nil
}

func (publisher *Publisher) loaded(ctx context.Context, content []byte) (bool, error) {
	body, err := publisher.request(ctx, http.MethodGet, "/metrics")
	if err != nil {
		return false, fmt.Errorf("read loaded notification configuration: %w", err)
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		// Parser diagnostics can include the response body.
		return false, errors.New("invalid Alertmanager metrics response")
	}
	loaded := true
	for name, want := range map[string]float64{
		"alertmanager_config_hash":                   configurationFingerprint(content),
		"alertmanager_config_last_reload_successful": 1,
	} {
		metric := metrics[name]
		samples := metric.GetMetric()
		if metric == nil || metric.GetType() != dto.MetricType_GAUGE || len(samples) != 1 ||
			len(samples[0].GetLabel()) != 0 || samples[0].GetGauge() == nil || samples[0].GetGauge().Value == nil {
			return false, errors.New("missing or invalid Alertmanager configuration metric")
		}
		if samples[0].GetGauge().GetValue() != want {
			loaded = false
		}
	}
	return loaded, nil
}

// Match config/coordinator.go in the pinned Alertmanager release: six MD5 bytes
// interpreted little-endian. This is a compatibility check, not authentication.
// see: https://github.com/prometheus/alertmanager/blob/v0.34.0/config/coordinator.go#L180
func configurationFingerprint(content []byte) float64 {
	digest := md5.Sum(content) //nolint:gosec // Matches Alertmanager's non-security configuration fingerprint.
	var truncated [8]byte
	copy(truncated[:], digest[:6])
	return float64(binary.LittleEndian.Uint64(truncated[:]))
}

func (publisher *Publisher) request(ctx context.Context, method, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, publisher.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create Alertmanager request: %w", err)
	}
	request.Header.Set("Accept", "text/plain; version=0.0.4")
	response, err := publisher.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute Alertmanager request: %w", err)
	}
	const limit = 4 * 1024 * 1024
	content, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return nil, fmt.Errorf("read Alertmanager response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(content) > limit {
		return nil, fmt.Errorf("unsuccessful or oversized Alertmanager response (HTTP %d)", response.StatusCode)
	}
	return content, nil
}
