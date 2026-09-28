package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AlertRule is the native state of an alerting rule returned by Prometheus.
type AlertRule struct {
	Name           string            `json:"name"`
	Query          string            `json:"query"`
	Duration       float64           `json:"duration"`
	Labels         map[string]string `json:"labels"`
	Type           string            `json:"type"`
	State          string            `json:"state"`
	Health         string            `json:"health"`
	LastError      string            `json:"lastError"`
	LastEvaluation time.Time         `json:"lastEvaluation"`
}

// ValidateExpression asks the running engine's parser to validate generated PromQL.
// It does not evaluate the expression or change the engine configuration.
func (client *Client) ValidateExpression(ctx context.Context, expression string) error {
	body, err := client.ruleRequest(ctx, http.MethodPost, "/api/v1/format_query", url.Values{"query": {expression}})
	if err != nil {
		return err
	}
	var formatted string
	if err := decodeRuleResponse(body, &formatted); err != nil {
		return err
	}
	if formatted == "" {
		return errors.New("empty validated expression returned by Prometheus")
	}
	return nil
}

// Reload asks the owned Prometheus process to reload its files.
func (client *Client) Reload(ctx context.Context) error {
	_, err := client.ruleRequest(ctx, http.MethodPost, "/-/reload", nil)
	return err
}

// AlertRules reads one group's alerting rules, including their native evaluation state.
func (client *Client) AlertRules(ctx context.Context, groupName string) ([]AlertRule, error) {
	body, err := client.ruleRequest(ctx, http.MethodGet, "/api/v1/rules",
		url.Values{"rule_group[]": {groupName}, "type": {"alert"}, "exclude_alerts": {"true"}})
	if err != nil {
		return nil, err
	}
	var data struct {
		GroupNextToken string `json:"groupNextToken"`
		Groups         []struct {
			Name  string      `json:"name"`
			Rules []AlertRule `json:"rules"`
		} `json:"groups"`
	}
	if err := decodeRuleResponse(body, &data); err != nil {
		return nil, err
	}
	if data.Groups == nil || data.GroupNextToken != "" {
		return nil, errors.New("missing or partial groups in Prometheus rules response")
	}
	var rules []AlertRule
	for _, group := range data.Groups {
		if group.Name != groupName {
			return nil, errors.New("unexpected rule group returned by Prometheus")
		}
		for _, rule := range group.Rules {
			if rule.Type != "alerting" || rule.Name == "" || rule.Query == "" ||
				(rule.State != "unknown" && rule.State != "inactive" && rule.State != "pending" && rule.State != "firing") ||
				(rule.Health != "unknown" && rule.Health != "ok" && rule.Health != "err") {
				return nil, errors.New("invalid native alert rule returned by Prometheus")
			}
		}
		rules = append(rules, group.Rules...)
	}
	return rules, nil
}

func decodeRuleResponse(body []byte, destination any) error {
	var envelope struct {
		Status   string          `json:"status"`
		Warnings []string        `json:"warnings"`
		Data     json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Status != "success" ||
		len(envelope.Warnings) != 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("invalid, unsuccessful or partial Prometheus rules response")
	}
	if err := json.Unmarshal(envelope.Data, destination); err != nil {
		return errors.New("invalid Prometheus rules response data")
	}
	return nil
}

func (client *Client) ruleRequest(ctx context.Context, method, path string, values url.Values) ([]byte, error) {
	rawEndpoint, err := url.JoinPath(client.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("build Prometheus rules endpoint: %w", err)
	}
	endpoint, err := url.Parse(rawEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Prometheus rules endpoint: %w", err)
	}
	var body io.Reader
	if method == http.MethodGet {
		parameters := endpoint.Query()
		maps.Copy(parameters, values)
		endpoint.RawQuery = parameters.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus rules request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute Prometheus rules request: %w", err)
	}
	const limit = 4 * 1024 * 1024
	content, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return nil, fmt.Errorf("read Prometheus rules response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(content) > limit {
		return nil, fmt.Errorf("unsuccessful or oversized Prometheus rules response (HTTP %d)", response.StatusCode)
	}
	return content, nil
}
