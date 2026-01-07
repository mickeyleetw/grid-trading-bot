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

func newTestStrategy() *Strategy {
	params := GridParams{
		InstID:          "ETH-USDT-SWAP",
		LowerBound:      decimal.NewFromInt(2000),
		UpperBound:      decimal.NewFromInt(2500),
		GridCount:       5,
		TotalInvestment: decimal.NewFromInt(10000),
		Leverage:        10,
		TradeMode:       "cross",
	}

	strategy := NewStrategy("test-strategy", params, RiskParams{})

	// Create grid levels: 2000, 2100, 2200, 2300, 2400, 2500
	strategy.Levels = []*GridLevel{
		{Index: 0, Price: decimal.NewFromInt(2000), Status: LevelEmpty},
		{Index: 1, Price: decimal.NewFromInt(2100), Status: LevelEmpty},
		{Index: 2, Price: decimal.NewFromInt(2200), Status: LevelEmpty},
		{Index: 3, Price: decimal.NewFromInt(2300), Status: LevelEmpty},
		{Index: 4, Price: decimal.NewFromInt(2400), Status: LevelEmpty},
		{Index: 5, Price: decimal.NewFromInt(2500), Status: LevelEmpty},
	}

	strategy.OrderSize = decimal.NewFromFloat(0.5)

	return strategy
}

func newTestOrderManager(client ExchangeClient) *OrderManager {
	calc := NewCalculator(zap.NewNop())
	return NewOrderManager(client, calc, zap.NewNop())
}

func TestPlaceInitialOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("place orders above and below current price", func(t *testing.T) {
		buyOrders := 0
		sellOrders := 0

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				if req.Side == "buy" {
					buyOrders++
					return "buy-order-" + req.Price, nil
				}
				sellOrders++
				return "sell-order-" + req.Price, nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Current price at 2250 (between level 2 and 3)
		currentPrice := decimal.NewFromInt(2250)

		err := orderMgr.PlaceInitialOrders(ctx, strategy, currentPrice)
		require.NoError(t, err)

		// Levels 0,1,2 (2000, 2100, 2200) are below -> buy orders
		// Levels 3,4,5 (2300, 2400, 2500) are above -> sell orders
		assert.Equal(t, 3, buyOrders)
		assert.Equal(t, 3, sellOrders)

		// Check order tracking
		assert.Equal(t, 6, len(strategy.Orders))
	})

	t.Run("place only buy orders when price at upper bound", func(t *testing.T) {
		buyOrders := 0
		sellOrders := 0

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				if req.Side == "buy" {
					buyOrders++
				} else {
					sellOrders++
				}
				return "order-id", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Current price at 2600 (above all levels)
		currentPrice := decimal.NewFromInt(2600)

		err := orderMgr.PlaceInitialOrders(ctx, strategy, currentPrice)
		require.NoError(t, err)

		assert.Equal(t, 6, buyOrders) // All levels below current price
		assert.Equal(t, 0, sellOrders)
	})

	t.Run("empty levels returns error", func(t *testing.T) {
		mockClient := &MockExchangeClient{}
		orderMgr := newTestOrderManager(mockClient)

		strategy := NewStrategy("test", GridParams{}, RiskParams{})
		strategy.Levels = []*GridLevel{} // Empty

		err := orderMgr.PlaceInitialOrders(ctx, strategy, decimal.NewFromInt(100))
		assert.Error(t, err)
	})
}

func TestPlaceOppositeOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("buy filled -> sell at higher level", func(t *testing.T) {
		var placedOrder *okx.OrderRequest

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				placedOrder = req
				return "opposite-order-id", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Simulate filled buy order at level 2
		filledOrder := &GridOrder{
			OrderID:   "filled-buy",
			Side:      "buy",
			GridIndex: 2,
			Price:     decimal.NewFromInt(2200),
		}

		err := orderMgr.PlaceOppositeOrder(ctx, strategy, filledOrder)
		require.NoError(t, err)

		// Should place sell at level 3
		assert.NotNil(t, placedOrder)
		assert.Equal(t, "sell", placedOrder.Side)
		assert.Equal(t, "2300", placedOrder.Price) // Level 3 price
	})

	t.Run("sell filled -> buy at lower level", func(t *testing.T) {
		var placedOrder *okx.OrderRequest

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				placedOrder = req
				return "opposite-order-id", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Simulate filled sell order at level 3
		filledOrder := &GridOrder{
			OrderID:   "filled-sell",
			Side:      "sell",
			GridIndex: 3,
			Price:     decimal.NewFromInt(2300),
		}

		err := orderMgr.PlaceOppositeOrder(ctx, strategy, filledOrder)
		require.NoError(t, err)

		// Should place buy at level 2
		assert.NotNil(t, placedOrder)
		assert.Equal(t, "buy", placedOrder.Side)
		assert.Equal(t, "2200", placedOrder.Price) // Level 2 price
	})

	t.Run("buy at top level - no opposite", func(t *testing.T) {
		orderPlaced := false

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				orderPlaced = true
				return "order-id", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Buy filled at top level
		filledOrder := &GridOrder{
			OrderID:   "filled-buy",
			Side:      "buy",
			GridIndex: 5, // Top level
		}

		err := orderMgr.PlaceOppositeOrder(ctx, strategy, filledOrder)
		require.NoError(t, err)

		// No order should be placed (no level above)
		assert.False(t, orderPlaced)
	})

	t.Run("sell at bottom level - no opposite", func(t *testing.T) {
		orderPlaced := false

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				orderPlaced = true
				return "order-id", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Sell filled at bottom level
		filledOrder := &GridOrder{
			OrderID:   "filled-sell",
			Side:      "sell",
			GridIndex: 0, // Bottom level
		}

		err := orderMgr.PlaceOppositeOrder(ctx, strategy, filledOrder)
		require.NoError(t, err)

		// No order should be placed (no level below)
		assert.False(t, orderPlaced)
	})
}

func TestCancelAllOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("cancel multiple orders", func(t *testing.T) {
		cancelCalled := false
		cancelledOrders := 0

		mockClient := &MockExchangeClient{
			GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
				return []okx.Order{
					{OrderID: "order1"},
					{OrderID: "order2"},
					{OrderID: "order3"},
				}, nil
			},
			CancelBatchOrdersFunc: func(ctx context.Context, orders []okx.CancelOrderRequest) error {
				cancelCalled = true
				cancelledOrders = len(orders)
				return nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Add some tracked orders
		strategy.AddOrder(&GridOrder{OrderID: "order1", GridIndex: 0, Side: "buy"})
		strategy.AddOrder(&GridOrder{OrderID: "order2", GridIndex: 1, Side: "buy"})

		err := orderMgr.CancelAllOrders(ctx, strategy)
		require.NoError(t, err)

		assert.True(t, cancelCalled)
		assert.Equal(t, 3, cancelledOrders)
		assert.Empty(t, strategy.Orders)
	})

	t.Run("no orders to cancel", func(t *testing.T) {
		cancelCalled := false

		mockClient := &MockExchangeClient{
			GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
				return []okx.Order{}, nil
			},
			CancelBatchOrdersFunc: func(ctx context.Context, orders []okx.CancelOrderRequest) error {
				cancelCalled = true
				return nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		err := orderMgr.CancelAllOrders(ctx, strategy)
		require.NoError(t, err)

		assert.False(t, cancelCalled)
	})
}

func TestHandleOrderFilled(t *testing.T) {
	ctx := context.Background()

	t.Run("handle buy order filled", func(t *testing.T) {
		oppositeOrderPlaced := false

		mockClient := &MockExchangeClient{
			PlaceOrderFunc: func(ctx context.Context, req *okx.OrderRequest) (string, error) {
				oppositeOrderPlaced = true
				assert.Equal(t, "sell", req.Side) // Opposite of buy
				return "opposite-order", nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Add a buy order
		strategy.AddOrder(&GridOrder{
			OrderID:   "buy-order-1",
			Side:      "buy",
			GridIndex: 2,
			Price:     decimal.NewFromInt(2200),
		})

		err := orderMgr.HandleOrderFilled(ctx, strategy, "buy-order-1",
			decimal.NewFromInt(2200), decimal.NewFromFloat(0.5))

		require.NoError(t, err)
		assert.True(t, oppositeOrderPlaced)

		// Original order should be removed
		_, exists := strategy.GetOrder("buy-order-1")
		assert.False(t, exists)

		// Metrics should be updated
		metrics := strategy.GetMetrics()
		assert.Equal(t, 1, metrics.TotalTrades)
	})

	t.Run("handle unknown order", func(t *testing.T) {
		mockClient := &MockExchangeClient{}
		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		err := orderMgr.HandleOrderFilled(ctx, strategy, "unknown-order",
			decimal.NewFromInt(2200), decimal.NewFromFloat(0.5))

		assert.ErrorIs(t, err, ErrOrderNotFound)
	})
}

func TestSyncOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("sync removes stale orders", func(t *testing.T) {
		mockClient := &MockExchangeClient{
			GetPendingOrdersFunc: func(ctx context.Context, instID string) ([]okx.Order, error) {
				// Only order1 exists on exchange
				return []okx.Order{
					{OrderID: "order1", State: "live"},
				}, nil
			},
		}

		orderMgr := newTestOrderManager(mockClient)
		strategy := newTestStrategy()

		// Add two orders locally
		strategy.AddOrder(&GridOrder{OrderID: "order1", GridIndex: 0})
		strategy.AddOrder(&GridOrder{OrderID: "order2", GridIndex: 1}) // This one is stale

		err := orderMgr.SyncOrders(ctx, strategy)
		require.NoError(t, err)

		// Only order1 should remain
		assert.Equal(t, 1, len(strategy.Orders))
		_, exists := strategy.GetOrder("order1")
		assert.True(t, exists)
		_, exists = strategy.GetOrder("order2")
		assert.False(t, exists)
	})
}

func TestGetActiveOrderCount(t *testing.T) {
	mockClient := &MockExchangeClient{}
	orderMgr := newTestOrderManager(mockClient)
	strategy := newTestStrategy()

	assert.Equal(t, 0, orderMgr.GetActiveOrderCount(strategy))

	strategy.AddOrder(&GridOrder{OrderID: "order1"})
	strategy.AddOrder(&GridOrder{OrderID: "order2"})

	assert.Equal(t, 2, orderMgr.GetActiveOrderCount(strategy))
}
