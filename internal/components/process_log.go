package components

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

const maxProcessLogLineSize = 64 * 1024

// processLogWriter receives one process stream; os/exec serializes its writes.
type processLogWriter struct {
	ctx        context.Context //nolint:containedctx // io.Writer has no context parameter; this writer belongs to one process run
	logger     *slog.Logger
	line       []byte
	discarding bool
}

func newProcessLogWriter(ctx context.Context, logger *slog.Logger) *processLogWriter {
	return &processLogWriter{ctx: ctx, logger: logger}
}

func (writer *processLogWriter) emit(line []byte, truncated bool) {
	record := processLogRecord(line)
	if truncated {
		record.AddAttrs(slog.Bool("truncated", true))
	}
	if writer.logger.Enabled(writer.ctx, record.Level) {
		// Logging failure must not interrupt or restart a managed process.
		_ = writer.logger.Handler().Handle(writer.ctx, record) //nolint:errcheck // best-effort logging, like slog.Logger
	}
}

func (writer *processLogWriter) Write(content []byte) (int, error) {
	for _, character := range content {
		if character == '\n' {
			writer.flush(false)
			writer.discarding = false
			continue
		}
		if writer.discarding {
			continue
		}
		if len(writer.line) == maxProcessLogLineSize {
			writer.flush(true)
			writer.discarding = true
			continue
		}
		writer.line = append(writer.line, character)
	}

	return len(content), nil
}

func (writer *processLogWriter) flush(truncated bool) {
	if len(writer.line) == 0 {
		return
	}
	line := bytes.TrimSuffix(writer.line, []byte{'\r'})
	writer.emit(line, truncated)
	writer.line = writer.line[:0]
}

func processLogRecord(line []byte) slog.Record {
	var entry struct {
		Time    time.Time  `json:"time"`
		Level   slog.Level `json:"level"`
		Message *string    `json:"msg"`
	}
	if err := json.Unmarshal(line, &entry); err != nil || entry.Message == nil {
		return slog.NewRecord(time.Now(), slog.LevelInfo, string(line), 0)
	}
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	record := slog.NewRecord(entry.Time, entry.Level, *entry.Message, 0)
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return record
	}
	delete(fields, "time")
	delete(fields, "level")
	delete(fields, "msg")
	attributes := make([]slog.Attr, 0, len(fields))
	for key, value := range fields {
		attributes = append(attributes, slog.Any(key, value))
	}
	record.AddAttrs(slog.GroupAttrs("upstream", attributes...))
	return record
}
