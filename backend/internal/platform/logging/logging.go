package logging

import (
	"fmt"
	"log/slog"
	"os"
)

func New(environment, level, version string) (*slog.Logger, error) {
	logLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	options := &slog.HandlerOptions{
		Level: logLevel,
	}

	var handler slog.Handler

	if environment == "development" {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	logger := slog.New(handler).With(
		slog.String("service", "audio-speech-vault"),
		slog.String("environment", environment),
		slog.String("version", version),
	)

	return logger, nil
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf(
			"unsupported log level %q",
			level,
		)
	}
}
