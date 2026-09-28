package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SilenceClient sends notification suspensions to the owned Alertmanager.
// Alertmanager owns their identity, storage and expiration.
type SilenceClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSilenceClient binds silence operations to one managed engine's HTTP origin.
func NewSilenceClient(baseURL string) (*SilenceClient, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("invalid Alertmanager URL: expected an HTTP origin")
	}
	return &SilenceClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

type silenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

// Silence is an Agent- or Monitor-scoped view of a native Alertmanager silence.
// State and timestamps are read from the engine, not evaluated by Arveld.
type Silence struct {
	ID               string    `json:"id"`
	MonitorID        string    `json:"monitor_id,omitempty"`
	AgentInstanceUID string    `json:"agent_instance_uid,omitempty"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	CreatedBy        string    `json:"created_by"`
	Comment          string    `json:"comment"`
	State            string    `json:"state"`
}

// ErrSilenceNotFound means the silence is absent from this owner's native collection.
var ErrSilenceNotFound = errors.New("notification silence not found")

// CreateMonitorSilence creates an exact Monitor silence for a validated time window.
// A failed request is not retried: the engine may already have accepted the write.
func (client *SilenceClient) CreateMonitorSilence(ctx context.Context, monitorID string, startsAt, endsAt time.Time, comment string) (string, error) {
	return client.createSilence(ctx, silenceMatcher{Name: "arveld_monitor_id", Value: monitorID, IsEqual: true}, startsAt, endsAt, comment)
}

// CreateAgentSilence silences an Agent and its Monitors for a validated time window.
// agentUID must use the canonical UUID representation carried by native alerts.
func (client *SilenceClient) CreateAgentSilence(ctx context.Context, agentUID string, startsAt, endsAt time.Time, comment string) (string, error) {
	return client.createSilence(ctx, silenceMatcher{Name: "arveld_agent_id", Value: agentUID, IsEqual: true}, startsAt, endsAt, comment)
}

func (client *SilenceClient) createSilence(ctx context.Context, matcher silenceMatcher, startsAt, endsAt time.Time, comment string) (string, error) {
	content, err := json.Marshal(struct {
		Matchers  []silenceMatcher `json:"matchers"`
		StartsAt  time.Time        `json:"startsAt"`
		EndsAt    time.Time        `json:"endsAt"`
		CreatedBy string           `json:"createdBy"`
		Comment   string           `json:"comment"`
	}{
		Matchers: []silenceMatcher{matcher},
		StartsAt: startsAt, EndsAt: endsAt, CreatedBy: "Arveld", Comment: comment,
	})
	if err != nil {
		return "", fmt.Errorf("encode notification silence: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/v2/silences", bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("create Alertmanager silence request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("execute Alertmanager silence request: %w", err)
	}
	const limit = 4 * 1024
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return "", fmt.Errorf("read Alertmanager silence response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(body) > limit {
		return "", fmt.Errorf("unsuccessful or oversized Alertmanager silence response (HTTP %d)", response.StatusCode)
	}
	var created struct {
		ID string `json:"silenceID"`
	}
	if err := json.Unmarshal(body, &created); err != nil || strings.TrimSpace(created.ID) == "" {
		return "", errors.New("invalid Alertmanager silence response")
	}
	return created.ID, nil
}

// ListMonitorSilences reads exact, whole-Monitor silences in native order.
// An empty monitorID reads silences across all Monitors, including deleted ones.
// Expired entries remain visible only while retained by Alertmanager.
func (client *SilenceClient) ListMonitorSilences(ctx context.Context, monitorID string) ([]Silence, error) {
	return client.listSilences(ctx, "arveld_monitor_id", monitorID)
}

// ListAgentSilences reads exact, whole-Agent silences, including expired entries
// retained by Alertmanager. Monitor-owned silences are not included.
func (client *SilenceClient) ListAgentSilences(ctx context.Context, agentUID string) ([]Silence, error) {
	return client.listSilences(ctx, "arveld_agent_id", agentUID)
}

// ListSilences reads whole-Agent and whole-Monitor silences in native order,
// including retained entries whose owners no longer exist.
func (client *SilenceClient) ListSilences(ctx context.Context) ([]Silence, error) {
	return client.listSilences(ctx, "", "")
}

func (client *SilenceClient) listSilences(ctx context.Context, ownerLabel, ownerID string) ([]Silence, error) {
	endpoint := client.baseURL + "/api/v2/silences"
	if ownerID != "" {
		endpoint += "?filter=" + url.QueryEscape(ownerLabel+"="+strconv.Quote(ownerID))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Alertmanager silence list request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute Alertmanager silence list request: %w", err)
	}
	const limit = 4 * 1024 * 1024
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return nil, fmt.Errorf("read Alertmanager silence list response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(body) > limit {
		return nil, fmt.Errorf("unsuccessful or oversized Alertmanager silence list response (HTTP %d)", response.StatusCode)
	}
	var values []struct {
		ID        string           `json:"id"`
		Matchers  []silenceMatcher `json:"matchers"`
		StartsAt  time.Time        `json:"startsAt"`
		EndsAt    time.Time        `json:"endsAt"`
		CreatedBy string           `json:"createdBy"`
		Comment   string           `json:"comment"`
		Status    struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &values); err != nil || values == nil {
		return nil, errors.New("invalid Alertmanager silence list response")
	}
	silences := make([]Silence, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.ID) == "" || len(value.Matchers) == 0 || value.StartsAt.IsZero() || value.EndsAt.IsZero() {
			return nil, errors.New("incomplete Alertmanager silence response")
		}
		switch value.Status.State {
		case "pending", "active", "expired":
		default:
			return nil, errors.New("invalid Alertmanager silence state")
		}
		matcher, ok := wholeSilenceMatcher(value.Matchers, ownerLabel, ownerID)
		if !ok {
			continue
		}
		silence := Silence{
			ID: value.ID, StartsAt: value.StartsAt, EndsAt: value.EndsAt,
			CreatedBy: value.CreatedBy, Comment: value.Comment, State: value.Status.State,
		}
		switch matcher.Name {
		case "arveld_monitor_id":
			silence.MonitorID = matcher.Value
		case "arveld_agent_id":
			silence.AgentInstanceUID = matcher.Value
		default:
			continue
		}
		silences = append(silences, silence)
	}
	return silences, nil
}

// wholeSilenceMatcher excludes multiple matchers, regex/negative scopes and other owners.
func wholeSilenceMatcher(matchers []silenceMatcher, ownerLabel, ownerID string) (silenceMatcher, bool) {
	if len(matchers) != 1 || matchers[0].IsRegex || !matchers[0].IsEqual || matchers[0].Value == "" {
		return silenceMatcher{}, false
	}
	matcher := matchers[0]
	if (ownerLabel != "" && matcher.Name != ownerLabel) || (ownerID != "" && matcher.Value != ownerID) {
		return silenceMatcher{}, false
	}
	return matcher, true
}

// CancelMonitorSilence expires a silence only after verifying its Monitor scope.
// Alertmanager treats repeated expiration as success while the entry is retained.
func (client *SilenceClient) CancelMonitorSilence(ctx context.Context, monitorID, id string) error {
	return client.cancelSilence(ctx, "arveld_monitor_id", monitorID, id)
}

// CancelAgentSilence expires a silence only after verifying its whole-Agent scope.
// Alertmanager treats repeated expiration as success while the entry is retained.
func (client *SilenceClient) CancelAgentSilence(ctx context.Context, agentUID, id string) error {
	return client.cancelSilence(ctx, "arveld_agent_id", agentUID, id)
}

func (client *SilenceClient) cancelSilence(ctx context.Context, ownerLabel, ownerID, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := client.listSilences(ctx, ownerLabel, ownerID)
	if err != nil {
		return err
	}
	for _, value := range values {
		if value.ID != id {
			continue
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, client.baseURL+"/api/v2/silence/"+url.PathEscape(id), nil)
		if err != nil {
			return fmt.Errorf("create Alertmanager silence cancellation request: %w", err)
		}
		response, err := client.httpClient.Do(request)
		if err != nil {
			return fmt.Errorf("execute Alertmanager silence cancellation request: %w", err)
		}
		const limit = 4 * 1024
		body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err := errors.Join(readErr, response.Body.Close()); err != nil {
			return fmt.Errorf("read Alertmanager silence cancellation response: %w", err)
		}
		if len(body) > limit {
			return errors.New("oversized Alertmanager silence cancellation response")
		}
		switch response.StatusCode {
		case http.StatusOK:
			return nil
		case http.StatusNotFound:
			return ErrSilenceNotFound
		default:
			return fmt.Errorf("unsuccessful Alertmanager silence cancellation (HTTP %d)", response.StatusCode)
		}
	}
	return ErrSilenceNotFound
}
