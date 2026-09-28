package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/RSTCK-Innovation/arveld/internal/app"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/database"
)

func runResetPassword(arguments []string) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	flags := flag.NewFlagSet("arveld reset-password", flag.ContinueOnError)
	configPath := flags.String("config", "data/arveld.yml", "path to the existing Arveld YAML configuration")
	passwordStdin := flags.Bool("password-stdin", false, "read the new password from a file or pipe on standard input")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *configPath == "" || !*passwordStdin {
		logger.Error("use reset-password --password-stdin [--config path] with no positional arguments")
		return 2
	}
	password, err := readResetPassword()
	if err != nil {
		logger.Error("read new password", "error", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := resetPassword(ctx, *configPath, password); err != nil {
		logger.Error("reset administrator password", "error", err)
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, "Administrator password reset; existing sessions invalidated."); err != nil {
		logger.Error("write reset confirmation", "error", err)
		return 1
	}
	return 0
}

func readResetPassword() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect standard input: %w", err)
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("password input must be a file or pipe, not a terminal")
	}
	// At most 128 four-byte code points plus an optional CRLF line ending.
	const maxInputBytes = 128*utf8.UTFMax + 2
	input, err := io.ReadAll(io.LimitReader(os.Stdin, maxInputBytes+1))
	if err != nil {
		return "", fmt.Errorf("read standard input: %w", err)
	}
	if len(input) > maxInputBytes {
		return "", errors.New("password input is too large")
	}
	password := strings.TrimSuffix(string(input), "\n")
	if len(password) < len(input) {
		password = strings.TrimSuffix(password, "\r")
	}
	if strings.ContainsAny(password, "\r\n") {
		return "", errors.New("password input must contain one line")
	}
	if err := auth.ValidatePassword(password); err != nil {
		return "", fmt.Errorf("validate new password: %w", err)
	}
	return password, nil
}

func resetPassword(ctx context.Context, configPath, password string) (returnErr error) {
	config, err := app.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load existing configuration: %w", err)
	}
	if config.DatabasePath == "" {
		return errors.New("database path is required")
	}
	db, err := database.OpenExistingFile(ctx, config.DatabasePath)
	if err != nil {
		return fmt.Errorf("open existing database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close reset database: %w", err))
		}
	}()
	if err := database.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := auth.NewStore(db).ResetPassword(ctx, password); err != nil {
		return fmt.Errorf("update administrator credentials: %w", err)
	}
	return nil
}
