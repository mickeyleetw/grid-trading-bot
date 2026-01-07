package grid

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// StrategyState represents the current state of the strategy
type StrategyState string

const (
	StateIdle      StrategyState = "idle"      // Not started
	StateRunning   StrategyState = "running"   // Running normally
	StatePaused    StrategyState = "paused"    // Temporarily paused
	StateStopping  StrategyState = "stopping"  // In process of stopping
	StateStopped   StrategyState = "stopped"   // Stopped normally
	StateEmergency StrategyState = "emergency" // Emergency stop triggered
)

// GridLevelStatus represents the status of a grid level
type GridLevelStatus string

const (
	LevelEmpty       GridLevelStatus = "empty"        // No orders
	LevelBuyPending  GridLevelStatus = "buy_pending"  // Buy order pending
	LevelSellPending GridLevelStatus = "sell_pending" // Sell order pending
	LevelBothPending GridLevelStatus = "both_pending" // Both orders pending
)

// OrderState represents the state of an order
type OrderState string

const (
	OrderStateLive            OrderState = "live"             // Order is active
	OrderStateFilled          OrderState = "filled"           // Order is filled
	OrderStateCanceled        OrderState = "canceled"         // Order is canceled
	OrderStatePartiallyFilled OrderState = "partially_filled" // Order is partially filled
)

// OrderSide represents the side of an order
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"  // Buy order
	OrderSideSell OrderSide = "sell" // Sell order
)

// TradeMode represents the trading mode
type TradeMode string

const (
	TradeModeCross    TradeMode = "cross"    // Cross margin mode
	TradeModeIsolated TradeMode = "isolated" // Isolated margin mode
)

// IsValid checks if the trade mode is valid
func (m TradeMode) IsValid() bool {
	return m == TradeModeCross || m == TradeModeIsolated
}

// String returns the string representation of TradeMode
func (m TradeMode) String() string {
	return string(m)
}

// GridParams holds the grid strategy parameters
type GridParams struct {
	InstID          string          // Trading pair (e.g., "ETH-USDT-SWAP")
	UpperBound      decimal.Decimal // Upper price boundary
	LowerBound      decimal.Decimal // Lower price boundary
	GridCount       int             // Number of grid intervals
	TotalInvestment decimal.Decimal // Total investment amount
	Leverage        int             // Leverage multiplier
	TradeMode       TradeMode       // Trading mode (cross or isolated)
}

// RiskParams holds the risk management parameters
type RiskParams struct {
	StopLossRatio decimal.Decimal // Stop loss ratio (e.g., 0.2 = 20% loss)
	MaxDrawdown   decimal.Decimal // Maximum allowed drawdown
	EmergencyStop bool            // Enable emergency stop feature
}

// ReconnectFailedCallback is called when WebSocket reconnection fails after max attempts
type ReconnectFailedCallback func(channelType string, attempts int)

// StrategyConfig holds configurable strategy settings
type StrategyConfig struct {
	RiskCheckInterval      time.Duration           // Interval for risk checks (default: 30s)
	RebuildTimeout         time.Duration           // Timeout for grid rebuild operations (default: 30s)
	OrderPrecision         int32                   // Decimal places for order size (default: 4)
	QuoteCurrency          string                  // Quote currency for balance queries (default: "USDT")
	EnableAutoReconnect    bool                    // Enable automatic WebSocket reconnection (default: true)
	ReconnectBaseDelay     time.Duration           // Base delay for reconnection attempts (default: 1s)
	ReconnectMaxDelay      time.Duration           // Maximum delay for reconnection attempts (default: 60s)
	ReconnectMaxAttempts   int                     // Maximum reconnection attempts, 0 = unlimited (default: 0)
	ReconnectTimeout       time.Duration           // Timeout for each reconnection attempt (default: 30s)
	OnReconnectFailed      ReconnectFailedCallback // Callback when reconnection fails (optional)
	EmergencyStopOnFailure bool                    // Trigger emergency stop when reconnection fails (default: false)
}

// DefaultStrategyConfig returns the default strategy configuration
func DefaultStrategyConfig() StrategyConfig {
	return StrategyConfig{
		RiskCheckInterval:    30 * time.Second,
		RebuildTimeout:       30 * time.Second,
		OrderPrecision:       4,
		QuoteCurrency:        "USDT",
		EnableAutoReconnect:  true,
		ReconnectBaseDelay:   1 * time.Second,
		ReconnectMaxDelay:    60 * time.Second,
		ReconnectMaxAttempts: 0, // unlimited
		ReconnectTimeout:     30 * time.Second,
	}
}

// Validate validates the configuration and applies defaults for zero values
func (c *StrategyConfig) Validate() {
	if c.RiskCheckInterval <= 0 {
		c.RiskCheckInterval = 30 * time.Second
	}
	if c.RebuildTimeout <= 0 {
		c.RebuildTimeout = 30 * time.Second
	}
	if c.OrderPrecision <= 0 {
		c.OrderPrecision = 4
	}
	if c.QuoteCurrency == "" {
		c.QuoteCurrency = "USDT"
	}
	if c.ReconnectBaseDelay <= 0 {
		c.ReconnectBaseDelay = 1 * time.Second
	}
	if c.ReconnectMaxDelay <= 0 {
		c.ReconnectMaxDelay = 60 * time.Second
	}
	if c.ReconnectTimeout <= 0 {
		c.ReconnectTimeout = 30 * time.Second
	}
}

// GridLevel represents a single grid line
type GridLevel struct {
	Index       int             // Grid index (0 = lowest)
	Price       decimal.Decimal // Price at this level
	BuyOrderID  string          // Buy order ID (if any)
	SellOrderID string          // Sell order ID (if any)
	Status      GridLevelStatus // Current status
	mu          sync.RWMutex    // Protects order IDs and status
}

// SetBuyOrder sets the buy order ID and updates status (thread-safe)
func (l *GridLevel) SetBuyOrder(orderID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.BuyOrderID = orderID
	l.updateStatusLocked()
}

// SetSellOrder sets the sell order ID and updates status (thread-safe)
func (l *GridLevel) SetSellOrder(orderID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.SellOrderID = orderID
	l.updateStatusLocked()
}

// ClearBuyOrder clears the buy order ID and updates status (thread-safe)
func (l *GridLevel) ClearBuyOrder() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.BuyOrderID = ""
	l.updateStatusLocked()
}

// ClearSellOrder clears the sell order ID and updates status (thread-safe)
func (l *GridLevel) ClearSellOrder() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.SellOrderID = ""
	l.updateStatusLocked()
}

// ClearAllOrders clears both order IDs and resets status (thread-safe)
func (l *GridLevel) ClearAllOrders() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.BuyOrderID = ""
	l.SellOrderID = ""
	l.Status = LevelEmpty
}

// GetOrderIDs returns the current order IDs (thread-safe)
func (l *GridLevel) GetOrderIDs() (buyID, sellID string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.BuyOrderID, l.SellOrderID
}

// GetStatus returns the current status (thread-safe)
func (l *GridLevel) GetStatus() GridLevelStatus {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Status
}

// updateStatusLocked updates status based on current order IDs (must hold lock)
func (l *GridLevel) updateStatusLocked() {
	hasBuy := l.BuyOrderID != ""
	hasSell := l.SellOrderID != ""

	switch {
	case hasBuy && hasSell:
		l.Status = LevelBothPending
	case hasBuy:
		l.Status = LevelBuyPending
	case hasSell:
		l.Status = LevelSellPending
	default:
		l.Status = LevelEmpty
	}
}

// GridOrder represents a grid order
type GridOrder struct {
	OrderID    string          // Order ID from exchange
	InstID     string          // Trading pair
	Side       OrderSide       // Order side (buy or sell)
	Price      decimal.Decimal // Order price
	Size       decimal.Decimal // Order size
	GridIndex  int             // Corresponding grid index
	State      OrderState      // Order state
	FilledSize decimal.Decimal // Filled size
	CreateTime time.Time       // Creation time
	UpdateTime time.Time       // Last update time
	mu         sync.RWMutex    // Protects mutable fields
}

// UpdateState updates the order state (thread-safe)
func (o *GridOrder) UpdateState(state OrderState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.State = state
	o.UpdateTime = time.Now()
}

// UpdateFilled updates the filled size and state (thread-safe)
func (o *GridOrder) UpdateFilled(filledSize decimal.Decimal, state OrderState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.FilledSize = filledSize
	o.State = state
	o.UpdateTime = time.Now()
}

// GetState returns the current state (thread-safe)
func (o *GridOrder) GetState() OrderState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.State
}

// StrategyMetrics holds the strategy performance metrics
type StrategyMetrics struct {
	InitialEquity decimal.Decimal // Initial equity
	CurrentEquity decimal.Decimal // Current equity
	UnrealizedPnL decimal.Decimal // Unrealized P&L
	RealizedPnL   decimal.Decimal // Realized P&L
	TotalTrades   int             // Total number of trades
	WinTrades     int             // Number of winning trades
	MaxDrawdown   decimal.Decimal // Maximum drawdown experienced
	PeakEquity    decimal.Decimal // Peak equity for drawdown calculation
	LastPrice     decimal.Decimal // Last market price
	UpdateTime    time.Time       // Last update time
}

// Strategy represents the grid trading strategy state
type Strategy struct {
	ID         string                // Strategy ID
	Params     GridParams            // Grid parameters
	RiskParams RiskParams            // Risk parameters
	State      StrategyState         // Current state
	Levels     []*GridLevel          // Grid levels
	Orders     map[string]*GridOrder // Active orders (orderID -> GridOrder)
	Metrics    StrategyMetrics       // Performance metrics
	OrderSize  decimal.Decimal       // Size per order

	mu        sync.RWMutex // Protects state
	startTime time.Time    // Strategy start time
	stopTime  time.Time    // Strategy stop time
}

// NewStrategy creates a new strategy instance
func NewStrategy(id string, params GridParams, riskParams RiskParams) *Strategy {
	return &Strategy{
		ID:         id,
		Params:     params,
		RiskParams: riskParams,
		State:      StateIdle,
		Levels:     make([]*GridLevel, 0),
		Orders:     make(map[string]*GridOrder),
		Metrics:    StrategyMetrics{},
	}
}

// GetState returns the current strategy state
func (s *Strategy) GetState() StrategyState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// SetState sets the strategy state
func (s *Strategy) SetState(state StrategyState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
}

// IsRunning returns true if strategy is running
func (s *Strategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State == StateRunning
}

// AddOrder adds an order to the strategy
func (s *Strategy) AddOrder(order *GridOrder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Orders[order.OrderID] = order
}

// RemoveOrder removes an order from the strategy
func (s *Strategy) RemoveOrder(orderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Orders, orderID)
}

// GetOrder returns an order by ID
func (s *Strategy) GetOrder(orderID string) (*GridOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order, ok := s.Orders[orderID]
	return order, ok
}

// GetMetrics returns a copy of the current metrics
func (s *Strategy) GetMetrics() StrategyMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Metrics
}

// UpdateMetrics updates the strategy metrics
func (s *Strategy) UpdateMetrics(fn func(*StrategyMetrics)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.Metrics)
}

// GetStartTime returns the strategy start time
func (s *Strategy) GetStartTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startTime
}

// GetStopTime returns the strategy stop time
func (s *Strategy) GetStopTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopTime
}

// SetStartTime sets the strategy start time
func (s *Strategy) SetStartTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTime = t
}

// SetStopTime sets the strategy stop time
func (s *Strategy) SetStopTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopTime = t
}
