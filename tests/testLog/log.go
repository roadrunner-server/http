package testLog //nolint: stylecheck

import (
	"log/slog"
	"os"
)

func SlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}
