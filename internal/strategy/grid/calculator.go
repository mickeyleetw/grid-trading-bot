package grid

import (
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Calculator constants
const (
	// DefaultOrderPrecision is the decimal places for order size
	DefaultOrderPrecision = 4
	// DefaultRebuildMarginRatio is the multiplier for lower bound when rebuilding
	// near zero prices (0.9 = 10% below current price)
	DefaultRebuildMarginRatio = 0.9
)

// Calculator handles grid level calculations
type Calculator struct {
	logger         *zap.Logger
	orderPrecision int32
}

// NewCalculator creates a new grid calculator with default precision
func NewCalculator(logger *zap.Logger) *Calculator {
	return NewCalculatorWithPrecision(logger, DefaultOrderPrecision)
}

// NewCalculatorWithPrecision creates a new grid calculator with custom precision
func NewCalculatorWithPrecision(logger *zap.Logger, precision int32) *Calculator {
	if logger == nil {
		logger = zap.NewNop()
	}
	if precision <= 0 {
		precision = DefaultOrderPrecision
	}
	return &Calculator{
		logger:         logger,
		orderPrecision: precision,
	}
}

// CalculateGridLevels calculates equal-width grid levels between upper and lower bounds
// Returns gridCount+1 levels (inclusive of both bounds)
func (c *Calculator) CalculateGridLevels(params GridParams) ([]*GridLevel, error) {
	if err := c.validateParams(params); err != nil {
		return nil, err
	}

	gridCount := params.GridCount
	upper := params.UpperBound
	lower := params.LowerBound

	// Calculate grid spacing
	// spacing = (upper - lower) / gridCount
	spacing := upper.Sub(lower).Div(decimal.NewFromInt(int64(gridCount)))

	levels := make([]*GridLevel, gridCount+1)
	for i := 0; i <= gridCount; i++ {
		// price = lower + (spacing * i)
		price := lower.Add(spacing.Mul(decimal.NewFromInt(int64(i))))
		levels[i] = &GridLevel{
			Index:  i,
			Price:  price,
			Status: LevelEmpty,
		}
	}

	c.logger.Info("Grid levels calculated",
		zap.Int("count", len(levels)),
		zap.String("lower", lower.String()),
		zap.String("upper", upper.String()),
		zap.String("spacing", spacing.String()))

	return levels, nil
}

// CalculateOrderSize calculates the order size for each grid level
// Formula: totalInvestment / gridCount / currentPrice / leverage
func (c *Calculator) CalculateOrderSize(
	params GridParams,
	currentPrice decimal.Decimal,
) decimal.Decimal {
	if currentPrice.IsZero() || params.GridCount == 0 {
		return decimal.Zero
	}

	// Investment per grid level
	investmentPerGrid := params.TotalInvestment.Div(decimal.NewFromInt(int64(params.GridCount)))

	// Size = investment / price * leverage
	leverage := decimal.NewFromInt(int64(params.Leverage))
	if leverage.IsZero() {
		leverage = decimal.NewFromInt(1)
	}

	size := investmentPerGrid.Mul(leverage).Div(currentPrice)

	// Round to configured decimal places (contract precision)
	size = size.Round(c.orderPrecision)

	c.logger.Debug("Order size calculated",
		zap.String("investmentPerGrid", investmentPerGrid.String()),
		zap.String("currentPrice", currentPrice.String()),
		zap.String("size", size.String()))

	return size
}

// FindGridIndex finds the grid index for a given price
// Returns -1 if price is below lower bound, gridCount if above upper bound
func (c *Calculator) FindGridIndex(levels []*GridLevel, price decimal.Decimal) int {
	if len(levels) == 0 {
		return -1
	}

	// Below lowest level
	if price.LessThan(levels[0].Price) {
		return -1
	}

	// Above highest level
	if price.GreaterThanOrEqual(levels[len(levels)-1].Price) {
		return len(levels) - 1
	}

	// Find the level just below or at the price
	for i := len(levels) - 1; i >= 0; i-- {
		if price.GreaterThanOrEqual(levels[i].Price) {
			return i
		}
	}

	return -1
}

// FindNearestLevelIndex finds the nearest grid level to a price
func (c *Calculator) FindNearestLevelIndex(levels []*GridLevel, price decimal.Decimal) int {
	if len(levels) == 0 {
		return -1
	}

	nearestIndex := 0
	minDiff := price.Sub(levels[0].Price).Abs()

	for i := 1; i < len(levels); i++ {
		diff := price.Sub(levels[i].Price).Abs()
		if diff.LessThan(minDiff) {
			minDiff = diff
			nearestIndex = i
		}
	}

	return nearestIndex
}

// NeedRebuild checks if the grid needs to be rebuilt
// Returns true if current price is outside the grid bounds
func (c *Calculator) NeedRebuild(levels []*GridLevel, currentPrice decimal.Decimal) bool {
	if len(levels) < 2 {
		return true
	}

	lowerBound := levels[0].Price
	upperBound := levels[len(levels)-1].Price

	return currentPrice.LessThan(lowerBound) || currentPrice.GreaterThan(upperBound)
}

// RebuildGrid creates a new grid centered around the current price
// New bounds are calculated based on the original grid range ratio
func (c *Calculator) RebuildGrid(
	params GridParams,
	currentPrice decimal.Decimal,
) ([]*GridLevel, GridParams, error) {
	if currentPrice.IsZero() {
		return nil, params, NewStrategyError("RebuildGrid", ErrInvalidParams, map[string]interface{}{
			"currentPrice": currentPrice.String(),
		})
	}

	// Calculate original range
	originalRange := params.UpperBound.Sub(params.LowerBound)
	halfRange := originalRange.Div(decimal.NewFromInt(2))

	// New bounds centered on current price
	newParams := params
	newParams.LowerBound = currentPrice.Sub(halfRange)
	newParams.UpperBound = currentPrice.Add(halfRange)

	// Ensure lower bound is positive
	if newParams.LowerBound.LessThanOrEqual(decimal.Zero) {
		newParams.LowerBound = currentPrice.Mul(decimal.NewFromFloat(DefaultRebuildMarginRatio))
		newParams.UpperBound = newParams.LowerBound.Add(originalRange)
	}

	c.logger.Info("Rebuilding grid",
		zap.String("currentPrice", currentPrice.String()),
		zap.String("newLower", newParams.LowerBound.String()),
		zap.String("newUpper", newParams.UpperBound.String()))

	levels, err := c.CalculateGridLevels(newParams)
	if err != nil {
		return nil, params, err
	}

	return levels, newParams, nil
}

// GetGridSpacing returns the price spacing between grid levels
func (c *Calculator) GetGridSpacing(levels []*GridLevel) decimal.Decimal {
	if len(levels) < 2 {
		return decimal.Zero
	}
	return levels[1].Price.Sub(levels[0].Price)
}

// GetOppositeLevel returns the level index for placing an opposite order
// For a filled buy order at index i, the opposite sell should be at i+1
// For a filled sell order at index i, the opposite buy should be at i-1
func (c *Calculator) GetOppositeLevel(filledIndex int, side OrderSide, totalLevels int) int {
	if side == OrderSideBuy {
		// Buy filled -> sell at higher level
		if filledIndex+1 < totalLevels {
			return filledIndex + 1
		}
		return -1 // At upper bound, no opposite
	}
	// Sell filled -> buy at lower level
	if filledIndex-1 >= 0 {
		return filledIndex - 1
	}
	return -1 // At lower bound, no opposite
}

// validateParams validates grid parameters
func (c *Calculator) validateParams(params GridParams) error {
	if params.GridCount <= 0 {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"gridCount": params.GridCount,
			"reason":    "grid count must be positive",
		})
	}

	if params.UpperBound.LessThanOrEqual(params.LowerBound) {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"upperBound": params.UpperBound.String(),
			"lowerBound": params.LowerBound.String(),
			"reason":     "upper bound must be greater than lower bound",
		})
	}

	if params.LowerBound.LessThanOrEqual(decimal.Zero) {
		return NewStrategyError("validateParams", ErrInvalidParams, map[string]interface{}{
			"lowerBound": params.LowerBound.String(),
			"reason":     "lower bound must be positive",
		})
	}

	return nil
}
