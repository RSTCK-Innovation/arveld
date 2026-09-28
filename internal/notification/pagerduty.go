package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pagerDutyConfig uses Events API v2; legacy service keys are not accepted.
type pagerDutyConfig struct {
	URL        string `json:"url"`
	RoutingKey string `json:"routing_key"`
}

func normalizePagerDutyConfig(encoded json.RawMessage) (json.RawMessage, error) {
	config := pagerDutyConfig{URL: "https://events.pagerduty.com/v2/enqueue"}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, ErrInvalidChannel
	}
	if len(config.RoutingKey) > 512 || !printableASCII(config.RoutingKey) || strings.ContainsAny(config.RoutingKey, "{}") {
		return nil, ErrInvalidChannel
	}
	if err := validateWebhookURL(config.URL); err != nil {
		return nil, err
	}
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode PagerDuty configuration: %w", err)
	}
	return result, nil
}
