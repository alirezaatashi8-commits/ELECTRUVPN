package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New creates the process-wide structured logger. JSON is the default because
// it is directly consumable by Loki, ELK, and other log aggregation systems.
func New() *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ELECTRU_LOG_LEVEL"))) {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn", "warning":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	opts := &slog.HandlerOptions{Level: level, AddSource: os.Getenv("ELECTRU_LOG_SOURCE") == "1"}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ELECTRU_LOG_FORMAT")), "text") {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
