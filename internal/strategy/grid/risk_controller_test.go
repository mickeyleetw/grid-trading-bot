package grid

import (
	"context"
	"testing"

	"grid-trading-bot/internal/okx"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockExchangeClient implements ExchangeClient for testing
type MockExchangeClient struct {
	GetBalanceFunc        func(ctx context.Context, currency string) (*okx.Balance, error)
	GetPositionsFunc      func(ctx context.Context, instID string) ([]okx.Position, error)
	PlaceOrderFunc        func(ctx context.Context, req *okx.OrderRequest) (string, error)
	CancelOrderFunc       func(ctx context.Context, instID, orderID string) error
	CancelBatchOrdersFunc func(ctx context.Context, orders []okx.CancelOrderRequest) error
	GetPendingOrdersFunc  func(ctx context.Context, instID string) ([]okx.Order, error)
	GetTickerFunc         func(ctx context.Context, instID string) (*okx.Ticker, error)
}

func (m *MockExchangeClient) GetBalance(ctx context.Context, currency string) (*okx.Balance, error) {
	if m.GetBalanceFunc != nil {
		return m.GetBalanceFunc(ctx, currency)
	}
	return &okx.Balance{Available: "10000"}, nil
}

func (m *MockExchangeClient) GetPositions(ctx context.Context, instID string) ([]okx.Position, error) {
	if m.GetPositionsFunc != nil {
		return m.GetPositionsFunc(ctx, instID)
	}
	return []okx.Position{}, nil
}

func (m *MockExchangeClient) PlaceOrder(ctx context.Context, req *okx.OrderRequest) (string, error) {
	if m.PlaceOrderFunc != nil {
		return m.PlaceOrderFunc(ctx, req)
	}
	return "test-order-id", nil
}

func (m *MockExchangeClient) CancelOrder(ctx context.Context, instID, orderID string) error {
	if m.CancelOrderFunc != nil {
		return m.CancelOrderFunc(ctx, instID, orderID)
	}
	return nil
}

func (m *MockExchangeClient) CancelBatchOrders(ctx context.Context, orders []okx.CancelOrderRequest) error {
	if m.CancelBatchOrdersFunc != nil {
		return m.CancelBatchOrdersFunc(ctx, orders)
	}
	return nil
}

func (m *MockExchangeClient) GetPendingOrders(ctx context.Context, instID string) ([]okx.Order, error) {
	if m.GetPendingOrdersFunc != nil {
		return m.GetPendingOrdersFunc(ctx, instID)
	}
	return []okx.Order{}, nil
}

func (m *MockExchangeClient) GetTicker(ctx context.Context, instID string) (*okx.Ticker, error) {
	if m.GetTickerFunc != nil {
		return m.GetTickerFunc(ctx, instID)
	}
	return &okx.Ticker{Last: "2000"}, nil
}

func newTestRiskController(stopLossRatio, maxDrawdown float64) *RiskController {
	return NewRiskController(
		RiskParams{
			StopLossRatio: decimal.NewFromFloat(stopLossRatio),
			MaxDrawdown:   decimal.NewFromFloat(maxDrawdown),
			EmergencyStop: true,
		},
		&MockExchangeClient{},
		zap.NewNop(),
	)
}

func TestShouldStopLoss(t *testing.T) {
	tests := []struct {
		name          string
		stopLossRatio float64
		initialEquity float64
		currentEquity float64
		expected      bool
	}{
		{
			name:          "no stop loss - equity above threshold (90%)",
			stopLossRatio: 0.2, // 20% loss threshold -> 80% equity threshold
			initialEquity: 10000,
			currentEquity: 9000, // 90% of initial
			expected:      false,
		},
		{
			name:          "no stop loss - exactly at threshold (80%)",
			stopLossRatio: 0.2,
			initialEquity: 10000,
			currentEquity: 8000, // exactly 80%
			expected:      false,
		},
		{
			name:          "trigger stop loss - below threshold (75%)",
			stopLossRatio: 0.2,
			initialEquity: 10000,
			currentEquity: 7500, // 75% < 80%
			expected:      true,
		},
		{
			name:          "trigger stop loss - significant loss (50%)",
			stopLossRatio: 0.2,
			initialEquity: 10000,
			currentEquity: 5000, // 50% < 80%
			expected:      true,
		},
		{
			name:          "zero initial equity - no stop loss",
			stopLossRatio: 0.2,
			initialEquity: 0,
			currentEquity: 5000,
			expected:      false,
		},
		{
			name:          "high threshold (30% loss)",
			stopLossRatio: 0.3, // 30% loss threshold -> 70% equity
			initialEquity: 10000,
			currentEquity: 7500, // 75% > 70%
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := newTestRiskController(tt.stopLossRatio, 0.5)

			metrics := StrategyMetrics{
				InitialEquity: decimal.NewFromFloat(tt.initialEquity),
				CurrentEquity: decimal.NewFromFloat(tt.currentEquity),
			}

			result := ctrl.ShouldStopLoss(metrics)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckMaxDrawdown(t *testing.T) {
	tests := []struct {
		name            string
		maxDrawdown     float64
		currentDrawdown float64
		expected        bool
	}{
		{
			name:            "within limit",
			maxDrawdown:     0.2,
			currentDrawdown: 0.15,
			expected:        false,
		},
		{
			name:            "at limit",
			maxDrawdown:     0.2,
			currentDrawdown: 0.2,
			expected:        false,
		},
		{
			name:            "exceeded",
			maxDrawdown:     0.2,
			currentDrawdown: 0.25,
			expected:        true,
		},
		{
			name:            "zero max drawdown - no check",
			maxDrawdown:     0,
			currentDrawdown: 0.5,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := newTestRiskController(0.2, tt.maxDrawdown)

			metrics := StrategyMetrics{
				MaxDrawdown: decimal.NewFromFloat(tt.currentDrawdown),
			}

			result := ctrl.CheckMaxDrawdown(metrics)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckRisk(t *testing.T) {
	t.Run("no risk exceeded", func(t *testing.T) {
		ctrl := newTestRiskController(0.2, 0.3)
		metrics := StrategyMetrics{
			InitialEquity: decimal.NewFromFloat(10000),
			CurrentEquity: decimal.NewFromFloat(9000),
			MaxDrawdown:   decimal.NewFromFloat(0.1),
		}

		err := ctrl.CheckRisk(metrics)
		assert.NoError(t, err)
	})

	t.Run("stop loss triggered", func(t *testing.T) {
		ctrl := newTestRiskController(0.2, 0.3)
		metrics := StrategyMetrics{
			InitialEquity: decimal.NewFromFloat(10000),
			CurrentEquity: decimal.NewFromFloat(7000), // 70% < 80%
			MaxDrawdown:   decimal.NewFromFloat(0.1),
		}

		err := ctrl.CheckRisk(metrics)
		assert.ErrorIs(t, err, ErrStopLossTriggered)
	})

	t.Run("max drawdown exceeded", func(t *testing.T) {
		ctrl := newTestRiskController(0.2, 0.3)
		metrics := StrategyMetrics{
			InitialEquity: decimal.NewFromFloat(10000),
			CurrentEquity: decimal.NewFromFloat(9000),
			MaxDrawdown:   decimal.NewFromFloat(0.35), // > 0.3
		}

		err := ctrl.CheckRisk(metrics)
		assert.ErrorIs(t, err, ErrMaxDrawdownExceeded)
	})
}

func TestCalculateEquity(t *testing.T) {
	ctx := context.Background()

	t.Run("calculate with position and P&L", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
				return &okx.Balance{
					Available: "8000",
				}, nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{
					{Position: "1", UnrealizedPnL: "500"},
				}, nil
			},
		}

		ctrl := NewRiskController(
			RiskParams{StopLossRatio: decimal.NewFromFloat(0.2)},
			mockClient,
			zap.NewNop(),
		)

		peakEquity := decimal.NewFromFloat(10000)
		maxDrawdown := decimal.Zero

		update, err := ctrl.CalculateEquity(ctx, "ETH-USDT-SWAP", peakEquity, maxDrawdown)
		require.NoError(t, err)

		// 8000 + 500 = 8500
		assert.True(t, update.CurrentEquity.Equal(decimal.NewFromFloat(8500)))
		assert.True(t, update.UnrealizedPnL.Equal(decimal.NewFromFloat(500)))
	})

	t.Run("calculate drawdown when equity drops", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
				return &okx.Balance{Available: "8000"}, nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{}, nil
			},
		}

		ctrl := NewRiskController(
			RiskParams{StopLossRatio: decimal.NewFromFloat(0.2)},
			mockClient,
			zap.NewNop(),
		)

		peakEquity := decimal.NewFromFloat(10000)
		maxDrawdown := decimal.Zero

		update, err := ctrl.CalculateEquity(ctx, "ETH-USDT-SWAP", peakEquity, maxDrawdown)
		require.NoError(t, err)

		// Drawdown = (10000 - 8000) / 10000 = 0.2
		assert.True(t, update.MaxDrawdown.Equal(decimal.NewFromFloat(0.2)))
	})
}

func TestInitializeEquity(t *testing.T) {
	ctx := context.Background()

	t.Run("with no existing positions", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
				return &okx.Balance{Available: "10000"}, nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{}, nil
			},
		}

		ctrl := NewRiskController(
			RiskParams{},
			mockClient,
			zap.NewNop(),
		)

		metrics := StrategyMetrics{}
		err := ctrl.InitializeEquity(ctx, &metrics, "ETH-USDT-SWAP")

		require.NoError(t, err)
		assert.True(t, metrics.InitialEquity.Equal(decimal.NewFromFloat(10000)))
		assert.True(t, metrics.CurrentEquity.Equal(decimal.NewFromFloat(10000)))
		assert.True(t, metrics.PeakEquity.Equal(decimal.NewFromFloat(10000)))
	})

	t.Run("with existing positions and unrealized PnL", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
				return &okx.Balance{Available: "10000"}, nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{
					{Position: "1", UnrealizedPnL: "500"},
				}, nil
			},
		}

		ctrl := NewRiskController(
			RiskParams{},
			mockClient,
			zap.NewNop(),
		)

		metrics := StrategyMetrics{}
		err := ctrl.InitializeEquity(ctx, &metrics, "ETH-USDT-SWAP")

		require.NoError(t, err)
		// 10000 + 500 = 10500
		assert.True(t, metrics.InitialEquity.Equal(decimal.NewFromFloat(10500)))
		assert.True(t, metrics.CurrentEquity.Equal(decimal.NewFromFloat(10500)))
		assert.True(t, metrics.UnrealizedPnL.Equal(decimal.NewFromFloat(500)))
	})

	t.Run("with empty instID uses balance only", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetBalanceFunc: func(ctx context.Context, currency string) (*okx.Balance, error) {
				return &okx.Balance{Available: "10000"}, nil
			},
		}

		ctrl := NewRiskController(
			RiskParams{},
			mockClient,
			zap.NewNop(),
		)

		metrics := StrategyMetrics{}
		err := ctrl.InitializeEquity(ctx, &metrics, "")

		require.NoError(t, err)
		assert.True(t, metrics.InitialEquity.Equal(decimal.NewFromFloat(10000)))
	})
}

func TestEmergencyClose(t *testing.T) {
	ctx := context.Background()

	t.Run("cancel orders and close positions", func(t *testing.T) {
		cancelCalled := false
		closeCalled := false

		mockClient := &MockExchangeClient{
			GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
				return []okx.Order{
					{OrderID: "order1"},
					{OrderID: "order2"},
				}, nil
			},
			CancelBatchOrdersFunc: func(ctx context.Context, orders []okx.CancelOrderRequest) error {
				cancelCalled = true
				assert.Len(t, orders, 2)
				return nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{
					{Position: "1"},
				}, nil
			},
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				closeCalled = true
				assert.Equal(t, "sell", req.Side) // Long position -> sell
				assert.Equal(t, "market", req.OrderType)
				assert.True(t, req.ReduceOnly)
				return "close-order-id", nil
			},
		}

		ctrl := NewRiskController(RiskParams{}, mockClient, zap.NewNop())
		err := ctrl.EmergencyClose(ctx, "ETH-USDT-SWAP", TradeModeCross)

		require.NoError(t, err)
		assert.True(t, cancelCalled)
		assert.True(t, closeCalled)
	})

	t.Run("close short position", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
				return []okx.Order{}, nil
			},
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{
					{Position: "-1"}, // Short position
				}, nil
			},
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				assert.Equal(t, "buy", req.Side) // Short position -> buy to close
				return "close-order-id", nil
			},
		}

		ctrl := NewRiskController(RiskParams{}, mockClient, zap.NewNop())
		err := ctrl.EmergencyClose(ctx, "ETH-USDT-SWAP", TradeModeCross)

		require.NoError(t, err)
	})
}

func TestHasOpenPosition(t *testing.T) {
	ctx := context.Background()

	t.Run("has position", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{
					{Position: "1.5"},
				}, nil
			},
		}

		ctrl := NewRiskController(RiskParams{}, mockClient, zap.NewNop())
		hasPos, size, err := ctrl.HasOpenPosition(ctx, "ETH-USDT-SWAP")

		require.NoError(t, err)
		assert.True(t, hasPos)
		assert.True(t, size.Equal(decimal.NewFromFloat(1.5)))
	})

	t.Run("no position", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetPositionsFunc: func(ctx context.Context, instID string) ([]okx.Position, error) {
				return []okx.Position{}, nil
			},
		}

		ctrl := NewRiskController(RiskParams{}, mockClient, zap.NewNop())
		hasPos, size, err := ctrl.HasOpenPosition(ctx, "ETH-USDT-SWAP")

		require.NoError(t, err)
		assert.False(t, hasPos)
		assert.True(t, size.IsZero())
	})
}
