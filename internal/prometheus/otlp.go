package prometheus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

const prometheusOTLPMetricsPath = "/api/v1/otlp/v1/metrics"

type otlpMetricsProxy struct {
	forward http.Handler
	timeout time.Duration
}

func (proxy *otlpMetricsProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), proxy.timeout)
	defer cancel()
	proxy.forward.ServeHTTP(response, request.WithContext(ctx))
}

// NewOTLPMetricsProxy forwards metrics with a positive total transfer timeout.
func NewOTLPMetricsProxy(
	prometheusURL string,
	logger *slog.Logger,
	timeout time.Duration,
) (http.Handler, error) {
	if timeout <= 0 {
		return nil, errors.New("OTLP metrics timeout must be positive")
	}
	endpoint, err := url.JoinPath(prometheusURL, prometheusOTLPMetricsPath)
	if err != nil {
		return nil, fmt.Errorf("build Prometheus OTLP endpoint: %w", err)
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Prometheus OTLP endpoint: %w", err)
	}

	proxy := &otlpMetricsProxy{timeout: timeout, forward: &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target)
			proxyRequest.Out.URL.Path = target.Path
			proxyRequest.Out.URL.RawPath = target.RawPath
			proxyRequest.Out.Header.Del("Authorization")
			if target.User != nil {
				password, _ := target.User.Password()
				proxyRequest.Out.SetBasicAuth(target.User.Username(), password)
			}
		},
		ErrorHandler: func(
			response http.ResponseWriter,
			request *http.Request,
			err error,
		) {
			logger.ErrorContext(
				request.Context(),
				"forward OTLP metrics to Prometheus",
				"error",
				err,
			)
			http.Error(
				response,
				http.StatusText(http.StatusBadGateway),
				http.StatusBadGateway,
			)
		},
	}}
	return proxy, nil
}
