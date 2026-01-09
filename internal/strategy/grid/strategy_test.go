package grid

import (
	"context"
	"errors"
	"testing"
	"time"

	"grid-trading-bot/internal/okx"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockWSClient implements WSClient for testing
type MockWSClient struct {
	ConnectPublicFunc          func(ctx context.Context) error
	ConnectPrivateFunc         func(ctx context.Context) error
	SubscribeTickerFunc        func(instID string) error
	SubscribeOrdersFunc        func(instID string) error
	SetTickerHandlerFunc       func(handler func(ticker *okx.WSTicker))
	SetOrderHandlerFunc        func(handler func(order *okx.WSOrder))
	SetConnectedHandlerFunc    func(handler func(channelType string))
	SetDisconnectedHandlerFunc func(handler func(channelType string, err error))
	IsPublicConnectedFunc      func() bool
	IsPrivateConnectedFunc     func() bool
	DisconnectFunc             func()

	// Store handlers for testing
	tickerHandler       func(ticker *okx.WSTicker)
	orderHandler        func(order *okx.WSOrder)
	connectedHandler    func(channelType string)
	disconnectedHandler func(channelType string, err error)
}

func (m *MockWSClient) ConnectPublic(ctx context.Context) error {
	if m.ConnectPublicFunc != nil {
		return m.ConnectPublicFunc(ctx)
	}
	return nil
}

func (m *MockWSClient) ConnectPrivate(ctx context.Context) error {
	if m.ConnectPrivateFunc != nil {
		return m.ConnectPrivateFunc(ctx)
	}
	return nil
}

func (m *MockWSClient) SubscribeTicker(instID string) error {
	if m.SubscribeTickerFunc != nil {
		return m.SubscribeTickerFunc(instID)
	}
	return nil
}

func (m *MockWSClient) SubscribeOrders(instID string) error {
	if m.SubscribeOrdersFunc != nil {
		return m.SubscribeOrdersFunc(instID)
	}
	return nil
}

func (m *MockWSClient) SetTickerHandler(handler func(ticker *okx.WSTicker)) {
	m.tickerHandler = handler
	if m.SetTickerHandlerFunc != nil {
		m.SetTickerHandlerFunc(handler)
	}
}

func (m *MockWSClient) SetOrderHandler(handler func(order *okx.WSOrder)) {
	m.orderHandler = handler
	if m.SetOrderHandlerFunc != nil {
		m.SetOrderHandlerFunc(handler)
	}
}

func (m *MockWSClient) SetConnectedHandler(handler func(channelType string)) {
	m.connectedHandler = handler
	if m.SetConnectedHandlerFunc != nil {
		m.SetConnectedHandlerFunc(handler)
	}
}

func (m *MockWSClient) SetDisconnectedHandler(handler func(channelType string, err error)) {
	m.disconnectedHandler = handler
	if m.SetDisconnectedHandlerFunc != nil {
		m.SetDisconnectedHandlerFunc(handler)
	}
}

func (m *MockWSClient) IsPublicConnected() bool {
	if m.IsPublicConnectedFunc != nil {
		return m.IsPublicConnectedFunc()
	}
	return true
}

func (m *MockWSClient) IsPrivateConnected() bool {
	if m.IsPrivateConnectedFunc != nil {
		return m.IsPrivateConnectedFunc()
	}
	return true
}

func (m *MockWSClient) Disconnect() {
	if m.DisconnectFunc != nil {
		m.DisconnectFunc()
	}
}

// SimulateTicker simulates a ticker update
func (m *MockWSClient) SimulateTicker(ticker *okx.WSTicker) {
	if m.tickerHandler != nil {
		m.tickerHandler(ticker)
	}
}

// SimulateOrder simulates an order update
func (m *MockWSClient) SimulateOrder(order *okx.WSOrder) {
	if m.orderHandler != nil {
		m.orderHandler(order)
	}
}

func newTestStrategyManager() (*StrategyManager, *MockExchangeClient, *MockWSClient) {
	mockRest := &MockExchangeClient{
		GetTickerFunc: func(ctx context.Context, instID string) (*okx.Ticker, error) {
			return &okx.Ticker{Last: "2250"}, nil
		},
		GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
			return &okx.Balance{Available: "10000"}, nil
		},
		GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
			return []okx.Position{}, nil
		},
		PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
			return "order-" + req.Price, nil
		},
		GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
			return []okx.Order{}, nil
		},
	}

	mockWS := &MockWSClient{}

	manager := NewStrategyManager(mockRest, mockWS, zap.NewNop())
	return manager, mockRest, mockWS
}

func TestStrategyManager_Start(t *testing.T) {
	ctx := context.Background()

	t.Run("successful start", func(t *testing.T) {
		manager, _, _ := newTestStrategyManager()

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		riskParams := RiskParams{
			StopLossRatio: decimal.NewFromFloat(0.2),
			MaxDrawdown:   decimal.NewFromFloat(0.3),
			EmergencyStop: true,
		}

		err := manager.Start(ctx, params, riskParams)
		require.NoError(t, err)

		status := manager.GetStatus()
		assert.Equal(t, StateRunning, status.State)
		assert.Equal(t, "ETH-USDT-SWAP", status.Params.InstID)
		assert.Greater(t, status.ActiveOrders, 0)

		// Clean up
		_ = manager.Stop(ctx)
	})

	t.Run("start twice returns error", func(t *testing.T) {
		manager, _, _ := newTestStrategyManager()

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Try to start again
		err = manager.Start(ctx, params, RiskParams{})
		assert.ErrorIs(t, err, ErrStrategyAlreadyRunning)

		// Clean up
		_ = manager.Stop(ctx)
	})

	t.Run("invalid params returns error", func(t *testing.T) {
		manager, _, _ := newTestStrategyManager()

		params := GridParams{
			InstID:    "", // Invalid - empty
			GridCount: 5,
		}

		err := manager.Start(ctx, params, RiskParams{})
		assert.Error(t, err)
	})
}

func TestStrategyManager_Stop(t *testing.T) {
	ctx := context.Background()

	t.Run("successful stop", func(t *testing.T) {
		disconnectCalled := false

		manager, _, mockWS := newTestStrategyManager()
		mockWS.DisconnectFunc = func() {
			disconnectCalled = true
		}

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		err = manager.Stop(ctx)
		require.NoError(t, err)

		status := manager.GetStatus()
		assert.Equal(t, StateStopped, status.State)
		assert.True(t, disconnectCalled)
	})

	t.Run("stop without start returns error", func(t *testing.T) {
		manager, _, _ := newTestStrategyManager()

		err := manager.Stop(ctx)
		assert.ErrorIs(t, err, ErrStrategyNotRunning)
	})
}

func TestStrategyManager_HandleTickerUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("updates last price", func(t *testing.T) {
		manager, _, mockWS := newTestStrategyManager()

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Simulate ticker update
		mockWS.SimulateTicker(&okx.WSTicker{
			InstID: "ETH-USDT-SWAP",
			Last:   "2300",
		})

		// Give some time for async processing
		time.Sleep(50 * time.Millisecond)

		metrics := manager.GetMetrics()
		assert.True(t, metrics.LastPrice.Equal(decimal.NewFromInt(2300)))

		_ = manager.Stop(ctx)
	})
}

func TestStrategyManager_HandleOrderFilled(t *testing.T) {
	ctx := context.Background()

	t.Run("places opposite order on fill", func(t *testing.T) {
		oppositeOrderPlaced := false

		manager, mockRest, mockWS := newTestStrategyManager()
		mockRest.PlaceOrderFunc = func(ctx context.Context, req *okx.OrderRequest) (string, error) {
			if req.Side == "sell" {
				oppositeOrderPlaced = true
			}
			return "order-" + req.Price, nil
		}

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Get one of the buy orders
		var buyOrderID string
		for orderID, order := range manager.strategy.Orders {
			if order.Side == OrderSideBuy {
				buyOrderID = orderID
				break
			}
		}

		// Simulate order filled
		mockWS.SimulateOrder(&okx.WSOrder{
			OrderID:     buyOrderID,
			State:       "filled",
			Side:        "buy",
			FillPrice:   "2100",
			AccFillSize: "0.5",
		})

		// Give some time for async processing
		time.Sleep(100 * time.Millisecond)

		assert.True(t, oppositeOrderPlaced, "Should place opposite sell order")

		_ = manager.Stop(ctx)
	})
}

func TestStrategyManager_EmergencyStop(t *testing.T) {
	ctx := context.Background()

	t.Run("performs emergency close", func(t *testing.T) {
		cancelCalled := false
		closeCalled := false

		manager, mockRest, _ := newTestStrategyManager()
		mockRest.GetPendingOrdersFunc = func(ctx context.Context, instID string) ([]okx.Order, error) {
			return []okx.Order{{OrderID: "order1"}}, nil
		}
		mockRest.CancelBatchOrdersFunc = func(ctx context.Context, orders []okx.CancelOrderRequest) error {
			cancelCalled = true
			return nil
		}
		mockRest.GetPositionsFunc = func(ctx context.Context, instID string) ([]okx.Position, error) {
			return []okx.Position{{Position: "1"}}, nil
		}
		mockRest.PlaceOrderFunc = func(ctx context.Context, req *okx.OrderRequest) (string, error) {
			if req.ReduceOnly {
				closeCalled = true
			}
			return "order-id", nil
		}

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{EmergencyStop: true})
		require.NoError(t, err)

		err = manager.EmergencyStop(ctx)
		require.NoError(t, err)

		assert.True(t, cancelCalled, "Should cancel orders")
		assert.True(t, closeCalled, "Should close positions")

		status := manager.GetStatus()
		assert.Equal(t, StateEmergency, status.State)
	})
}

func TestValidateParams(t *testing.T) {
	manager := NewStrategyManager(nil, nil, zap.NewNop())

	tests := []struct {
		name        string
		params      GridParams
		expectError bool
	}{
		{
			name: "valid params",
			params: GridParams{
				InstID:          "ETH-USDT-SWAP",
				GridCount:       10,
				TotalInvestment: decimal.NewFromInt(10000),
				TradeMode:       TradeModeCross,
			},
			expectError: false,
		},
		{
			name: "empty instID",
			params: GridParams{
				InstID:          "",
				GridCount:       10,
				TotalInvestment: decimal.NewFromInt(10000),
				TradeMode:       TradeModeCross,
			},
			expectError: true,
		},
		{
			name: "zero grid count",
			params: GridParams{
				InstID:          "ETH-USDT-SWAP",
				GridCount:       0,
				TotalInvestment: decimal.NewFromInt(10000),
				TradeMode:       TradeModeCross,
			},
			expectError: true,
		},
		{
			name: "zero investment",
			params: GridParams{
				InstID:          "ETH-USDT-SWAP",
				GridCount:       10,
				TotalInvestment: decimal.Zero,
				TradeMode:       TradeModeCross,
			},
			expectError: true,
		},
		{
			name: "invalid trade mode",
			params: GridParams{
				InstID:          "ETH-USDT-SWAP",
				GridCount:       10,
				TotalInvestment: decimal.NewFromInt(10000),
				TradeMode:       TradeMode("invalid"),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.validateParams(tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetStatus_WithoutStrategy(t *testing.T) {
	manager := NewStrategyManager(nil, nil, zap.NewNop())

	status := manager.GetStatus()
	assert.Equal(t, StateIdle, status.State)
}

func TestGetMetrics_WithoutStrategy(t *testing.T) {
	manager := NewStrategyManager(nil, nil, zap.NewNop())

	metrics := manager.GetMetrics()
	assert.True(t, metrics.InitialEquity.IsZero())
}

func TestStrategyManager_Reconnect(t *testing.T) {
	ctx := context.Background()

	t.Run("auto reconnect on disconnect when enabled", func(t *testing.T) {
		reconnectAttempted := false

		manager, _, mockWS := newTestStrategyManager()
		mockWS.ConnectPublicFunc = func(ctx context.Context) error {
			reconnectAttempted = true
			return nil
		}

		// Use custom config with auto reconnect enabled and short delays for testing
		config := DefaultStrategyConfig()
		config.EnableAutoReconnect = true
		config.ReconnectBaseDelay = 10 * time.Millisecond
		config.ReconnectMaxAttempts = 1
		config.ReconnectTimeout = 100 * time.Millisecond

		manager.config = config

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Simulate disconnection
		manager.handleDisconnected("public", nil)

		// Give time for reconnection attempt
		time.Sleep(200 * time.Millisecond)

		assert.True(t, reconnectAttempted, "Should attempt reconnection")

		_ = manager.Stop(ctx)
	})

	t.Run("no reconnect when disabled", func(t *testing.T) {
		reconnectAttempted := false

		manager, _, mockWS := newTestStrategyManager()
		mockWS.ConnectPublicFunc = func(ctx context.Context) error {
			reconnectAttempted = true
			return nil
		}

		// Disable auto reconnect
		config := DefaultStrategyConfig()
		config.EnableAutoReconnect = false
		manager.config = config

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Reset the flag (ConnectPublic was called during Start)
		reconnectAttempted = false

		// Simulate disconnection
		manager.handleDisconnected("public", nil)

		// Give time to verify no reconnection attempt
		time.Sleep(50 * time.Millisecond)

		assert.False(t, reconnectAttempted, "Should not attempt reconnection when disabled")

		_ = manager.Stop(ctx)
	})

	t.Run("no reconnect when strategy stopped", func(t *testing.T) {
		manager, _, _ := newTestStrategyManager()

		// Strategy not started, so handleDisconnected should not trigger reconnect
		manager.handleDisconnected("public", nil)

		// No panic or error expected
	})

	t.Run("calls OnReconnectFailed callback after max attempts", func(t *testing.T) {
		callbackCalled := false
		var callbackChannel string
		var callbackAttempts int

		manager, _, mockWS := newTestStrategyManager()

		// Track connection attempts - first call succeeds (during Start), subsequent fail
		connectCallCount := 0
		mockWS.ConnectPublicFunc = func(ctx context.Context) error {
			connectCallCount++
			if connectCallCount == 1 {
				return nil // First call during Start succeeds
			}
			return errors.New("connection failed")
		}

		config := DefaultStrategyConfig()
		config.EnableAutoReconnect = true
		config.ReconnectBaseDelay = 1 * time.Millisecond
		config.ReconnectMaxAttempts = 2
		config.ReconnectTimeout = 10 * time.Millisecond
		config.OnReconnectFailed = func(channelType string, attempts int) {
			callbackCalled = true
			callbackChannel = channelType
			callbackAttempts = attempts
		}

		manager.config = config

		params := GridParams{
			InstID:          "ETH-USDT-SWAP",
			LowerBound:      decimal.NewFromInt(2000),
			UpperBound:      decimal.NewFromInt(2500),
			GridCount:       5,
			TotalInvestment: decimal.NewFromInt(10000),
			Leverage:        10,
			TradeMode:       TradeModeCross,
		}

		err := manager.Start(ctx, params, RiskParams{})
		require.NoError(t, err)

		// Simulate disconnection
		manager.handleDisconnected("public", nil)

		// Wait for reconnection attempts to complete
		time.Sleep(100 * time.Millisecond)

		assert.True(t, callbackCalled, "OnReconnectFailed callback should be called")
		assert.Equal(t, "public", callbackChannel)
		assert.Equal(t, 2, callbackAttempts)

		_ = manager.Stop(ctx)
	})
}

func TestStrategyConfig_Validate(t *testing.T) {
	t.Run("applies defaults for zero values", func(t *testing.T) {
		config := StrategyConfig{}
		config.Validate()

		assert.Equal(t, 30*time.Second, config.RiskCheckInterval)
		assert.Equal(t, 30*time.Second, config.RebuildTimeout)
		assert.Equal(t, int32(4), config.OrderPrecision)
		assert.Equal(t, "USDT", config.QuoteCurrency)
		assert.Equal(t, 1*time.Second, config.ReconnectBaseDelay)
		assert.Equal(t, 60*time.Second, config.ReconnectMaxDelay)
		assert.Equal(t, 30*time.Second, config.ReconnectTimeout)
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		config := StrategyConfig{
			RiskCheckInterval: 10 * time.Second,
			OrderPrecision:    6,
			QuoteCurrency:     "USDC",
		}
		config.Validate()

		assert.Equal(t, 10*time.Second, config.RiskCheckInterval)
		assert.Equal(t, int32(6), config.OrderPrecision)
		assert.Equal(t, "USDC", config.QuoteCurrency)
	})
}

func TestWithConfig(t *testing.T) {
	customConfig := StrategyConfig{
		RiskCheckInterval: 10 * time.Second,
		OrderPrecision:    6,
		QuoteCurrency:     "USDC",
	}

	manager := NewStrategyManager(nil, nil, zap.NewNop(), WithConfig(customConfig))

	assert.Equal(t, 10*time.Second, manager.config.RiskCheckInterval)
	assert.Equal(t, int32(6), manager.config.OrderPrecision)
	assert.Equal(t, "USDC", manager.config.QuoteCurrency)
}
