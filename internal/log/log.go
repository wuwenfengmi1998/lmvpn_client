// Package log configures structured logging (slog) to a rotating file
// with a console mirror. Separate log files are used for the GUI and
// the privileged daemon.
package log

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Role selects which log file to write to.
type Role string

const (
	RoleGUI    Role = "gui"
	RoleDaemon Role = "daemon"
)

var logger *slog.Logger

// Init configures the package-level logger writing to the given file
// path (rotated by size) and to stderr.
func Init(role Role, logFile string) *slog.Logger {
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		// Fall back to stderr-only on failure.
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
		return logger
	}
	w := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10, // MB
		MaxBackups: 3,
		MaxAge:     30, // days
		LocalTime:  true,
	}
	mw := io.MultiWriter(os.Stderr, w)
	logger = slog.New(slog.NewTextHandler(mw, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	return logger
}

// L returns the package-level logger. It returns a default logger if
// Init was not called.
func L() *slog.Logger {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return logger
}
