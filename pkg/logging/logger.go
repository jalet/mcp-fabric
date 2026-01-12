// Package logging provides a shared Zap logger with configurable log levels.
package logging

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a new Zap SugaredLogger with the specified component name.
// It reads the LOG_LEVEL environment variable to set the log level.
// Valid levels: debug, info, warn, error (case-insensitive).
// Defaults to info if not set or invalid.
func NewLogger(component string) *zap.SugaredLogger {
	level := parseLogLevel(os.Getenv("LOG_LEVEL"))

	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    buildEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := config.Build()
	if err != nil {
		// Fallback to a basic logger if config fails
		logger, _ = zap.NewProduction()
	}

	return logger.Named(component).Sugar()
}

// NewLoggerWithLevel creates a logger with an explicit level (for testing or special cases).
func NewLoggerWithLevel(component string, level zapcore.Level) *zap.SugaredLogger {
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    buildEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, err := config.Build()
	if err != nil {
		logger, _ = zap.NewProduction()
	}

	return logger.Named(component).Sugar()
}

// ParseLogLevel converts a string log level to zapcore.Level.
// Exported for use by operator's controller-runtime integration.
func ParseLogLevel(levelStr string) zapcore.Level {
	return parseLogLevel(levelStr)
}

func parseLogLevel(levelStr string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return zapcore.DebugLevel
	case "info", "":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func buildEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}
