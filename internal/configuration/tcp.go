package configuration

import (
	"fmt"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// TCPMonitor is Arveld's serializable TCP connection monitoring intent.
// Endpoint contains a host and port; durations use explicit seconds.
type TCPMonitor struct {
	ID              string `json:"id"`
	Endpoint        string `json:"endpoint"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func addTCPMonitor(document *collectorDocument, value TCPMonitor) error {
	if err := monitor.ValidateTCPSettings(value.Endpoint, value.IntervalSeconds, value.TimeoutSeconds); err != nil {
		return fmt.Errorf("validate TCP settings: %w", err)
	}
	return addMonitorReceiver(document, "tcp_check", value.ID, map[string]any{
		"collection_interval": strconv.Itoa(value.IntervalSeconds) + "s",
		"targets": []map[string]any{{
			"endpoint": value.Endpoint,
			"dialer":   map[string]any{"timeout": strconv.Itoa(value.TimeoutSeconds) + "s"},
		}},
	})
}
