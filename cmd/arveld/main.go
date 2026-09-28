// Arveld starts the Arveld controller.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/RSTCK-Innovation/arveld/internal/app"
)

// version is set by the release build.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	flags := flag.NewFlagSet("arveld", flag.ContinueOnError)
	showVersion := flags.Bool("version", false, "print the Arveld version and exit")
	configPath := flags.String("config", "", "path to the Arveld YAML configuration")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *showVersion {
		if _, err := fmt.Fprintln(os.Stdout, "arveld "+version); err != nil {
			return 1
		}
		return 0
	}
	if flags.NArg() != 0 {
		if flags.Arg(0) != "reset-password" {
			logger.Error("unexpected positional arguments")
			return 2
		}
		resetArguments := flags.Args()[1:]
		flags.Visit(func(option *flag.Flag) {
			if option.Name == "config" {
				resetArguments = append([]string{"--config", *configPath}, resetArguments...)
			}
		})
		return runResetPassword(resetArguments)
	}

	config, err := app.LoadConfig(*configPath)
	if err != nil {
		logger.Error("load configuration", "error", err)
		return 1
	}
	config.Version = version

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := app.Run(ctx, config, logger); err != nil {
		logger.Error("controller stopped", "error", err)
		return 1
	}

	return 0
}
