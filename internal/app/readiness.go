package app

import (
	"context"

	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
)

func newReadinessChecker(
	runtime ComponentRuntime,
	managedState *managedComponentsState,
) httpapi.ReadinessChecker {
	readinessProbe := components.NewReadinessProbe()

	prometheusReadinessEndpoint := runtime.PrometheusURL + "/-/ready"
	alertmanagerReadinessEndpoint := runtime.AlertmanagerURL + "/-/ready"

	return func(ctx context.Context) httpapi.DependencyReadiness {
		if managedState == nil || !managedState.ready() {
			return httpapi.DependencyReadiness{}
		}

		prometheusResult := make(chan bool, 1)
		alertmanagerResult := make(chan bool, 1)

		go func() {
			prometheusResult <- readinessProbe.Ready(ctx, prometheusReadinessEndpoint)
		}()

		go func() {
			alertmanagerResult <- readinessProbe.Ready(ctx, alertmanagerReadinessEndpoint)
		}()

		return httpapi.DependencyReadiness{
			Prometheus:   <-prometheusResult,
			Alertmanager: <-alertmanagerResult,
		}
	}
}
