package integration

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestOTLPMetricsForwardsAuthorizedRequestToPrometheus(t *testing.T) {
	payload := []byte{0x0a, 0x03, 0x01, 0x02, 0x03}
	prometheus := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost {
				t.Errorf("method = %q, want %q", request.Method, http.MethodPost)
			}
			if request.URL.Path != "/api/v1/otlp/v1/metrics" {
				t.Errorf(
					"path = %q, want %q",
					request.URL.Path,
					"/api/v1/otlp/v1/metrics",
				)
			}
			if got, want := request.Header.Get("Content-Type"), "application/x-protobuf"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			if authorization := request.Header.Get("Authorization"); authorization != "" {
				t.Errorf("Authorization = %q, want it removed", authorization)
			}

			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read forwarded body: %v", err)

				return
			}
			if !bytes.Equal(body, payload) {
				t.Errorf("body = %v, want %v", body, payload)
			}

			response.WriteHeader(http.StatusAccepted)
			if _, err := response.Write([]byte("accepted by Prometheus")); err != nil {
				t.Errorf("write Prometheus response: %v", err)
			}
		},
	))
	t.Cleanup(prometheus.Close)

	config := controllerConfig(t, prometheus.URL)
	token := createControllerAgentKey(t, config.DatabasePath)
	controllerURL, _ := startController(t, config)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		controllerURL+"/v1/otlp/v1/metrics",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/x-protobuf")
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()

	if response.StatusCode != http.StatusAccepted {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(responseBody), "accepted by Prometheus"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func createControllerAgentKey(t *testing.T, databasePath string) string {
	t.Helper()
	db := testutil.OpenDatabase(t, databasePath)
	createAdministrator(t, db)
	return createAgentKey(t, db)
}

func TestOTLPMetricsUsesExternalPrometheusCredentials(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			username, password, ok := request.BasicAuth()
			if !ok || username != "metrics-user" || password != "metrics-password" {
				http.Error(response, "invalid upstream credentials", http.StatusUnauthorized)

				return
			}

			response.WriteHeader(http.StatusAccepted)
		},
	))
	t.Cleanup(prometheus.Close)

	prometheusURL, err := url.Parse(prometheus.URL)
	if err != nil {
		t.Fatalf("parse Prometheus test URL: %v", err)
	}
	prometheusURL.User = url.UserPassword("metrics-user", "metrics-password")
	config := controllerConfig(t, prometheusURL.String())
	token := createControllerAgentKey(t, config.DatabasePath)
	controllerURL, _ := startController(t, config)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		controllerURL+"/v1/otlp/v1/metrics",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()

	if response.StatusCode != http.StatusAccepted {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
}

func TestOTLPMetricsReturnsBadGatewayWhenPrometheusIsUnreachable(t *testing.T) {
	prometheus := httptest.NewServer(http.NotFoundHandler())
	prometheusURL := prometheus.URL
	prometheus.Close()

	config := controllerConfig(t, prometheusURL)
	token := createControllerAgentKey(t, config.DatabasePath)
	controllerURL, _ := startController(t, config)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		controllerURL+"/v1/otlp/v1/metrics",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()

	if response.StatusCode != http.StatusBadGateway {
		t.Errorf(
			"status code = %d, want %d",
			response.StatusCode,
			http.StatusBadGateway,
		)
	}
}
