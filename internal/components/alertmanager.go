package components

import (
	"context"
	"fmt"
	"path/filepath"
)

const alertmanagerListenAddress = "127.0.0.1:19093"

// ManagedAlertmanagerURL is the base URL of managed Alertmanager.
const ManagedAlertmanagerURL = "http://" + alertmanagerListenAddress

// ManagedAlertmanagerReadinessEndpoint is the readiness endpoint of managed Alertmanager.
const ManagedAlertmanagerReadinessEndpoint = ManagedAlertmanagerURL + "/-/ready"

// WriteAlertmanagerConfig atomically writes Arveld's generated configuration.
// Alertmanager validates its schema when starting or reloading the file.
func WriteAlertmanagerConfig(dataDirectory string, content []byte) (string, error) {
	return writeComponentConfig(dataDirectory, alertmanagerComponent, string(content))
}

// runAlertmanager supervises Alertmanager using the application-prepared config.
func (supervisor *Supervisor) runAlertmanager(ctx context.Context) error {
	configPath := filepath.Join(supervisor.dataDirectory, "config", "alertmanager.yml")
	if err := supervisor.runComponent(
		ctx,
		alertmanagerComponent,
		"--config.file="+configPath,
		"--storage.path="+filepath.Join(supervisor.dataDirectory, "alertmanager"),
		"--web.listen-address="+alertmanagerListenAddress,
		"--cluster.listen-address=",
		"--log.format=json",
	); err != nil {
		return fmt.Errorf("run managed Alertmanager: %w", err)
	}

	return nil
}
