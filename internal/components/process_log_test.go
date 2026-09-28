package components

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestProcessLogWriterFramesAndBoundsLines(t *testing.T) {
	var output bytes.Buffer
	writer := newProcessLogWriter(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)))
	chunks := []string{
		`{"level":"INFO","msg":"split`,
		" line", "\",\"sequence\":9007199254740993}\nplain line\n",
		strings.Repeat("x", maxProcessLogLineSize+10),
		"\n{\"level\":\"WARN\",\"msg\":\"recovered\"}\nlast line",
	}
	for _, chunk := range chunks {
		written, err := writer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("Write() = %d, %v, want %d, nil", written, err, len(chunk))
		}
	}
	writer.flush(false)
	decoder := json.NewDecoder(&output)
	for index, message := range []string{"split line", "plain line", strings.Repeat("x", maxProcessLogLineSize), "recovered", "last line"} {
		var entry struct {
			Message   string `json:"msg"`
			Level     string `json:"level"`
			Truncated bool   `json:"truncated"`
			Upstream  struct {
				Sequence int64 `json:"sequence"`
			} `json:"upstream"`
		}
		if err := decoder.Decode(&entry); err != nil {
			t.Fatalf("decode entry %d: %v", index, err)
		}
		if entry.Message != message || entry.Truncated != (index == 2) {
			t.Errorf("entry %d has wrong message or truncation flag", index)
		}
		if index == 0 && entry.Upstream.Sequence != 9007199254740993 {
			t.Errorf("sequence = %d, want exact integer", entry.Upstream.Sequence)
		}
		if index == 3 && entry.Level != "WARN" {
			t.Errorf("recovered level = %s, want WARN", entry.Level)
		}
	}
	if decoder.More() {
		t.Error("unexpected additional log entries")
	}
}
