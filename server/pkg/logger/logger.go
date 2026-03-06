package logger

import (
	"log/slog"
	"os"
)

var Log *slog.Logger

// Init initializes the structured logger. Call once at startup.
func Init(env string) {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if env == "development" {
		opts.Level = slog.LevelDebug
		// Text handler for dev (human-readable)
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		// JSON handler for production (machine-parseable)
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	Log = slog.New(handler)
	slog.SetDefault(Log)
}

// Convenience wrappers
func Info(msg string, args ...any) {
	Log.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	Log.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	Log.Error(msg, args...)
}

func Debug(msg string, args ...any) {
	Log.Debug(msg, args...)
}
