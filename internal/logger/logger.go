package logger

import (
	"fmt"

	"go.uber.org/zap"
)

func New(env string) (*zap.Logger, error) {
	switch env {
	case "production":
		return zap.NewProduction()
	case "testing":
		return zap.NewNop(), nil
	case "development":
		return zap.NewDevelopment()
	default:
		return nil, fmt.Errorf("invalid environment: %s (must be one of: production, testing, development)", env)
	}
}
