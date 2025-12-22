package okx

// WSOperation represents WebSocket operation types
type WSOperation string

const (
	WSOpLogin       WSOperation = "login"
	WSOpSubscribe   WSOperation = "subscribe"
	WSOpUnsubscribe WSOperation = "unsubscribe"
)

// WSEvent represents WebSocket event types
type WSEvent string

const (
	WSEventLogin       WSEvent = "login"
	WSEventSubscribe   WSEvent = "subscribe"
	WSEventUnsubscribe WSEvent = "unsubscribe"
	WSEventError       WSEvent = "error"
)

// WSRequest represents a generic WebSocket request
type WSRequest struct {
	Op   WSOperation `json:"op"`
	Args []any       `json:"args"`
}

// WSLoginArg represents login arguments for private channel authentication
type WSLoginArg struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

// WSSubscribeArg represents subscription arguments
type WSSubscribeArg struct {
	Channel  string `json:"channel"`
	InstID   string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// WSResponse represents a generic WebSocket response
type WSResponse struct {
	Event  WSEvent `json:"event,omitempty"`
	Code   string  `json:"code,omitempty"`
	Msg    string  `json:"msg,omitempty"`
	ConnID string  `json:"connId,omitempty"`
}

// WSLoginResponse represents login response
type WSLoginResponse struct {
	Event  WSEvent `json:"event"`
	Code   string  `json:"code"`
	Msg    string  `json:"msg"`
	ConnID string  `json:"connId,omitempty"`
}

// WSSubscribeResponse represents subscription response
type WSSubscribeResponse struct {
	Event  WSEvent         `json:"event"`
	Arg    *WSSubscribeArg `json:"arg,omitempty"`
	Code   string          `json:"code,omitempty"`
	Msg    string          `json:"msg,omitempty"`
	ConnID string          `json:"connId,omitempty"`
}

// WSTicker represents real-time ticker data from WebSocket
type WSTicker struct {
	InstID       string `json:"instId"`
	Last         string `json:"last"`
	LastSize     string `json:"lastSz"`
	AskPrice     string `json:"askPx"`
	AskSize      string `json:"askSz"`
	BidPrice     string `json:"bidPx"`
	BidSize      string `json:"bidSz"`
	Open24h      string `json:"open24h"`
	High24h      string `json:"high24h"`
	Low24h       string `json:"low24h"`
	Volume24h    string `json:"volCcy24h"`
	Volume24hCcy string `json:"vol24h"`
	Timestamp    string `json:"ts"`
}

// WSTickerMessage represents ticker push message
type WSTickerMessage struct {
	Arg  WSSubscribeArg `json:"arg"`
	Data []WSTicker     `json:"data"`
}

// WSOrder represents order data from WebSocket
type WSOrder struct {
	InstID         string `json:"instId"`
	OrderID        string `json:"ordId"`
	ClientOrdID    string `json:"clOrdId"`
	Tag            string `json:"tag"`
	Price          string `json:"px"`
	Size           string `json:"sz"`
	OrderType      string `json:"ordType"`
	Side           string `json:"side"`
	PositionSide   string `json:"posSide"`
	TradeMode      string `json:"tdMode"`
	AccFillSize    string `json:"accFillSz"`
	FillPrice      string `json:"fillPx"`
	TradeID        string `json:"tradeId"`
	FillSize       string `json:"fillSz"`
	FillTime       string `json:"fillTime"`
	State          string `json:"state"`
	AveragePrice   string `json:"avgPx"`
	Leverage       string `json:"lever"`
	FeeCurrency    string `json:"feeCcy"`
	Fee            string `json:"fee"`
	RebateCurrency string `json:"rebateCcy"`
	Rebate         string `json:"rebate"`
	PnL            string `json:"pnl"`
	Category       string `json:"category"`
	CreateTime     string `json:"cTime"`
	UpdateTime     string `json:"uTime"`
	Code           string `json:"code"`
	Msg            string `json:"msg"`
}

// WSOrderMessage represents order push message
type WSOrderMessage struct {
	Arg  WSSubscribeArg `json:"arg"`
	Data []WSOrder      `json:"data"`
}

// OrderState represents order states
type OrderState string

const (
	OrderStateLive            OrderState = "live"
	OrderStatePartiallyFilled OrderState = "partially_filled"
	OrderStateFilled          OrderState = "filled"
	OrderStateCanceled        OrderState = "canceled"
)

// IsTerminal returns true if the order is in a terminal state
func (s OrderState) IsTerminal() bool {
	return s == OrderStateFilled || s == OrderStateCanceled
}

// IsFilled returns true if the order is filled
func (s OrderState) IsFilled() bool {
	return s == OrderStateFilled
}
