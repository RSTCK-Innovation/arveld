package configuration

import (
	"fmt"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// DNSMonitor is Arveld's serializable query and explicit resolver selection.
type DNSMonitor struct {
	ID              string `json:"id"`
	Endpoint        string `json:"endpoint"`
	DNSServer       string `json:"dns_server"`
	RecordType      string `json:"record_type"`
	Transport       string `json:"transport"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func addDNSMonitor(document *collectorDocument, value DNSMonitor) error {
	if err := monitor.ValidateDNSSettings(value.Endpoint, value.DNSServer, value.RecordType, value.Transport, value.IntervalSeconds, value.TimeoutSeconds); err != nil {
		return fmt.Errorf("validate DNS settings: %w", err)
	}
	return addMonitorReceiver(document, "dns_check", value.ID, map[string]any{
		"collection_interval": strconv.Itoa(value.IntervalSeconds) + "s",
		"dns_servers": []map[string]any{{
			"endpoint": value.DNSServer, "network": value.Transport,
			"timeout": strconv.Itoa(value.TimeoutSeconds) + "s",
		}},
		"hostnames": []map[string]any{{"name": value.Endpoint, "record_type": value.RecordType}},
	})
}
