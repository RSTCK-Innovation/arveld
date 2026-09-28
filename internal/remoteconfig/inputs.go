package remoteconfig

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// addMonitorInputs copies persisted execution settings into the immutable snapshot.
// Display names and Agent assignments stay in product storage.
func addMonitorInputs(specification *configuration.Specification, values []monitor.Monitor) error {
	for _, value := range values {
		switch value.Protocol {
		case "http":
			specification.HTTPMonitors = append(specification.HTTPMonitors, configuration.HTTPMonitor{
				ID: value.ID, Endpoint: value.Endpoint, Method: value.Method,
				HTTPOptions: value.HTTPOptions, SkipTLSVerify: value.SkipTLSVerify,
				IntervalSeconds: value.IntervalSeconds, TimeoutSeconds: value.TimeoutSeconds,
			})
		case "tcp":
			specification.TCPMonitors = append(specification.TCPMonitors, configuration.TCPMonitor{
				ID: value.ID, Endpoint: value.Endpoint, IntervalSeconds: value.IntervalSeconds, TimeoutSeconds: value.TimeoutSeconds,
			})
		case "icmp":
			specification.ICMPMonitors = append(specification.ICMPMonitors, configuration.ICMPMonitor{
				ID: value.ID, Endpoint: value.Endpoint, PingCount: value.PingCount, IntervalSeconds: value.IntervalSeconds, TimeoutSeconds: value.TimeoutSeconds,
			})
		case "dns":
			specification.DNSMonitors = append(specification.DNSMonitors, configuration.DNSMonitor{
				ID: value.ID, Endpoint: value.Endpoint, DNSServer: value.DNSServer, RecordType: value.RecordType,
				Transport: value.Transport, IntervalSeconds: value.IntervalSeconds, TimeoutSeconds: value.TimeoutSeconds,
			})
		default:
			return fmt.Errorf("unsupported Monitor protocol %q", value.Protocol)
		}
	}
	return nil
}

func recheckMonitorInputs(ctx context.Context, tx *sql.Tx, uid agent.InstanceUID, values []monitor.Monitor) error {
	latest, err := monitor.ListForAgent(ctx, tx, uid)
	if err != nil {
		return fmt.Errorf("recheck current Monitor inputs: %w", err)
	}
	if !reflect.DeepEqual(values, latest) {
		return ErrInputsChanged
	}
	return nil
}
