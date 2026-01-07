package grid

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"grid-trading-bot/internal/okx"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Default configuration constants (kept for backwards compatibility)
const (
	DefaultRiskCheckInterval = 30 * time.Second // Default interval for risk checks
	DefaultRebuildTimeout    = 30 * time.Second // Default timeout for grid rebuild operations
)

// Functional options for StrategyManager
type StrategyOption func(*StrategyManager)

// WithConfig sets custom configuration for the strategy manager
func WithConfig(config StrategyConfig) StrategyOption {
	return func(m *StrategyManager) {
		config.Validate()
		m.config = config
	}
}

// WSClient defines the WebSocket client interface
type WSClient interface {
	ConnectPublic(ctx context.Context) error
	ConnectPrivate(ctx context.Context) error
	SubscribeTicker(instID string) error
	SubscribeOrders(instID string) error
	SetTickerHandler(handler func(ticker *okx.WSTicker))
	SetOrderHandler(handler func(order *okx.WSOrder))
	SetConnectedHandler(handler func(channelType string))
	SetDisconnectedHandler(handler func(channelType string, err error))
	IsPublicConnected() bool
	IsPrivateConnected() bool
	Disconnect()
}

// StrategyManager manages the grid trading strategy lifecycle
type StrategyManager struct {
	restClient ExchangeClient
	wsClient   WSClient
	logger     *zap.Logger
	config     StrategyConfig

	strategy   *Strategy
	calculator *Calculator
	orderMgr   *OrderManager
	riskCtrl   *RiskController

	// Control channels
	stopChan chan struct{}
	doneChan chan struct{}

	// Atomic flag to prevent concurrent grid rebuilds
	rebuilding int32

	mu sync.RWMutex
}

// NewStrategyManager creates a new strategy manager
// Options can be passed to customize the configuration using WithConfig()
func NewStrategyManager(
	restClient ExchangeClient,
	wsClient WSClient,
	logger *zap.Logger,
	opts ...StrategyOption,
) *StrategyManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	m := &StrategyManager{
		restClient: restClient,
		wsClient:   wsClient,
		logger:     logger,
		config:     DefaultStrategyConfig(),
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Initialize calculator with config
	m.calculator = NewCalculatorWithPrecision(logger, m.config.OrderPrecision)

	return m
}

// Start starts the grid trading strategy
func (m *StrategyManager) Start(ctx context.Context, params GridParams, riskParams RiskParams) error {
	m.mu.Lock()
	if m.strategy != nil && m.strategy.IsRunning() {
		m.mu.Unlock()
		return ErrStrategyAlreadyRunning
	}
	m.mu.Unlock()

	m.logger.Info("Starting grid strategy",
		zap.String("instId", params.InstID),
		zap.String("lowerBound", params.LowerBound.String()),
		zap.String("upperBound", params.UpperBound.String()),
		zap.Int("gridCount", params.GridCount))

	// Step 1: Validate parameters
	if err := m.validateParams(params); err != nil {
		return err
	}

	// Step 2: Get current market price
	ticker, err := m.restClient.GetTicker(ctx, params.InstID)
	if err != nil {
		return WrapError("GetTicker", err)
	}

	currentPrice, err := decimal.NewFromString(ticker.Last)
	if err != nil {
		return WrapError("ParsePrice", err)
	}

	m.logger.Info("Current market price", zap.String("price", currentPrice.String()))

	// Step 3: Calculate grid levels
	levels, err := m.calculator.CalculateGridLevels(params)
	if err != nil {
		return err
	}

	// Step 4: Calculate order size
	orderSize := m.calculator.CalculateOrderSize(params, currentPrice)
	if orderSize.IsZero() {
		return NewStrategyError("CalculateOrderSize", ErrInvalidParams, map[string]interface{}{
			"reason": "calculated order size is zero",
		})
	}

	// Step 5: Create strategy instance
	strategyID := generateStrategyID()
	m.mu.Lock()
	m.strategy = NewStrategy(strategyID, params, riskParams)
	m.strategy.Levels = levels
	m.strategy.OrderSize = orderSize
	m.strategy.SetStartTime(time.Now())
	m.mu.Unlock()

	// Step 6: Initialize risk controller
	m.riskCtrl = NewRiskControllerWithCurrency(riskParams, m.restClient, m.logger, m.config.QuoteCurrency)
	if err := m.riskCtrl.InitializeEquity(ctx, &m.strategy.Metrics, params.InstID); err != nil {
		return err
	}

	// Step 7: Initialize order manager
	m.orderMgr = NewOrderManager(m.restClient, m.calculator, m.logger)

	// Step 8: Setup WebSocket connections
	if err := m.setupWebSocket(ctx, params.InstID); err != nil {
		return err
	}

	// Step 9: Place initial orders
	if err := m.orderMgr.PlaceInitialOrders(ctx, m.strategy, currentPrice); err != nil {
		// Partial failure is a warning, not a fatal error
		if errors.Is(err, ErrOrderPartialFailure) {
			m.logger.Warn("Some initial orders failed to place", zap.Error(err))
		} else {
			return err
		}
	}

	// Step 10: Start background tasks
	m.stopChan = make(chan struct{})
	m.doneChan = make(chan struct{})
	go m.runRiskCheckLoop()

	// Set state to running
	m.strategy.SetState(StateRunning)

	m.logger.Info("Grid strategy started",
		zap.String("strategyId", strategyID),
		zap.String("orderSize", orderSize.String()),
		zap.Int("orderCount", m.orderMgr.GetActiveOrderCount(m.strategy)))

	return nil
}

// Stop stops the grid trading strategy
func (m *StrategyManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.strategy == nil {
		m.mu.Unlock()
		return ErrStrategyNotRunning
	}

	if m.strategy.GetState() == StateStopping || m.strategy.GetState() == StateStopped {
		m.mu.Unlock()
		return ErrStrategyStopping
	}

	m.strategy.SetState(StateStopping)
	m.mu.Unlock()

	m.logger.Info("Stopping grid strategy")

	// Signal background tasks to stop
	close(m.stopChan)

	// Wait for background tasks with timeout
	select {
	case <-m.doneChan:
	case <-time.After(10 * time.Second):
		m.logger.Warn("Timeout waiting for background tasks")
	}

	// Cancel all pending orders
	if err := m.orderMgr.CancelAllOrders(ctx, m.strategy); err != nil {
		m.logger.Error("Failed to cancel orders", zap.Error(err))
	}

	// Disconnect WebSocket
	if m.wsClient != nil {
		m.wsClient.Disconnect()
	}

	m.strategy.SetState(StateStopped)
	m.strategy.SetStopTime(time.Now())

	m.logger.Info("Grid strategy stopped",
		zap.String("strategyId", m.strategy.ID),
		zap.Int("totalTrades", m.strategy.Metrics.TotalTrades))

	return nil
}

// EmergencyStop performs emergency stop with position closure
func (m *StrategyManager) EmergencyStop(ctx context.Context) error {
	m.mu.Lock()
	if m.strategy == nil {
		m.mu.Unlock()
		return ErrStrategyNotRunning
	}
	m.strategy.SetState(StateEmergency)
	m.mu.Unlock()

	m.logger.Warn("Emergency stop triggered")

	// Signal background tasks
	select {
	case <-m.stopChan:
	default:
		close(m.stopChan)
	}

	// Emergency close through risk controller
	if m.riskCtrl != nil {
		if err := m.riskCtrl.EmergencyClose(ctx, m.strategy.Params.InstID, m.strategy.Params.TradeMode); err != nil {
			m.logger.Error("Emergency close failed", zap.Error(err))
			return err
		}
	}

	// Disconnect WebSocket
	if m.wsClient != nil {
		m.wsClient.Disconnect()
	}

	m.strategy.SetStopTime(time.Now())

	m.logger.Info("Emergency stop completed")

	return nil
}

// GetStatus returns the current strategy status
func (m *StrategyManager) GetStatus() *StrategyStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.strategy == nil {
		return &StrategyStatus{State: StateIdle}
	}

	return &StrategyStatus{
		ID:           m.strategy.ID,
		State:        m.strategy.GetState(),
		Params:       m.strategy.Params,
		Metrics:      m.strategy.GetMetrics(),
		ActiveOrders: m.orderMgr.GetActiveOrderCount(m.strategy),
		StartTime:    m.strategy.GetStartTime(),
	}
}

// GetMetrics returns the current strategy metrics
func (m *StrategyManager) GetMetrics() StrategyMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.strategy == nil {
		return StrategyMetrics{}
	}

	return m.strategy.GetMetrics()
}

// StrategyStatus represents the current strategy status
type StrategyStatus struct {
	ID           string
	State        StrategyState
	Params       GridParams
	Metrics      StrategyMetrics
	ActiveOrders int
	StartTime    time.Time
}

// setupWebSocket sets up WebSocket connections and handlers
func (m *StrategyManager) setupWebSocket(ctx context.Context, instID string) error {
	// Set handlers before connecting
	m.wsClient.SetTickerHandler(m.handleTickerUpdate)
	m.wsClient.SetOrderHandler(m.handleOrderUpdate)
	m.wsClient.SetConnectedHandler(m.handleConnected)
	m.wsClient.SetDisconnectedHandler(m.handleDisconnected)

	// Connect to public channel for ticker
	if err := m.wsClient.ConnectPublic(ctx); err != nil {
		return WrapError("ConnectPublic", err)
	}

	// Connect to private channel for orders
	if err := m.wsClient.ConnectPrivate(ctx); err != nil {
		return WrapError("ConnectPrivate", err)
	}

	// Subscribe to ticker
	if err := m.wsClient.SubscribeTicker(instID); err != nil {
		return WrapError("SubscribeTicker", err)
	}

	// Subscribe to orders
	if err := m.wsClient.SubscribeOrders(instID); err != nil {
		return WrapError("SubscribeOrders", err)
	}

	m.logger.Info("WebSocket setup completed", zap.String("instId", instID))

	return nil
}

// handleTickerUpdate processes ticker updates from WebSocket
func (m *StrategyManager) handleTickerUpdate(ticker *okx.WSTicker) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	if strategy == nil || !strategy.IsRunning() {
		return
	}

	lastPrice, err := decimal.NewFromString(ticker.Last)
	if err != nil {
		m.logger.Warn("Failed to parse ticker price", zap.Error(err))
		return
	}

	// Update last price
	strategy.UpdateMetrics(func(metrics *StrategyMetrics) {
		metrics.LastPrice = lastPrice
		metrics.UpdateTime = time.Now()
	})

	// Check if grid rebuild is needed
	if m.calculator.NeedRebuild(strategy.Levels, lastPrice) {
		// Use atomic flag to prevent concurrent rebuilds
		if !atomic.CompareAndSwapInt32(&m.rebuilding, 0, 1) {
			m.logger.Debug("Grid rebuild already in progress, skipping")
			return
		}

		m.logger.Info("Price breakout detected, rebuilding grid",
			zap.String("price", lastPrice.String()))

		go func() {
			defer atomic.StoreInt32(&m.rebuilding, 0)

			ctx, cancel := context.WithTimeout(context.Background(), m.config.RebuildTimeout)
			defer cancel()

			if err := m.rebuildGrid(ctx, lastPrice); err != nil {
				m.logger.Error("Failed to rebuild grid", zap.Error(err))
			}
		}()
	}
}

// handleOrderUpdate processes order updates from WebSocket
func (m *StrategyManager) handleOrderUpdate(order *okx.WSOrder) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	if strategy == nil || !strategy.IsRunning() {
		return
	}

	m.logger.Debug("Order update received",
		zap.String("orderId", order.OrderID),
		zap.String("state", order.State),
		zap.String("side", order.Side))

	switch order.State {
	case "filled":
		m.handleOrderFilled(order)
	case "canceled":
		m.handleOrderCanceled(order)
	case "partially_filled":
		m.handlePartiallyFilled(order)
	}
}

// handleOrderFilled processes filled orders
func (m *StrategyManager) handleOrderFilled(order *okx.WSOrder) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	if strategy == nil {
		return
	}

	filledPrice, _ := decimal.NewFromString(order.FillPrice)
	filledSize, _ := decimal.NewFromString(order.AccFillSize)

	ctx := context.Background()

	if err := m.orderMgr.HandleOrderFilled(ctx, strategy, order.OrderID, filledPrice, filledSize); err != nil {
		if !errors.Is(err, ErrOrderNotFound) {
			m.logger.Error("Failed to handle filled order",
				zap.String("orderId", order.OrderID),
				zap.Error(err))
		}
	}

	// Check risk after trade
	go func() {
		if err := m.checkRiskAndUpdate(ctx); err != nil {
			m.logger.Error("Risk check failed after trade", zap.Error(err))
		}
	}()
}

// handleOrderCanceled processes canceled orders
func (m *StrategyManager) handleOrderCanceled(order *okx.WSOrder) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	if strategy == nil {
		return
	}

	strategy.RemoveOrder(order.OrderID)

	m.logger.Debug("Order canceled",
		zap.String("orderId", order.OrderID))
}

// handlePartiallyFilled processes partially filled orders
func (m *StrategyManager) handlePartiallyFilled(order *okx.WSOrder) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	if strategy == nil {
		return
	}

	gridOrder, ok := strategy.GetOrder(order.OrderID)
	if !ok {
		return
	}

	filledSize, _ := decimal.NewFromString(order.AccFillSize)
	gridOrder.FilledSize = filledSize
	gridOrder.UpdateTime = time.Now()

	m.logger.Debug("Order partially filled",
		zap.String("orderId", order.OrderID),
		zap.String("filledSize", filledSize.String()))
}

// handleConnected handles WebSocket connection events
func (m *StrategyManager) handleConnected(channelType string) {
	m.logger.Info("WebSocket connected", zap.String("channel", channelType))
}

// handleDisconnected handles WebSocket disconnection events
func (m *StrategyManager) handleDisconnected(channelType string, err error) {
	m.logger.Warn("WebSocket disconnected",
		zap.String("channel", channelType),
		zap.Error(err))

	// Attempt automatic reconnection if enabled
	if m.config.EnableAutoReconnect {
		go m.attemptReconnect(channelType)
	}
}

// attemptReconnect attempts to reconnect WebSocket with exponential backoff
func (m *StrategyManager) attemptReconnect(channelType string) {
	m.mu.RLock()
	strategy := m.strategy
	m.mu.RUnlock()

	// Don't reconnect if strategy is not running
	if strategy == nil || !strategy.IsRunning() {
		m.logger.Debug("Skipping reconnection - strategy not running")
		return
	}

	instID := strategy.Params.InstID
	attempt := 0
	delay := m.config.ReconnectBaseDelay

	for {
		// Check if strategy is still running
		m.mu.RLock()
		strategy = m.strategy
		m.mu.RUnlock()

		if strategy == nil || !strategy.IsRunning() {
			m.logger.Debug("Stopping reconnection - strategy no longer running")
			return
		}

		// Check max attempts
		if m.config.ReconnectMaxAttempts > 0 && attempt >= m.config.ReconnectMaxAttempts {
			m.logger.Error("WebSocket reconnection failed - max attempts reached",
				zap.String("channel", channelType),
				zap.Int("attempts", attempt))

			// Call the failure callback if configured
			if m.config.OnReconnectFailed != nil {
				m.config.OnReconnectFailed(channelType, attempt)
			}

			// Trigger emergency stop if configured
			if m.config.EmergencyStopOnFailure {
				m.logger.Warn("Triggering emergency stop due to reconnection failure")
				go func() {
					if err := m.EmergencyStop(context.Background()); err != nil {
						m.logger.Error("Failed to execute emergency stop", zap.Error(err))
					}
				}()
			}

			return
		}

		attempt++
		m.logger.Info("Attempting WebSocket reconnection",
			zap.String("channel", channelType),
			zap.Int("attempt", attempt),
			zap.Duration("delay", delay))

		// Wait before reconnecting
		select {
		case <-m.stopChan:
			m.logger.Debug("Reconnection cancelled - strategy stopping")
			return
		case <-time.After(delay):
		}

		// Attempt reconnection
		ctx, cancel := context.WithTimeout(context.Background(), m.config.ReconnectTimeout)
		var reconnectErr error

		if channelType == "public" {
			reconnectErr = m.wsClient.ConnectPublic(ctx)
			if reconnectErr == nil {
				reconnectErr = m.wsClient.SubscribeTicker(instID)
			}
		} else if channelType == "private" {
			reconnectErr = m.wsClient.ConnectPrivate(ctx)
			if reconnectErr == nil {
				reconnectErr = m.wsClient.SubscribeOrders(instID)
			}
		}
		cancel()

		if reconnectErr == nil {
			m.logger.Info("WebSocket reconnection successful",
				zap.String("channel", channelType),
				zap.Int("attempts", attempt))
			return
		}

		m.logger.Warn("WebSocket reconnection failed, will retry",
			zap.String("channel", channelType),
			zap.Int("attempt", attempt),
			zap.Error(reconnectErr))

		// Exponential backoff with max delay
		delay = delay * 2
		if delay > m.config.ReconnectMaxDelay {
			delay = m.config.ReconnectMaxDelay
		}
	}
}

// rebuildGrid rebuilds the grid after price breakout
func (m *StrategyManager) rebuildGrid(ctx context.Context, currentPrice decimal.Decimal) error {
	m.mu.Lock()
	strategy := m.strategy
	if strategy == nil || !strategy.IsRunning() {
		m.mu.Unlock()
		return ErrStrategyNotRunning
	}
	m.mu.Unlock()

	m.logger.Info("Rebuilding grid",
		zap.String("currentPrice", currentPrice.String()))

	// Cancel all existing orders
	if err := m.orderMgr.CancelAllOrders(ctx, strategy); err != nil {
		return err
	}

	// Close any open positions
	if m.riskCtrl != nil {
		hasPos, posSize, err := m.riskCtrl.HasOpenPosition(ctx, strategy.Params.InstID)
		if err != nil {
			m.logger.Warn("Failed to check position", zap.Error(err))
		} else if hasPos {
			m.logger.Info("Closing position before grid rebuild",
				zap.String("size", posSize.String()))
			if err := m.riskCtrl.EmergencyClose(ctx, strategy.Params.InstID, strategy.Params.TradeMode); err != nil {
				return WrapError("ClosePosition", err)
			}
		}
	}

	// Calculate new grid
	newLevels, newParams, err := m.calculator.RebuildGrid(strategy.Params, currentPrice)
	if err != nil {
		return err
	}

	// Update strategy
	m.mu.Lock()
	strategy.Levels = newLevels
	strategy.Params = newParams
	strategy.Orders = make(map[string]*GridOrder)
	m.mu.Unlock()

	// Place new orders
	if err := m.orderMgr.PlaceInitialOrders(ctx, strategy, currentPrice); err != nil {
		return err
	}

	m.logger.Info("Grid rebuild completed",
		zap.String("newLower", newParams.LowerBound.String()),
		zap.String("newUpper", newParams.UpperBound.String()))

	return nil
}

// runRiskCheckLoop runs periodic risk checks
func (m *StrategyManager) runRiskCheckLoop() {
	defer close(m.doneChan)

	ticker := time.NewTicker(m.config.RiskCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			ctx := context.Background()
			if err := m.checkRiskAndUpdate(ctx); err != nil {
				if IsFatal(err) {
					m.logger.Error("Fatal risk condition detected", zap.Error(err))
					_ = m.EmergencyStop(ctx)
					return
				}
				m.logger.Warn("Risk check warning", zap.Error(err))
			}
		}
	}
}

// checkRiskAndUpdate performs risk check and updates metrics
func (m *StrategyManager) checkRiskAndUpdate(ctx context.Context) error {
	m.mu.RLock()
	strategy := m.strategy
	riskCtrl := m.riskCtrl
	m.mu.RUnlock()

	if strategy == nil || riskCtrl == nil || !strategy.IsRunning() {
		return nil
	}

	// Get current metrics for calculation (thread-safe read)
	currentMetrics := strategy.GetMetrics()

	// Calculate equity without modifying state
	equityUpdate, err := riskCtrl.CalculateEquity(
		ctx,
		strategy.Params.InstID,
		currentMetrics.PeakEquity,
		currentMetrics.MaxDrawdown,
	)
	if err != nil {
		return err
	}

	// Apply update through thread-safe method
	strategy.UpdateMetrics(func(m *StrategyMetrics) {
		m.CurrentEquity = equityUpdate.CurrentEquity
		m.UnrealizedPnL = equityUpdate.UnrealizedPnL
		m.PeakEquity = equityUpdate.PeakEquity
		m.MaxDrawdown = equityUpdate.MaxDrawdown
		m.UpdateTime = time.Now()
	})

	// Check risk limits
	return riskCtrl.CheckRisk(strategy.GetMetrics())
}

// validateParams validates strategy parameters
func (m *StrategyManager) validateParams(params GridParams) error {
	if params.InstID == "" {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"reason": "instID is required",
		})
	}

	if params.GridCount <= 0 {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"reason": "gridCount must be positive",
		})
	}

	if params.TotalInvestment.LessThanOrEqual(decimal.Zero) {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"reason": "totalInvestment must be positive",
		})
	}

	if !params.TradeMode.IsValid() {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"reason":    "tradeMode must be 'cross' or 'isolated'",
			"tradeMode": params.TradeMode.String(),
		})
	}

	return nil
}

// generateStrategyID generates a unique strategy ID
func generateStrategyID() string {
	return time.Now().Format("20060102150405")
}
