package opamp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogLoggerFormatsMessages(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(
		&output,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	))
	adapter := &slogLogger{logger: logger}

	adapter.Debugf(context.Background(), "agent %d connected", 7)
	adapter.Errorf(context.Background(), "agent %d failed", 8)

	logs := output.String()
	if !strings.Contains(logs, `level=DEBUG msg="agent 7 connected"`) {
		t.Errorf("debug log missing from %q", logs)
	}
	if !strings.Contains(logs, `level=ERROR msg="agent 8 failed"`) {
		t.Errorf("error log missing from %q", logs)
	}
}
