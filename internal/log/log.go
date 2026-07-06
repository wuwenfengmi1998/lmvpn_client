// Package log configures structured logging (slog) to a rotating file
// with a console mirror. Separate log files are used for the GUI and
// the privileged daemon.
//
// When the daemon is launched via daemon-launch, its stderr is already
// redirected to the log file. To avoid duplicate log lines, the daemon
// detects the LMVPN_DAEMON=1 env var and writes only to the file (not
// stderr). The GUI always mirrors to stderr for console visibility.
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
// path (rotated by size). For the GUI it also mirrors to stderr; for
// the forked daemon it writes only to the file (since stderr is
// already redirected to the file by the launcher).
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

	// Determine the output writer. When the daemon was forked by
	// daemon-launch, LMVPN_DAEMON=1 is set and stderr is already
	// pointed at the log file — so write to the file only to avoid
	// doubling every line.
	var out io.Writer
	if role == RoleDaemon && os.Getenv("LMVPN_DAEMON") == "1" {
		out = w
	} else {
		out = io.MultiWriter(os.Stderr, w)
	}

	logger = slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
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
