package monitor_test

import (
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

func TestHTTPOptionsRejectUnexecutableOrAmbiguousRequests(t *testing.T) {
	for _, test := range []struct {
		name    string
		options monitor.HTTPOptions
	}{
		{"oversized body", monitor.HTTPOptions{Body: strings.Repeat("é", 32769)}},
		{"header injection", monitor.HTTPOptions{Headers: map[string]string{"Authorization": "Bearer token\r\nInjected: yes"}}},
		{"duplicate header case", monitor.HTTPOptions{Headers: map[string]string{"Content-Type": "a", "content-type": "b"}}},
		{"manual framing", monitor.HTTPOptions{Headers: map[string]string{"Content-Length": "100"}}},
		{"invalid regex", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "regex", Value: "("}}}},
		{"silently ignored empty assertion", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "contains"}}}},
		{"unsupported empty equality", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "json_path", Path: "ready", Equals: new("")}}}},
		{"missing size", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "min_size"}}}},
		{"negative size", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "max_size", Size: new(int64(-1))}}}},
		{"mixed rule", monitor.HTTPOptions{Validations: []monitor.Validation{{Type: "contains", Value: "ready", Path: "data"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := monitor.Monitor{Name: "API readiness", Protocol: "http", Endpoint: "https://example.com", Method: "POST", IntervalSeconds: 30, TimeoutSeconds: 5, HTTPOptions: test.options}
			if _, err := monitor.Validate(value); err == nil {
				t.Fatal("invalid HTTP options accepted")
			}
		})
	}
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		options := monitor.HTTPOptions{Body: `{"ready":true}`, Headers: map[string]string{"Authorization": "Bearer $literal"}}
		value := monitor.Monitor{Name: "API readiness", Protocol: "http", Endpoint: "https://example.com", Method: method, IntervalSeconds: 30, TimeoutSeconds: 5, HTTPOptions: options}
		if _, err := monitor.Validate(value); err != nil {
			t.Fatalf("%s options: %v", method, err)
		}
	}
}
