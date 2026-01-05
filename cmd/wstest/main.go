package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grid-trading-bot/internal/okx"

	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	// Load credentials from environment
	apiKey := os.Getenv("OKX_API_KEY")
	secretKey := os.Getenv("OKX_SECRET_KEY")
	passphrase := os.Getenv("OKX_PASSPHRASE")

	// Check if we should use demo trading
	simulated := os.Getenv("OKX_SIMULATED") != "false"

	logger.Info("Starting WebSocket test",
		zap.Bool("simulated", simulated),
		zap.Bool("hasApiKey", apiKey != ""))

	// Create WebSocket client
	wsClient := okx.NewWebSocketClient(&okx.WSConfig{
		APIKey:     apiKey,
		SecretKey:  secretKey,
		Passphrase: passphrase,
		Simulated:  simulated,
	}, logger)

	// Set up handlers
	wsClient.SetTickerHandler(func(ticker *okx.WSTicker) {
		logger.Info("Ticker update",
			zap.String("instId", ticker.InstID),
			zap.String("last", ticker.Last),
			zap.String("bid", ticker.BidPrice),
			zap.String("ask", ticker.AskPrice))
	})

	wsClient.SetOrderHandler(func(order *okx.WSOrder) {
		logger.Info("Order update",
			zap.String("orderId", order.OrderID),
			zap.String("instId", order.InstID),
			zap.String("side", order.Side),
			zap.String("state", order.State),
			zap.String("fillSz", order.AccFillSize))
	})

	wsClient.SetConnectedHandler(func(channelType string) {
		logger.Info("Connected", zap.String("channel", channelType))
	})

	wsClient.SetDisconnectedHandler(func(channelType string, err error) {
		logger.Warn("Disconnected",
			zap.String("channel", channelType),
			zap.Error(err))
	})

	ctx := context.Background()

	// Connect to public channel
	logger.Info("Connecting to public WebSocket...")
	if err := wsClient.ConnectPublic(ctx); err != nil {
		logger.Fatal("Failed to connect to public WebSocket", zap.Error(err))
	}

	// Subscribe to ticker
	instID := "ETH-USDT-SWAP"
	logger.Info("Subscribing to ticker", zap.String("instId", instID))
	if err := wsClient.SubscribeTicker(instID); err != nil {
		logger.Error("Failed to subscribe to ticker", zap.Error(err))
	}

	// Connect to private channel if credentials are provided
	if apiKey != "" && secretKey != "" && passphrase != "" {
		logger.Info("Connecting to private WebSocket...")
		if err := wsClient.ConnectPrivate(ctx); err != nil {
			logger.Error("Failed to connect to private WebSocket", zap.Error(err))
		} else {
			logger.Info("Subscribing to orders", zap.String("instId", instID))
			if err := wsClient.SubscribeOrders(instID); err != nil {
				logger.Error("Failed to subscribe to orders", zap.Error(err))
			}
		}
	} else {
		logger.Info("Skipping private channel (no credentials provided)")
	}

	// Print status
	fmt.Println("\n========================================")
	fmt.Println("WebSocket Test Running")
	fmt.Println("========================================")
	fmt.Printf("Public Connected:  %v\n", wsClient.IsPublicConnected())
	fmt.Printf("Private Connected: %v\n", wsClient.IsPrivateConnected())
	fmt.Printf("Authenticated:     %v\n", wsClient.IsAuthenticated())
	fmt.Println("========================================")
	fmt.Println("Press Ctrl+C to exit")
	fmt.Println("========================================")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Keep running until interrupted
	select {
	case <-sigChan:
		logger.Info("Shutting down...")
	case <-time.After(5 * time.Minute):
		logger.Info("Test timeout, shutting down...")
	}

	// Disconnect
	wsClient.Disconnect()
	logger.Info("WebSocket test completed")
}
