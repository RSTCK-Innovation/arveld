// Package prometheus calls the Prometheus HTTP API used by Arveld.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	instantQueryPath     = "/api/v1/query"
	rangeQueryPath       = "/api/v1/query_range"
	defaultQueryLookback = "5m"
)

// RangeQuery contains the parameters of a PromQL range query.
type RangeQuery struct {
	Expression    string
	Start         string
	End           string
	Step          string
	LookbackDelta string
}

// Client calls one Prometheus server.
type Client struct {
	baseURL              string
	httpClient           *http.Client
	instantQueryEndpoint url.URL
	rangeQueryEndpoint   url.URL
}

// NewClient creates a client for a Prometheus base URL with a positive total query timeout.
func NewClient(baseURL string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		return nil, errors.New("query timeout must be positive")
	}
	endpoint, err := url.JoinPath(baseURL, instantQueryPath)
	if err != nil {
		return nil, fmt.Errorf("build Prometheus instant query endpoint: %w", err)
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Prometheus instant query endpoint: %w", err)
	}
	rangeEndpoint, err := url.JoinPath(baseURL, rangeQueryPath)
	if err != nil {
		return nil, fmt.Errorf("build Prometheus range query endpoint: %w", err)
	}
	parsedRangeEndpoint, err := url.Parse(rangeEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Prometheus range query endpoint: %w", err)
	}

	return &Client{
		baseURL:              baseURL,
		httpClient:           &http.Client{Timeout: timeout},
		instantQueryEndpoint: *parsedEndpoint,
		rangeQueryEndpoint:   *parsedRangeEndpoint,
	}, nil
}

// Query evaluates one PromQL expression at the current Prometheus server time.
// An empty lookbackDelta uses five minutes, independently of native rule evaluation.
// The caller must close the returned response body.
func (client *Client) Query(
	ctx context.Context,
	expression string,
	lookbackDelta string,
) (*http.Response, error) {
	endpoint := client.instantQueryEndpoint
	parameters := make(url.Values)
	parameters.Set("query", expression)
	if lookbackDelta == "" {
		lookbackDelta = defaultQueryLookback
	}
	parameters.Set("lookback_delta", lookbackDelta)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint.String(),
		strings.NewReader(parameters.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus instant query request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute Prometheus instant query: %w", err)
	}

	return response, nil
}

// QueryRange evaluates one PromQL expression over a time range.
// An empty LookbackDelta uses the same five-minute default as Query.
// The caller must close the returned response body.
func (client *Client) QueryRange(
	ctx context.Context,
	query RangeQuery,
) (*http.Response, error) {
	endpoint := client.rangeQueryEndpoint
	parameters := make(url.Values)
	parameters.Set("query", query.Expression)
	parameters.Set("start", query.Start)
	parameters.Set("end", query.End)
	parameters.Set("step", query.Step)
	if query.LookbackDelta == "" {
		query.LookbackDelta = defaultQueryLookback
	}
	parameters.Set("lookback_delta", query.LookbackDelta)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint.String(),
		strings.NewReader(parameters.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("create Prometheus range query request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute Prometheus range query: %w", err)
	}

	return response, nil
}
