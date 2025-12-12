package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewLogger(level string) (*zap.Logger, error) {
	config := zap.NewProductionConfig()

	// Parse level
	l, err := zapcore.ParseLevel(level)
	if err != nil {
		l = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(l)

	// Customize encoding if needed (e.g., console for dev)
	// config.Encoding = "console" // or "json"

	return config.Build()
}

func NewFileLogger(path string, level string) (*zap.Logger, error) {
	config := zap.NewProductionConfig()

	// Parse level
	l, err := zapcore.ParseLevel(level)
	if err != nil {
		l = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(l)

	// Output to file and stderr (optional, maybe just file?)
	// User asked for "separate logs", implies *just* separate or *also* separate?
	// Usually separate means distinct file.
	config.OutputPaths = []string{path}
	config.ErrorOutputPaths = []string{path}

	return config.Build()
}
