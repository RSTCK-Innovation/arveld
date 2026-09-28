// Arveld-agent runs the supervised Arveld Collector from one executable.
package main

import (
	"fmt"
	"os"
)

// The Supervisor sets this only in the child process environment. Collector
// subcommands and flags can then pass through without changing their meaning.
const collectorMode = "ARVELD_INTERNAL_COLLECTOR"

// version is set by the release build.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if os.Getenv(collectorMode) == "1" {
		return runCollector(arguments)
	}
	if len(arguments) > 0 && arguments[0] == "collector" {
		return runCollector(arguments[1:])
	}
	if len(arguments) == 1 && (arguments[0] == "--version" || arguments[0] == "-version") {
		if _, err := fmt.Fprintln(os.Stdout, "arveld-agent "+version); err != nil {
			return fmt.Errorf("print version: %w", err)
		}
		return nil
	}
	return runSupervisor(arguments)
}
