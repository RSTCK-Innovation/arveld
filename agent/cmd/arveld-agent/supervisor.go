package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/open-telemetry/opentelemetry-collector-contrib/cmd/opampsupervisor/supervisor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/cmd/opampsupervisor/supervisor/config"
	"github.com/open-telemetry/opentelemetry-collector-contrib/cmd/opampsupervisor/supervisor/telemetry"
)

func runSupervisor(arguments []string) error {
	flags := flag.NewFlagSet("arveld-agent", flag.ContinueOnError)
	directory := flags.String("storage-directory", "data/arveld-agent", "directory for persistent Agent identity and configuration")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse Agent arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *directory == "" {
		return errors.New("storage directory must not be empty")
	}
	endpoint, err := opampEndpoint(os.Getenv("ARVELD_URL"))
	if err != nil {
		return err
	}
	token := os.Getenv("ARVELD_AGENT_TOKEN")
	if token == "" {
		return errors.New("ARVELD_AGENT_TOKEN is required")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate Agent executable: %w", err)
	}

	cfg := config.DefaultSupervisor()
	cfg.Server.Endpoint = endpoint
	cfg.Server.Headers = http.Header{"Authorization": {"Bearer " + token}}
	cfg.Storage.Directory = *directory
	cfg.Agent.Executable = executable
	cfg.Agent.Env = map[string]string{collectorMode: "1"}
	cfg.Agent.ValidateConfig = true
	cfg.Agent.AutomaticConfigRollback = true
	cfg.Capabilities.AcceptsRemoteConfig = true

	logger, err := telemetry.NewLogger(cfg.Telemetry.Logs)
	if err != nil {
		return fmt.Errorf("create Agent logger: %w", err)
	}
	ctx := context.Background()
	shutdown, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	managed, err := supervisor.NewSupervisor(ctx, logger, cfg)
	if err != nil {
		return fmt.Errorf("create Agent supervisor: %w", err)
	}
	// Keep the Supervisor's context alive while it stops its child gracefully.
	defer managed.Shutdown()
	if err := managed.Start(ctx); err != nil {
		return fmt.Errorf("start Agent supervisor: %w", err)
	}
	<-shutdown.Done()
	// Restore default signal handling so another signal can force shutdown.
	stop()
	return nil
}

func opampEndpoint(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		strings.ContainsAny(address, " \t\r\n?#") {
		return "", errors.New("ARVELD_URL must be an absolute HTTP(S) URL without credentials, whitespace, query or fragment")
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", errors.New("ARVELD_URL must use HTTP or HTTPS")
	}
	return strings.TrimRight(parsed.String(), "/") + "/v1/opamp", nil
}
