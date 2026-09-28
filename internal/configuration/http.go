package configuration

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// HTTPMonitor is Arveld's serializable HTTP monitoring intent.
// Durations use explicit seconds, independently of Collector duration types.
type HTTPMonitor struct {
	monitor.HTTPOptions
	SkipTLSVerify   bool   `json:"skip_tls_verify,omitempty"`
	ID              string `json:"id"`
	Endpoint        string `json:"endpoint"`
	Method          string `json:"method"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func addHTTPMonitor(document *collectorDocument, value HTTPMonitor) error {
	if err := monitor.ValidateHTTPSettings(value.Endpoint, value.Method, value.IntervalSeconds, value.TimeoutSeconds); err != nil {
		return fmt.Errorf("validate HTTP settings: %w", err)
	}
	if err := monitor.ValidateHTTPOptions(value.HTTPOptions); err != nil {
		return fmt.Errorf("validate HTTP options: %w", err)
	}
	target := map[string]any{
		"endpoint": collectorLiteral(value.Endpoint), "method": value.Method,
		"timeout": strconv.Itoa(value.TimeoutSeconds) + "s",
	}
	if value.Body != "" {
		target["body"] = collectorLiteral(value.Body)
		target["auto_content_type"] = true
	}
	if len(value.Headers) > 0 {
		headers := make(map[string]string, len(value.Headers))
		for name, content := range value.Headers {
			headers[collectorLiteral(http.CanonicalHeaderKey(name))] = collectorLiteral(content)
		}
		target["headers"] = headers
	}
	if value.SkipTLSVerify {
		target["tls"] = map[string]bool{"insecure_skip_verify": true}
	}
	if len(value.Validations) > 0 {
		rules := make([]map[string]any, 0, len(value.Validations))
		for _, rule := range value.Validations {
			rules = append(rules, compileHTTPValidation(rule))
		}
		target["validations"] = rules
	}
	metrics := make(map[string]any)
	for _, name := range []string{
		"httpcheck.dns.lookup.duration", "httpcheck.client.connection.duration",
		"httpcheck.tls.handshake.duration", "httpcheck.client.request.duration",
		"httpcheck.response.duration", "httpcheck.response.size", "httpcheck.tls.cert_remaining",
		"httpcheck.validation.passed", "httpcheck.validation.failed",
	} {
		metrics[name] = map[string]bool{"enabled": true}
	}
	return addMonitorReceiver(document, "http_check", value.ID, map[string]any{
		"collection_interval": strconv.Itoa(value.IntervalSeconds) + "s", "targets": []map[string]any{target}, "metrics": metrics,
	})
}

func compileHTTPValidation(rule monitor.Validation) map[string]any {
	switch rule.Type {
	case "json_path":
		result := map[string]any{"json_path": collectorLiteral(rule.Path)}
		if rule.Equals != nil {
			result["equals"] = collectorLiteral(*rule.Equals)
		}
		return result
	case "min_size", "max_size":
		return map[string]any{rule.Type: *rule.Size}
	default:
		return map[string]any{rule.Type: collectorLiteral(rule.Value)}
	}
}

// Collector expansion must treat all user-supplied request and assertion data literally.
func collectorLiteral(value string) string { return strings.ReplaceAll(value, "$", "$$") }
