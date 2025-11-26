package main

import (
	"log"

	"grid-trading-bot/internal/config"
	"grid-trading-bot/internal/logger"
	"grid-trading-bot/ui"

	"go.uber.org/zap"
)

func main() {
	const configPath = "configs/config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	zapLogger, err := logger.New(cfg.App.Environment)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer func() {
		if syncErr := zapLogger.Sync(); syncErr != nil {
			log.Printf("Failed to sync logger: %v", syncErr)
		}
	}()

	zapLogger.Info("Starting Grid Trading Bot", zap.String("version", "0.1.0"))

	zapLogger.Info("Configuration loaded successfully",
		zap.String("path", configPath),
		zap.String("environment", cfg.App.Environment),
		zap.Bool("testNet", cfg.OKX.TestNet),
		zap.Int("serverPort", cfg.Server.Port),
		zap.Bool("telegramEnabled", cfg.Telegram.Enabled))

	zapLogger.Info("Grid Trading Bot initialized successfully")

	// Initialize HTTP server
	server := &ui.HTTPServer{}
	zapLogger.Info("Starting HTTP server", zap.Int("port", cfg.Server.Port))
	if err := server.Start(cfg.Server.Port); err != nil {
		zapLogger.Fatal("Failed to start HTTP server", zap.Error(err))
	}
}
