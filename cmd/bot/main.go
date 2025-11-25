// Package main is the entry point for the Grid Trading Bot application.
package main

import (
	"flag"
	"log"

	"grid-trading-bot/internal/config"
	"grid-trading-bot/internal/logger"

	"go.uber.org/zap"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.example.yaml", "Path to configuration file")
	dev := flag.Bool("dev", true, "Run in development mode")
	flag.Parse()

	// Initialize logger
	zapLogger, err := logger.New(*dev)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	// Ensure logger flushes buffered logs before program exits
	// Sync() does three things:
	//   1. Flushes buffer: zap buffers logs in memory for performance
	//   2. Writes to disk: writes all buffered logs to file/stdout
	//   3. Prevents loss: ensures no logs are lost when program terminates
	defer func() {
		if syncErr := zapLogger.Sync(); syncErr != nil {
			log.Printf("Failed to sync logger: %v", syncErr)
		}
	}()

	zapLogger.Info("Starting Grid Trading Bot", zap.String("version", "0.1.0"))

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		zapLogger.Fatal("Failed to load configuration", zap.Error(err))
	}
	zapLogger.Info("Configuration loaded successfully",
		zap.String("path", *configPath),
		zap.Bool("testNet", cfg.OKX.TestNet),
		zap.Int("serverPort", cfg.Server.Port),
		zap.Bool("telegramEnabled", cfg.Telegram.Enabled))

	zapLogger.Info("Grid Trading Bot initialized successfully")
	zapLogger.Info("Ready to start trading (implementation pending)")
}
