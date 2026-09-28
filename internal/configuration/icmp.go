package configuration

import (
	"fmt"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// ICMPMonitor is Arveld's serializable intent for a bounded sequence of pings.
type ICMPMonitor struct {
	ID              string `json:"id"`
	Endpoint        string `json:"endpoint"`
	PingCount       int    `json:"ping_count"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func addICMPMonitor(document *collectorDocument, value ICMPMonitor) error {
	if err := monitor.ValidateICMPSettings(value.Endpoint, value.PingCount, value.IntervalSeconds, value.TimeoutSeconds); err != nil {
		return fmt.Errorf("validate ICMP settings: %w", err)
	}
	return addMonitorReceiver(document, "icmpcheckreceiver", value.ID, map[string]any{
		"collection_interval": strconv.Itoa(value.IntervalSeconds) + "s",
		"targets": []map[string]any{{
			"host": value.Endpoint, "ping_count": value.PingCount,
			"ping_interval": "1s", "ping_timeout": strconv.Itoa(value.TimeoutSeconds) + "s",
		}},
	})
}
