// Package logger provides structured logging functionality using zap.
package logger

import "go.uber.org/zap"

// New creates a new zap logger instance.
// If development is true, it uses DevelopmentConfig (console output, debug level).
// Otherwise, it uses ProductionConfig (JSON output, info level).
func New(development bool) (*zap.Logger, error) {
	var logger *zap.Logger
	var err error

	if development {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}

	if err != nil {
		return nil, err
	}

	return logger, nil
}

// NewNop creates a no-op logger for testing purposes.
func NewNop() *zap.Logger {
	return zap.NewNop()
}
