package grid

import (
	"context"
	"time"

	"grid-trading-bot/internal/okx"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Default constants for risk management
const (
	DefaultQuoteCurrency = "USDT" // Default quote currency for balance queries
)

// ExchangeClient defines the interface for exchange operations
// This allows easy mocking for tests
type ExchangeClient interface {
	GetBalance(ctx context.Context, currency string) (*okx.Balance, error)
	GetPositions(ctx context.Context, instID string) ([]okx.Position, error)
	PlaceOrder(ctx context.Context, req *okx.OrderRequest) (string, error)
	CancelOrder(ctx context.Context, instID, orderID string) error
	CancelBatchOrders(ctx context.Context, orders []okx.CancelOrderRequest) error
	GetPendingOrders(ctx context.Context, instID string) ([]okx.Order, error)
	GetTicker(ctx context.Context, instID string) (*okx.Ticker, error)
}

// RiskController handles all risk management operations
type RiskController struct {
	params        RiskParams
	client        ExchangeClient
	logger        *zap.Logger
	quoteCurrency string
}

// NewRiskController creates a new risk controller with default quote currency
func NewRiskController(params RiskParams, client ExchangeClient, logger *zap.Logger) *RiskController {
	return NewRiskControllerWithCurrency(params, client, logger, DefaultQuoteCurrency)
}

// NewRiskControllerWithCurrency creates a new risk controller with custom quote currency
func NewRiskControllerWithCurrency(params RiskParams, client ExchangeClient, logger *zap.Logger, quoteCurrency string) *RiskController {
	if logger == nil {
		logger = zap.NewNop()
	}
	if quoteCurrency == "" {
		quoteCurrency = DefaultQuoteCurrency
	}
	return &RiskController{
		params:        params,
		client:        client,
		logger:        logger,
		quoteCurrency: quoteCurrency,
	}
}

// ShouldStopLoss checks if stop loss should be triggered
// Returns true if currentEquity / initialEquity < (1 - stopLossRatio)
// Example: if stopLossRatio = 0.2, stop when equity drops below 80%
// Safety: Returns false if initialEquity is zero to prevent divide-by-zero
func (r *RiskController) ShouldStopLoss(metrics StrategyMetrics) bool {
	// Safety: Prevent divide-by-zero when initialEquity hasn't been set
	if metrics.InitialEquity.IsZero() {
		return false
	}

	ratio := metrics.CurrentEquity.Div(metrics.InitialEquity)
	threshold := decimal.NewFromInt(1).Sub(r.params.StopLossRatio)

	shouldStop := ratio.LessThan(threshold)

	if shouldStop {
		r.logger.Warn("Stop loss condition detected",
			zap.String("currentEquity", metrics.CurrentEquity.String()),
			zap.String("initialEquity", metrics.InitialEquity.String()),
			zap.String("ratio", ratio.String()),
			zap.String("threshold", threshold.String()))
	}

	return shouldStop
}

// CheckMaxDrawdown checks if maximum drawdown has been exceeded
func (r *RiskController) CheckMaxDrawdown(metrics StrategyMetrics) bool {
	if r.params.MaxDrawdown.IsZero() {
		return false
	}

	exceeded := metrics.MaxDrawdown.GreaterThan(r.params.MaxDrawdown)

	if exceeded {
		r.logger.Warn("Max drawdown exceeded",
			zap.String("currentDrawdown", metrics.MaxDrawdown.String()),
			zap.String("maxAllowed", r.params.MaxDrawdown.String()))
	}

	return exceeded
}

// CheckRisk performs all risk checks and returns an error if any risk limit is exceeded
func (r *RiskController) CheckRisk(metrics StrategyMetrics) error {
	if r.ShouldStopLoss(metrics) {
		return ErrStopLossTriggered
	}

	if r.CheckMaxDrawdown(metrics) {
		return ErrMaxDrawdownExceeded
	}

	return nil
}

// EquityUpdate holds the result of an equity calculation
type EquityUpdate struct {
	CurrentEquity decimal.Decimal
	UnrealizedPnL decimal.Decimal
	PeakEquity    decimal.Decimal
	MaxDrawdown   decimal.Decimal
}

// CalculateEquity calculates the current equity without modifying any state
// Returns an EquityUpdate that can be applied to metrics via Strategy.UpdateMetrics
func (r *RiskController) CalculateEquity(ctx context.Context, instID string, currentPeak, currentMaxDrawdown decimal.Decimal) (*EquityUpdate, error) {
	// Get current balance
	balance, err := r.client.GetBalance(ctx, r.quoteCurrency)
	if err != nil {
		return nil, WrapError("GetBalance", err)
	}

	availableBalance, err := decimal.NewFromString(balance.Available)
	if err != nil {
		return nil, WrapError("ParseBalance", err)
	}

	// Get current positions and unrealized P&L
	positions, err := r.client.GetPositions(ctx, instID)
	if err != nil {
		return nil, WrapError("GetPositions", err)
	}

	unrealizedPnL := decimal.Zero
	for _, pos := range positions {
		pnl, err := decimal.NewFromString(pos.UnrealizedPnL)
		if err != nil {
			continue
		}
		unrealizedPnL = unrealizedPnL.Add(pnl)
	}

	// Current equity = available balance + unrealized P&L
	currentEquity := availableBalance.Add(unrealizedPnL)

	// Calculate peak equity
	peakEquity := currentPeak
	if currentEquity.GreaterThan(peakEquity) {
		peakEquity = currentEquity
	}

	// Calculate drawdown
	// Safety: Only calculate drawdown when peakEquity > 0 to prevent divide-by-zero.
	// This check is necessary because peakEquity could be zero if no trades have occurred yet.
	maxDrawdown := currentMaxDrawdown
	if peakEquity.GreaterThan(decimal.Zero) {
		drawdown := peakEquity.Sub(currentEquity).Div(peakEquity)
		if drawdown.GreaterThan(maxDrawdown) {
			maxDrawdown = drawdown
		}
	}

	r.logger.Debug("Equity calculated",
		zap.String("currentEquity", currentEquity.String()),
		zap.String("unrealizedPnL", unrealizedPnL.String()),
		zap.String("maxDrawdown", maxDrawdown.String()))

	return &EquityUpdate{
		CurrentEquity: currentEquity,
		UnrealizedPnL: unrealizedPnL,
		PeakEquity:    peakEquity,
		MaxDrawdown:   maxDrawdown,
	}, nil
}

// InitializeEquity sets the initial equity for the strategy
// It considers both available balance and unrealized P&L from existing positions
func (r *RiskController) InitializeEquity(ctx context.Context, metrics *StrategyMetrics, instID string) error {
	balance, err := r.client.GetBalance(ctx, r.quoteCurrency)
	if err != nil {
		return WrapError("GetBalance", err)
	}

	availableBalance, err := decimal.NewFromString(balance.Available)
	if err != nil {
		return WrapError("ParseBalance", err)
	}

	// Also consider unrealized P&L from existing positions
	unrealizedPnL := decimal.Zero
	if instID != "" {
		positions, err := r.client.GetPositions(ctx, instID)
		if err != nil {
			r.logger.Warn("Failed to get positions during equity init, using balance only",
				zap.Error(err))
		} else {
			for _, pos := range positions {
				pnl, err := decimal.NewFromString(pos.UnrealizedPnL)
				if err != nil {
					continue
				}
				unrealizedPnL = unrealizedPnL.Add(pnl)
			}
		}
	}

	initialEquity := availableBalance.Add(unrealizedPnL)

	metrics.InitialEquity = initialEquity
	metrics.CurrentEquity = initialEquity
	metrics.PeakEquity = initialEquity
	metrics.UnrealizedPnL = unrealizedPnL
	metrics.UpdateTime = time.Now()

	r.logger.Info("Initial equity set",
		zap.String("availableBalance", availableBalance.String()),
		zap.String("unrealizedPnL", unrealizedPnL.String()),
		zap.String("initialEquity", initialEquity.String()))

	return nil
}

// EmergencyClose cancels all orders and closes all positions
func (r *RiskController) EmergencyClose(ctx context.Context, instID string, tradeMode TradeMode) error {
	r.logger.Warn("Executing emergency close", zap.String("instId", instID))

	// Step 1: Cancel all pending orders
	if err := r.cancelAllOrders(ctx, instID); err != nil {
		r.logger.Error("Failed to cancel orders during emergency close", zap.Error(err))
		// Continue with position closure even if order cancellation fails
	}

	// Step 2: Close all positions
	if err := r.closeAllPositions(ctx, instID, tradeMode); err != nil {
		return WrapError("EmergencyClose", err)
	}

	r.logger.Info("Emergency close completed", zap.String("instId", instID))
	return nil
}

// cancelAllOrders cancels all pending orders for the instrument
func (r *RiskController) cancelAllOrders(ctx context.Context, instID string) error {
	pending, err := r.client.GetPendingOrders(ctx, instID)
	if err != nil {
		return err
	}

	if len(pending) == 0 {
		return nil
	}

	cancelReqs := make([]okx.CancelOrderRequest, len(pending))
	for i, order := range pending {
		cancelReqs[i] = okx.CancelOrderRequest{
			InstID:  instID,
			OrderID: order.OrderID,
		}
	}

	if err := r.client.CancelBatchOrders(ctx, cancelReqs); err != nil {
		return err
	}

	r.logger.Info("Cancelled all pending orders",
		zap.String("instId", instID),
		zap.Int("count", len(pending)))

	return nil
}

// closeAllPositions closes all open positions with market orders
func (r *RiskController) closeAllPositions(ctx context.Context, instID string, tradeMode TradeMode) error {
	positions, err := r.client.GetPositions(ctx, instID)
	if err != nil {
		return err
	}

	// Use provided tradeMode or default to cross
	effectiveTradeMode := tradeMode
	if effectiveTradeMode == "" {
		effectiveTradeMode = TradeModeCross
	}

	for _, pos := range positions {
		posSize, err := decimal.NewFromString(pos.Position)
		if err != nil {
			continue
		}

		if posSize.IsZero() {
			continue
		}

		// Determine side for closing
		side := "sell"
		if posSize.IsNegative() {
			side = "buy"
			posSize = posSize.Abs()
		}

		orderReq := &okx.OrderRequest{
			InstID:     instID,
			TradeMode:  effectiveTradeMode.String(),
			Side:       side,
			OrderType:  "market",
			Size:       posSize.String(),
			ReduceOnly: true,
		}

		orderID, err := r.client.PlaceOrder(ctx, orderReq)
		if err != nil {
			r.logger.Error("Failed to close position",
				zap.String("instId", instID),
				zap.String("side", side),
				zap.String("size", posSize.String()),
				zap.Error(err))
			return err
		}

		r.logger.Info("Position close order placed",
			zap.String("orderId", orderID),
			zap.String("instId", instID),
			zap.String("side", side),
			zap.String("size", posSize.String()))
	}

	return nil
}

// HasOpenPosition checks if there are any open positions
func (r *RiskController) HasOpenPosition(ctx context.Context, instID string) (bool, decimal.Decimal, error) {
	positions, err := r.client.GetPositions(ctx, instID)
	if err != nil {
		return false, decimal.Zero, err
	}

	totalPosition := decimal.Zero
	for _, pos := range positions {
		posSize, err := decimal.NewFromString(pos.Position)
		if err != nil {
			continue
		}
		totalPosition = totalPosition.Add(posSize)
	}

	hasPosition := !totalPosition.IsZero()
	return hasPosition, totalPosition, nil
}

// GetParams returns the risk parameters
func (r *RiskController) GetParams() RiskParams {
	return r.params
}

// UpdateParams updates the risk parameters
func (r *RiskController) UpdateParams(params RiskParams) {
	r.params = params
}
