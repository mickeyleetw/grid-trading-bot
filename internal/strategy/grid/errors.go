package grid

import (
	"errors"
	"fmt"
)

// Sentinel errors for strategy operations
var (
	ErrStrategyNotRunning     = errors.New("strategy is not running")
	ErrStrategyAlreadyRunning = errors.New("strategy is already running")
	ErrStrategyStopping       = errors.New("strategy is stopping")
	ErrInvalidParams          = errors.New("invalid strategy parameters")
	ErrPriceOutOfRange        = errors.New("price is out of grid range")
	ErrInsufficientBalance    = errors.New("insufficient balance")
	ErrOrderFailed            = errors.New("order placement failed")
	ErrOrderPartialFailure    = errors.New("some orders failed to place")
	ErrOrderNotFound          = errors.New("order not found")
	ErrRiskLimitExceeded      = errors.New("risk limit exceeded")
	ErrStopLossTriggered      = errors.New("stop loss triggered")
	ErrMaxDrawdownExceeded    = errors.New("maximum drawdown exceeded")
	ErrExchangeError          = errors.New("exchange error")
)

// StrategyError wraps an error with operation context
type StrategyError struct {
	Op      string                 // Operation name
	Err     error                  // Underlying error
	Context map[string]interface{} // Additional context
}

// Error implements the error interface
func (e *StrategyError) Error() string {
	if len(e.Context) > 0 {
		return fmt.Sprintf("%s: %v (context: %v)", e.Op, e.Err, e.Context)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error
func (e *StrategyError) Unwrap() error {
	return e.Err
}

// NewStrategyError creates a new strategy error
func NewStrategyError(op string, err error, ctx map[string]interface{}) *StrategyError {
	return &StrategyError{
		Op:      op,
		Err:     err,
		Context: ctx,
	}
}

// WrapError wraps an error with operation context
func WrapError(op string, err error) *StrategyError {
	return &StrategyError{
		Op:  op,
		Err: err,
	}
}

// IsRetryable returns true if the error might succeed on retry
func IsRetryable(err error) bool {
	var stratErr *StrategyError
	if errors.As(err, &stratErr) {
		err = stratErr.Err
	}

	// Exchange errors might be temporary
	if errors.Is(err, ErrExchangeError) {
		return true
	}

	return false
}

// IsFatal returns true if the error requires strategy to stop
func IsFatal(err error) bool {
	return errors.Is(err, ErrStopLossTriggered) ||
		errors.Is(err, ErrMaxDrawdownExceeded) ||
		errors.Is(err, ErrRiskLimitExceeded) ||
		errors.Is(err, ErrInsufficientBalance)
}
