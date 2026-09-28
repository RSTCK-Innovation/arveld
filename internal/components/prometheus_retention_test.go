package components

import (
	"testing"
)

func TestValidatePrometheusRetentionSizeSyntax(t *testing.T) {
	tests := []struct {
		name      string
		size      string
		wantError bool
	}{
		{name: "zero", size: "0"},
		{name: "bytes", size: "0B"},
		{name: "explicit positive sign", size: "+1GiB"},
		{name: "legacy binary unit", size: "1KB"},
		{name: "IEC binary unit", size: "1KiB"},
		{name: "decimal", size: "1.5MiB"},
		{name: "decimal without integer part", size: ".5GiB"},
		{name: "decimal without fractional part", size: "1.GiB"},
		{name: "combined units", size: "1MiB1KiB25B"},
		{name: "combined legacy units", size: "1MB1KB25B"},
		{name: "maximum boundary", size: "8EiB"},
		{name: "negative", size: "-1GiB", wantError: true},
		{name: "lowercase legacy prefix", size: "1kB", wantError: true},
		{name: "lowercase IEC prefix", size: "1kiB", wantError: true},
		{name: "incomplete unit", size: "1Gi", wantError: true},
		{name: "missing number", size: "GiB", wantError: true},
		{name: "missing unit", size: "1", wantError: true},
		{name: "two decimal separators", size: "1..5GiB", wantError: true},
		{name: "mixed IEC and legacy units", size: "1MiB1KB", wantError: true},
		{name: "integer overflow", size: "9223372036854775807B", wantError: true},
		{name: "overflow", size: "9EiB", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePrometheusRetention(PrometheusConfig{
				RetentionTime: "15d",
				RetentionSize: test.size,
			})
			if test.wantError && err == nil {
				t.Fatal("ValidatePrometheusRetention() error = nil, want an error")
			}
			if !test.wantError && err != nil {
				t.Errorf("ValidatePrometheusRetention() error = %v, want nil", err)
			}
		})
	}
}
