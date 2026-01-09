package grid

import (
	"context"
	"time"

	"grid-trading-bot/internal/okx"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// OrderManager handles all order operations for the grid strategy
type OrderManager struct {
	client     ExchangeClient
	calculator *Calculator
	logger     *zap.Logger
}

// NewOrderManager creates a new order manager
func NewOrderManager(client ExchangeClient, calculator *Calculator, logger *zap.Logger) *OrderManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &OrderManager{
		client:     client,
		calculator: calculator,
		logger:     logger,
	}
}

// PlaceInitialOrders places initial grid orders based on current price
// Orders below current price are buy orders, above are sell orders
func (o *OrderManager) PlaceInitialOrders(
	ctx context.Context,
	strategy *Strategy,
	currentPrice decimal.Decimal,
) error {
	if len(strategy.Levels) == 0 {
		return NewStrategyError("PlaceInitialOrders", ErrInvalidParams, map[string]interface{}{
			"reason": "no grid levels defined",
		})
	}

	o.logger.Info("Placing initial grid orders",
		zap.String("instId", strategy.Params.InstID),
		zap.String("currentPrice", currentPrice.String()),
		zap.Int("levelCount", len(strategy.Levels)))

	successCount := 0
	failCount := 0

	for _, level := range strategy.Levels {
		var orderID string
		var side OrderSide
		var err error

		if level.Price.LessThan(currentPrice) {
			// Below current price -> place buy order
			orderID, err = o.placeBuyOrder(ctx, strategy, level)
			side = OrderSideBuy
			if err == nil {
				level.SetBuyOrder(orderID)
			}
		} else if level.Price.GreaterThan(currentPrice) {
			// Above current price -> place sell order
			orderID, err = o.placeSellOrder(ctx, strategy, level)
			side = OrderSideSell
			if err == nil {
				level.SetSellOrder(orderID)
			}
		}
		// Skip level at current price

		if err != nil {
			o.logger.Error("Failed to place initial order",
				zap.Int("gridIndex", level.Index),
				zap.String("side", string(side)),
				zap.String("price", level.Price.String()),
				zap.Error(err))
			failCount++
			continue
		}

		if orderID != "" {
			// Track order
			strategy.AddOrder(&GridOrder{
				OrderID:    orderID,
				InstID:     strategy.Params.InstID,
				Side:       side,
				Price:      level.Price,
				Size:       strategy.OrderSize,
				GridIndex:  level.Index,
				State:      OrderStateLive,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			})
			successCount++
		}
	}

	o.logger.Info("Initial orders placed",
		zap.Int("success", successCount),
		zap.Int("failed", failCount))

	if successCount == 0 && failCount > 0 {
		return NewStrategyError("PlaceInitialOrders", ErrOrderFailed, map[string]interface{}{
			"failCount": failCount,
		})
	}

	// Return partial failure warning if some orders failed
	if failCount > 0 {
		return NewStrategyError("PlaceInitialOrders", ErrOrderPartialFailure, map[string]interface{}{
			"successCount": successCount,
			"failCount":    failCount,
		})
	}

	return nil
}

// PlaceOppositeOrder places an opposite order after one is filled
// Buy filled at level i -> Sell at level i+1
// Sell filled at level i -> Buy at level i-1
func (o *OrderManager) PlaceOppositeOrder(
	ctx context.Context,
	strategy *Strategy,
	filledOrder *GridOrder,
) error {
	oppositeIndex := o.calculator.GetOppositeLevel(
		filledOrder.GridIndex,
		filledOrder.Side,
		len(strategy.Levels),
	)

	if oppositeIndex < 0 || oppositeIndex >= len(strategy.Levels) {
		o.logger.Debug("No opposite level available",
			zap.Int("filledIndex", filledOrder.GridIndex),
			zap.String("side", string(filledOrder.Side)))
		return nil
	}

	level := strategy.Levels[oppositeIndex]

	var orderID string
	var side OrderSide
	var err error

	if filledOrder.Side == OrderSideBuy {
		// Buy filled -> place sell at higher level
		orderID, err = o.placeSellOrder(ctx, strategy, level)
		side = OrderSideSell
		if err == nil {
			level.SetSellOrder(orderID)
		}
	} else {
		// Sell filled -> place buy at lower level
		orderID, err = o.placeBuyOrder(ctx, strategy, level)
		side = OrderSideBuy
		if err == nil {
			level.SetBuyOrder(orderID)
		}
	}

	if err != nil {
		return NewStrategyError("PlaceOppositeOrder", ErrOrderFailed, map[string]interface{}{
			"side":       string(side),
			"gridIndex":  oppositeIndex,
			"price":      level.Price.String(),
			"underlying": err.Error(),
		})
	}

	// Track the new order
	strategy.AddOrder(&GridOrder{
		OrderID:    orderID,
		InstID:     strategy.Params.InstID,
		Side:       side,
		Price:      level.Price,
		Size:       strategy.OrderSize,
		GridIndex:  oppositeIndex,
		State:      OrderStateLive,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	})

	o.logger.Info("Opposite order placed",
		zap.String("orderId", orderID),
		zap.String("side", string(side)),
		zap.Int("gridIndex", oppositeIndex),
		zap.String("price", level.Price.String()))

	return nil
}

// CancelAllOrders cancels all pending orders for the strategy
func (o *OrderManager) CancelAllOrders(ctx context.Context, strategy *Strategy) error {
	instID := strategy.Params.InstID

	// Get all pending orders from exchange
	pending, err := o.client.GetPendingOrders(ctx, instID)
	if err != nil {
		return WrapError("GetPendingOrders", err)
	}

	if len(pending) == 0 {
		o.logger.Info("No pending orders to cancel")
		return nil
	}

	cancelReqs := make([]okx.CancelOrderRequest, 0, len(pending))
	for _, order := range pending {
		cancelReqs = append(cancelReqs, okx.CancelOrderRequest{
			InstID:  instID,
			OrderID: order.OrderID,
		})
	}

	if err := o.client.CancelBatchOrders(ctx, cancelReqs); err != nil {
		return WrapError("CancelBatchOrders", err)
	}

	// Clear local order tracking
	strategy.mu.Lock()
	strategy.Orders = make(map[string]*GridOrder)
	strategy.mu.Unlock()

	// Clear all levels using thread-safe method
	for _, level := range strategy.Levels {
		level.ClearAllOrders()
	}

	o.logger.Info("All orders cancelled",
		zap.String("instId", instID),
		zap.Int("count", len(pending)))

	return nil
}

// CancelOrder cancels a specific order
func (o *OrderManager) CancelOrder(ctx context.Context, strategy *Strategy, orderID string) error {
	instID := strategy.Params.InstID

	if err := o.client.CancelOrder(ctx, instID, orderID); err != nil {
		return WrapError("CancelOrder", err)
	}

	// Update local state
	gridOrder, ok := strategy.GetOrder(orderID)
	if ok {
		strategy.RemoveOrder(orderID)

		// Update level status using thread-safe method
		if gridOrder.GridIndex >= 0 && gridOrder.GridIndex < len(strategy.Levels) {
			level := strategy.Levels[gridOrder.GridIndex]
			if gridOrder.Side == OrderSideBuy {
				level.ClearBuyOrder()
			} else {
				level.ClearSellOrder()
			}
		}
	}

	o.logger.Info("Order cancelled",
		zap.String("orderId", orderID),
		zap.String("instId", instID))

	return nil
}

// SyncOrders synchronizes local order state with exchange
func (o *OrderManager) SyncOrders(ctx context.Context, strategy *Strategy) error {
	instID := strategy.Params.InstID

	pending, err := o.client.GetPendingOrders(ctx, instID)
	if err != nil {
		return WrapError("GetPendingOrders", err)
	}

	// Build map of exchange orders
	exchangeOrders := make(map[string]okx.Order)
	for _, order := range pending {
		exchangeOrders[order.OrderID] = order
	}

	// Update local state
	strategy.mu.Lock()
	defer strategy.mu.Unlock()

	// Remove orders that no longer exist on exchange
	for orderID := range strategy.Orders {
		if _, exists := exchangeOrders[orderID]; !exists {
			delete(strategy.Orders, orderID)
		}
	}

	// Update existing orders
	for orderID, gridOrder := range strategy.Orders {
		if exchOrder, exists := exchangeOrders[orderID]; exists {
			// Convert exchange state string to OrderState
			state := OrderState(exchOrder.State)
			if exchOrder.FilledSize != "" {
				filledSize, _ := decimal.NewFromString(exchOrder.FilledSize)
				gridOrder.UpdateFilled(filledSize, state)
			} else {
				gridOrder.UpdateState(state)
			}
		}
	}

	o.logger.Debug("Orders synchronized",
		zap.String("instId", instID),
		zap.Int("localOrders", len(strategy.Orders)),
		zap.Int("exchangeOrders", len(pending)))

	return nil
}

// placeOrder places a limit order at the specified grid level
func (o *OrderManager) placeOrder(
	ctx context.Context,
	strategy *Strategy,
	level *GridLevel,
	side string,
) (string, error) {
	req := &okx.OrderRequest{
		InstID:    strategy.Params.InstID,
		TradeMode: strategy.Params.TradeMode.String(),
		Side:      side,
		OrderType: "limit",
		Price:     level.Price.String(),
		Size:      strategy.OrderSize.String(),
	}

	orderID, err := o.client.PlaceOrder(ctx, req)
	if err != nil {
		return "", err
	}

	o.logger.Debug("Order placed",
		zap.String("orderId", orderID),
		zap.String("side", side),
		zap.Int("gridIndex", level.Index),
		zap.String("price", level.Price.String()),
		zap.String("size", strategy.OrderSize.String()))

	return orderID, nil
}

// placeBuyOrder places a limit buy order at the specified grid level
func (o *OrderManager) placeBuyOrder(
	ctx context.Context,
	strategy *Strategy,
	level *GridLevel,
) (string, error) {
	return o.placeOrder(ctx, strategy, level, "buy")
}

// placeSellOrder places a limit sell order at the specified grid level
func (o *OrderManager) placeSellOrder(
	ctx context.Context,
	strategy *Strategy,
	level *GridLevel,
) (string, error) {
	return o.placeOrder(ctx, strategy, level, "sell")
}

// HandleOrderFilled processes a filled order and places the opposite order
func (o *OrderManager) HandleOrderFilled(
	ctx context.Context,
	strategy *Strategy,
	orderID string,
	filledPrice decimal.Decimal,
	filledSize decimal.Decimal,
) error {
	gridOrder, ok := strategy.GetOrder(orderID)
	if !ok {
		o.logger.Warn("Filled order not found in strategy",
			zap.String("orderId", orderID))
		return ErrOrderNotFound
	}

	// Update order state using thread-safe method
	gridOrder.UpdateFilled(filledSize, OrderStateFilled)

	// Update level status using thread-safe method
	if gridOrder.GridIndex >= 0 && gridOrder.GridIndex < len(strategy.Levels) {
		level := strategy.Levels[gridOrder.GridIndex]
		if gridOrder.Side == OrderSideBuy {
			level.ClearBuyOrder()
		} else {
			level.ClearSellOrder()
		}
	}

	// Update metrics
	strategy.UpdateMetrics(func(m *StrategyMetrics) {
		m.TotalTrades++
		m.UpdateTime = time.Now()
	})

	o.logger.Info("Order filled",
		zap.String("orderId", orderID),
		zap.String("side", string(gridOrder.Side)),
		zap.Int("gridIndex", gridOrder.GridIndex),
		zap.String("price", filledPrice.String()),
		zap.String("size", filledSize.String()))

	// Remove from active orders
	strategy.RemoveOrder(orderID)

	// Place opposite order
	return o.PlaceOppositeOrder(ctx, strategy, gridOrder)
}

// GetActiveOrderCount returns the number of active orders
func (o *OrderManager) GetActiveOrderCount(strategy *Strategy) int {
	strategy.mu.RLock()
	defer strategy.mu.RUnlock()
	return len(strategy.Orders)
}
