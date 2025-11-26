package okx

import "time"

type APIResponse struct {
	Code string        `json:"code"`
	Msg  string        `json:"msg"`
	Data []interface{} `json:"data"`
}

type Balance struct {
	Currency  string `json:"ccy"`
	Balance   string `json:"bal"`
	Available string `json:"availBal"`
	Frozen    string `json:"frozenBal"`
}

type BalanceResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		Details []Balance `json:"details"`
	} `json:"data"`
}

type Position struct {
	InstID        string `json:"instId"`
	PositionSide  string `json:"posSide"`
	Position      string `json:"pos"`
	AvailPosition string `json:"availPos"`
	AveragePrice  string `json:"avgPx"`
	UnrealizedPnL string `json:"upl"`
	Leverage      string `json:"lever"`
}

type PositionResponse struct {
	Code string     `json:"code"`
	Msg  string     `json:"msg"`
	Data []Position `json:"data"`
}

type OrderRequest struct {
	InstID     string `json:"instId"`
	TradeMode  string `json:"tdMode"`
	Side       string `json:"side"`
	OrderType  string `json:"ordType"`
	Size       string `json:"sz"`
	Price      string `json:"px,omitempty"`
	ReduceOnly bool   `json:"reduceOnly,omitempty"`
}

type Order struct {
	OrderID      string `json:"ordId"`
	ClientOrdID  string `json:"clOrdId"`
	InstID       string `json:"instId"`
	Side         string `json:"side"`
	OrderType    string `json:"ordType"`
	Price        string `json:"px"`
	Size         string `json:"sz"`
	FilledSize   string `json:"accFillSz"`
	State        string `json:"state"`
	AveragePrice string `json:"avgPx"`
	CreateTime   string `json:"cTime"`
	UpdateTime   string `json:"uTime"`
}

type OrderResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		OrderID     string `json:"ordId"`
		ClientOrdID string `json:"clOrdId"`
		SCode       string `json:"sCode"`
		SMsg        string `json:"sMsg"`
	} `json:"data"`
}

type CancelOrderRequest struct {
	InstID  string `json:"instId"`
	OrderID string `json:"ordId,omitempty"`
	ClOrdID string `json:"clOrdId,omitempty"`
}

type CancelOrderResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		OrderID     string `json:"ordId"`
		ClientOrdID string `json:"clOrdId"`
		SCode       string `json:"sCode"`
		SMsg        string `json:"sMsg"`
	} `json:"data"`
}

type OrderListResponse struct {
	Code string  `json:"code"`
	Msg  string  `json:"msg"`
	Data []Order `json:"data"`
}

type Fill struct {
	InstID      string `json:"instId"`
	OrderID     string `json:"ordId"`
	TradeID     string `json:"tradeId"`
	Side        string `json:"side"`
	FillSize    string `json:"fillSz"`
	FillPrice   string `json:"fillPx"`
	Fee         string `json:"fee"`
	FeeCurrency string `json:"feeCcy"`
	Timestamp   string `json:"ts"`
}

type FillResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []Fill `json:"data"`
}

type Ticker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	BidPrice  string `json:"bidPx"`
	AskPrice  string `json:"askPx"`
	High24h   string `json:"high24h"`
	Low24h    string `json:"low24h"`
	Vol24h    string `json:"vol24h"`
	Timestamp string `json:"ts"`
}

type TickerResponse struct {
	Code string   `json:"code"`
	Msg  string   `json:"msg"`
	Data []Ticker `json:"data"`
}

type cache struct {
	lastPrice      float64
	lastPriceTime  time.Time
	balance        float64
	position       float64
	unrealizedPnL  float64
	accountUpdated time.Time
}
