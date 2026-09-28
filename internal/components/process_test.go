package components

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunProcessStartsExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	markerPath := filepath.Join(t.TempDir(), "started")
	spec := processSpec{
		executable: executable,
		arguments: []string{
			"-test.run=^TestRunProcessHelper$",
			"--",
			markerPath,
		},
	}

	if err := runProcessOnce(context.Background(), spec, discardLogger()); err != nil {
		t.Fatalf("run process: %v", err)
	}

	marker, err := os.ReadFile(markerPath) //nolint:gosec // path is inside t.TempDir
	if err != nil {
		t.Fatalf("read process marker: %v", err)
	}
	if string(marker) != "started" {
		t.Errorf("process marker = %q, want %q", marker, "started")
	}
}

func TestRunProcessLogsStandardOutputAndError(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	err = runProcessOnce(
		context.Background(),
		processSpec{
			component:  prometheusComponent,
			executable: executable,
			arguments: []string{
				"-test.run=^TestRunProcessHelper$",
				"--",
				"write-output",
			},
		},
		logger,
	)
	if err != nil {
		t.Fatalf("run process: %v", err)
	}

	type logEntry struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Component string `json:"component"`
		Stream    string `json:"stream"`
	}
	want := map[string]logEntry{
		"stdout": {
			Level:     "INFO",
			Message:   "component standard output",
			Component: "prometheus",
			Stream:    "stdout",
		},
		"stderr": {
			Level:     "INFO",
			Message:   "component standard error",
			Component: "prometheus",
			Stream:    "stderr",
		},
	}
	found := make(map[string]bool)
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	for {
		var entry logEntry
		if err := decoder.Decode(&entry); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode process log: %v", err)
		}
		if wantEntry, exists := want[entry.Stream]; exists && entry == wantEntry {
			found[entry.Stream] = true
		}
	}

	for stream := range want {
		if !found[stream] {
			t.Errorf("%s process output was not logged", stream)
		}
	}
}

func TestRunProcessAllowsGracefulShutdownBeforeKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to a Windows process is not implemented")
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	testDirectory := t.TempDir()
	readyPath := filepath.Join(testDirectory, "ready")
	stoppedPath := filepath.Join(testDirectory, "stopped")
	ctx, cancel := context.WithCancel(context.Background())
	processErrors := make(chan error, 1)
	go func() {
		processErrors <- runProcessOnce(
			ctx,
			processSpec{
				executable: executable,
				arguments: []string{
					"-test.run=^TestRunProcessHelper$",
					"--",
					"graceful-shutdown",
					readyPath,
					stoppedPath,
				},
			},
			discardLogger(),
		)
	}()

	waitForTestFile(t, readyPath)
	cancel()

	select {
	case <-processErrors:
	case <-time.After(2 * time.Second):
		t.Fatal("runProcess did not return after context cancellation")
	}
	if _, err := os.Stat(stoppedPath); err != nil {
		t.Errorf("graceful shutdown marker: %v", err)
	}
}

func TestRunProcessPreservesStructuredLogs(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []componentName{prometheusComponent, alertmanagerComponent} {
		t.Run(string(component), func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
			if err := runProcessOnce(t.Context(), processSpec{
				component:  component,
				executable: executable,
				arguments:  []string{"-test.run=^TestRunProcessHelper$", "--", "write-json-output"},
			}, logger); err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool)
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			for {
				var entry struct {
					Time      string `json:"time"`
					Level     string `json:"level"`
					Message   string `json:"msg"`
					Component string `json:"component"`
					Stream    string `json:"stream"`
					Upstream  struct {
						Source    string `json:"source"`
						Old       int    `json:"old"`
						New       int    `json:"new"`
						Component string `json:"component"`
					} `json:"upstream"`
				}
				if err := decoder.Decode(&entry); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if entry.Message != "updated GOGC" {
					continue
				}
				found[entry.Level] = true
				if entry.Time != "2026-09-05T01:49:42.144+02:00" ||
					entry.Component != string(component) || entry.Stream != "stderr" ||
					entry.Upstream.Source != "main.go:1730" || entry.Upstream.Old != 100 ||
					entry.Upstream.New != 75 || entry.Upstream.Component != "tsdb" {
					t.Errorf("upstream log fields were not preserved: %+v", entry)
				}
			}
			for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
				if !found[level] {
					t.Errorf("missing %s upstream log; output: %s", level, output.String())
				}
			}
		})
	}
}

func TestWaitForRestartReturnsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRestart(ctx, 100*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("wait for restart error = %v, want context canceled", err)
	}
}

func TestWaitForRestartReturnsAfterDelay(t *testing.T) {
	if err := waitForRestart(context.Background(), 0); err != nil {
		t.Errorf("wait for restart: %v", err)
	}
}

func TestSuperviseProcessRestartsAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := 0
	var waits []time.Duration
	run := func(context.Context, processSpec, *slog.Logger) error {
		runs++
		if runs == 1 {
			return errors.New("process crashed")
		}

		cancel()
		return context.Canceled
	}
	wait := func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	err := superviseProcess(ctx, processSpec{}, run, wait, discardLogger())
	if err != nil {
		t.Fatalf("supervise process: %v", err)
	}

	if runs != 2 {
		t.Errorf("process runs = %d, want 2", runs)
	}
	if len(waits) != 1 || waits[0] != 0 {
		t.Errorf("restart waits = %v, want [0s]", waits)
	}
}

func TestSuperviseProcessLogsExitAndRestart(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := 0
	run := func(context.Context, processSpec, *slog.Logger) error {
		runs++
		if runs == 1 {
			return errors.New("process crashed")
		}

		cancel()
		return context.Canceled
	}
	wait := func(context.Context, time.Duration) error {
		return nil
	}

	if err := superviseProcess(
		ctx,
		processSpec{component: prometheusComponent},
		run,
		wait,
		logger,
	); err != nil {
		t.Fatalf("supervise process: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var exitEntry struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Component string `json:"component"`
		Error     string `json:"error"`
	}
	if err := decoder.Decode(&exitEntry); err != nil {
		t.Fatalf("decode process log: %v", err)
	}
	if exitEntry.Level != "ERROR" {
		t.Errorf("log level = %q, want ERROR", exitEntry.Level)
	}
	if exitEntry.Message != "process exited" {
		t.Errorf("log message = %q, want %q", exitEntry.Message, "process exited")
	}
	if exitEntry.Component != "prometheus" {
		t.Errorf("log component = %q, want %q", exitEntry.Component, "prometheus")
	}
	if exitEntry.Error != "process crashed" {
		t.Errorf("log error = %q, want %q", exitEntry.Error, "process crashed")
	}

	var restartEntry struct {
		Level     string        `json:"level"`
		Message   string        `json:"msg"`
		Component string        `json:"component"`
		Delay     time.Duration `json:"delay"`
	}
	if err := decoder.Decode(&restartEntry); err != nil {
		t.Fatalf("decode restart log: %v", err)
	}
	if restartEntry.Level != "INFO" {
		t.Errorf("restart log level = %q, want INFO", restartEntry.Level)
	}
	if restartEntry.Message != "restarting" {
		t.Errorf("restart log message = %q, want %q", restartEntry.Message, "restarting")
	}
	if restartEntry.Component != "prometheus" {
		t.Errorf("restart log component = %q, want %q", restartEntry.Component, "prometheus")
	}
	if restartEntry.Delay != 0 {
		t.Errorf("restart log delay = %s, want 0s", restartEntry.Delay)
	}
}

func TestSuperviseProcessRestartsAfterCleanExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := 0
	var waits []time.Duration
	run := func(context.Context, processSpec, *slog.Logger) error {
		runs++
		if runs == 1 {
			return nil
		}

		cancel()
		return context.Canceled
	}
	wait := func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	if err := superviseProcess(ctx, processSpec{}, run, wait, discardLogger()); err != nil {
		t.Fatalf("supervise process: %v", err)
	}

	if runs != 2 {
		t.Errorf("process runs = %d, want 2", runs)
	}
	if len(waits) != 1 || waits[0] != 0 {
		t.Errorf("restart waits = %v, want [0s]", waits)
	}
}

func TestSuperviseProcessStopsWhenContextCanceledDuringRestartWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	run := func(context.Context, processSpec, *slog.Logger) error {
		return errors.New("process crashed")
	}
	wait := func(_ context.Context, _ time.Duration) error {
		cancel()
		return context.Canceled
	}

	if err := superviseProcess(ctx, processSpec{}, run, wait, discardLogger()); err != nil {
		t.Errorf("supervise process: %v", err)
	}
}

func TestSuperviseProcessIncreasesRestartDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := 0
	var waits []time.Duration
	run := func(context.Context, processSpec, *slog.Logger) error {
		runs++
		if runs == 8 {
			cancel()
			return context.Canceled
		}

		return errors.New("process crashed")
	}
	wait := func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	if err := superviseProcess(ctx, processSpec{}, run, wait, discardLogger()); err != nil {
		t.Fatalf("supervise process: %v", err)
	}

	want := []time.Duration{
		0,
		time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	if len(waits) != len(want) {
		t.Fatalf("restart waits = %v, want %v", waits, want)
	}
	for index, wantDelay := range want {
		if waits[index] != wantDelay {
			t.Errorf("restart wait %d = %s, want %s", index, waits[index], wantDelay)
		}
	}
}

func TestRunProcessHelper(t *testing.T) {
	var helperArguments []string
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			helperArguments = os.Args[index+1:]
			break
		}
	}
	if len(helperArguments) == 0 {
		return
	}
	if helperArguments[0] == "write-json-output" {
		for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
			writeTestOutput(t, os.Stderr, fmt.Sprintf(
				`{"time":"2026-09-05T01:49:42.144+02:00","level":%q,"source":"main.go:1730","msg":"updated GOGC","old":100,"new":75,"component":"tsdb"}`, level,
			))
		}
		return
	}
	if helperArguments[0] == "graceful-shutdown" {
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)

		writeTestFile(t, helperArguments[1], "ready")
		<-interrupts
		writeTestFile(t, helperArguments[2], "stopped")

		return
	}
	if helperArguments[0] == "write-output" {
		writeTestOutput(t, os.Stdout, "component standard output")
		writeTestOutput(t, os.Stderr, "component standard error")

		return
	}

	writeTestFile(t, helperArguments[0], "started")
}

func writeTestOutput(t *testing.T, destination io.Writer, message string) {
	t.Helper()

	if _, err := fmt.Fprintln(destination, message); err != nil {
		t.Fatalf("write process test output: %v", err)
	}
}

func waitForTestFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for test file %q: deadline exceeded", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	//nolint:gosec // marker path is generated by the parent test inside t.TempDir
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write process marker: %v", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
