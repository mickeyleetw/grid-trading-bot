package okx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

func (c *Client) doRequest(method, path, body string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Debug("Retrying API request",
				zap.Int("attempt", attempt),
				zap.String("path", path))
			time.Sleep(retryDelay)
		}

		url := c.baseURL + path
		req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		headers := c.addAuthHeaders(method, path, body, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to execute request: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("Failed to close response body", zap.Error(closeErr))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: status=%d, body=%s", resp.StatusCode, string(respBody))
			continue
		}

		if resp.StatusCode >= 400 {
			c.logger.Error("API request failed",
				zap.Int("status", resp.StatusCode),
				zap.String("path", path),
				zap.String("body", string(respBody)))
			return nil, fmt.Errorf("HTTP error: status=%d, body=%s", resp.StatusCode, string(respBody))
		}

		c.logger.Debug("API request succeeded",
			zap.String("path", path),
			zap.Int("status", resp.StatusCode))

		return respBody, nil
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func (c *Client) GetBalance(currency string) (*Balance, error) {
	path := "/api/v5/account/balance"
	if currency != "" {
		path += "?ccy=" + currency
	}

	respBody, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	var balanceResp BalanceResponse
	if err := json.Unmarshal(respBody, &balanceResp); err != nil {
		return nil, fmt.Errorf("failed to parse balance response: %w", err)
	}

	if err := c.validateAPIResponse(balanceResp.Code, balanceResp.Msg); err != nil {
		return nil, err
	}

	if len(balanceResp.Data) == 0 || len(balanceResp.Data[0].Details) == 0 {
		return nil, fmt.Errorf("no balance data")
	}

	for _, detail := range balanceResp.Data[0].Details {
		if currency == "" || detail.Currency == currency {
			return &detail, nil
		}
	}

	return nil, fmt.Errorf("balance not found for currency %s", currency)
}

func (c *Client) GetPositions(instID string) ([]Position, error) {
	path := "/api/v5/account/positions"
	if instID != "" {
		path += "?instId=" + instID
	}

	respBody, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	var posResp PositionResponse
	if err := json.Unmarshal(respBody, &posResp); err != nil {
		return nil, fmt.Errorf("failed to parse position response: %w", err)
	}

	if err := c.validateAPIResponse(posResp.Code, posResp.Msg); err != nil {
		return nil, err
	}

	return posResp.Data, nil
}

func (c *Client) PlaceOrder(req *OrderRequest) (string, error) {
	path := "/api/v5/trade/order"

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to serialize order request: %w", err)
	}

	respBody, err := c.doRequest("POST", path, string(body))
	if err != nil {
		return "", err
	}

	var orderResp OrderResponse
	if err := json.Unmarshal(respBody, &orderResp); err != nil {
		return "", fmt.Errorf("failed to parse order response: %w", err)
	}

	if err := c.validateAPIResponse(orderResp.Code, orderResp.Msg); err != nil {
		return "", err
	}

	if len(orderResp.Data) == 0 {
		return "", fmt.Errorf("order response has no data")
	}

	if orderResp.Data[0].SCode != "0" {
		return "", fmt.Errorf("order failed: %s", orderResp.Data[0].SMsg)
	}

	c.logger.Info("Order placed successfully",
		zap.String("orderId", orderResp.Data[0].OrderID),
		zap.String("instId", req.InstID),
		zap.String("side", req.Side))

	return orderResp.Data[0].OrderID, nil
}

func (c *Client) CancelOrder(instID, orderID string) error {
	path := "/api/v5/trade/cancel-order"

	req := CancelOrderRequest{
		InstID:  instID,
		OrderID: orderID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to serialize cancel order request: %w", err)
	}

	respBody, err := c.doRequest("POST", path, string(body))
	if err != nil {
		return err
	}

	var cancelResp CancelOrderResponse
	if err := json.Unmarshal(respBody, &cancelResp); err != nil {
		return fmt.Errorf("failed to parse cancel order response: %w", err)
	}

	if err := c.validateAPIResponse(cancelResp.Code, cancelResp.Msg); err != nil {
		return err
	}

	if len(cancelResp.Data) == 0 {
		return fmt.Errorf("cancel order response has no data")
	}

	if cancelResp.Data[0].SCode != "0" {
		return fmt.Errorf("cancel order failed: %s", cancelResp.Data[0].SMsg)
	}

	c.logger.Info("Order cancelled successfully",
		zap.String("orderId", orderID),
		zap.String("instId", instID))

	return nil
}

func (c *Client) CancelBatchOrders(orders []CancelOrderRequest) error {
	path := "/api/v5/trade/cancel-batch-orders"

	body, err := json.Marshal(orders)
	if err != nil {
		return fmt.Errorf("failed to serialize batch cancel order request: %w", err)
	}

	respBody, err := c.doRequest("POST", path, string(body))
	if err != nil {
		return err
	}

	var cancelResp CancelOrderResponse
	if err := json.Unmarshal(respBody, &cancelResp); err != nil {
		return fmt.Errorf("failed to parse batch cancel order response: %w", err)
	}

	if err := c.validateAPIResponse(cancelResp.Code, cancelResp.Msg); err != nil {
		return err
	}

	c.logger.Info("Batch cancel order completed",
		zap.Int("count", len(orders)))

	return nil
}

func (c *Client) GetPendingOrders(instID string) ([]Order, error) {
	path := "/api/v5/trade/orders-pending"
	if instID != "" {
		path += "?instId=" + instID
	}

	respBody, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	var orderResp OrderListResponse
	if err := json.Unmarshal(respBody, &orderResp); err != nil {
		return nil, fmt.Errorf("failed to parse pending orders response: %w", err)
	}

	if err := c.validateAPIResponse(orderResp.Code, orderResp.Msg); err != nil {
		return nil, err
	}

	return orderResp.Data, nil
}

func (c *Client) GetFills(instID string, limit int) ([]Fill, error) {
	path := "/api/v5/trade/fills?instType=SWAP"
	if instID != "" {
		path += "&instId=" + instID
	}
	if limit > 0 {
		path += "&limit=" + strconv.Itoa(limit)
	}

	respBody, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	var fillResp FillResponse
	if err := json.Unmarshal(respBody, &fillResp); err != nil {
		return nil, fmt.Errorf("failed to parse fill history response: %w", err)
	}

	if err := c.validateAPIResponse(fillResp.Code, fillResp.Msg); err != nil {
		return nil, err
	}

	return fillResp.Data, nil
}

func (c *Client) GetTicker(instID string) (*Ticker, error) {
	if time.Since(c.cache.lastPriceTime) < priceCacheDuration && c.cache.lastPrice > 0 {
		c.logger.Debug("Using cached price",
			zap.Float64("price", c.cache.lastPrice),
			zap.Duration("age", time.Since(c.cache.lastPriceTime)))
		return &Ticker{
			InstID: instID,
			Last:   fmt.Sprintf("%.8f", c.cache.lastPrice),
		}, nil
	}

	path := "/api/v5/market/ticker?instId=" + instID

	respBody, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	var tickerResp TickerResponse
	if parseErr := json.Unmarshal(respBody, &tickerResp); parseErr != nil {
		return nil, fmt.Errorf("failed to parse ticker response: %w", parseErr)
	}

	if validateErr := c.validateAPIResponse(tickerResp.Code, tickerResp.Msg); validateErr != nil {
		return nil, validateErr
	}

	if len(tickerResp.Data) == 0 {
		return nil, fmt.Errorf("no ticker data")
	}

	price, priceErr := strconv.ParseFloat(tickerResp.Data[0].Last, 64)
	if priceErr == nil {
		c.cache.lastPrice = price
		c.cache.lastPriceTime = time.Now()
		c.logger.Debug("Updated price cache",
			zap.Float64("price", price))
	}

	return &tickerResp.Data[0], nil
}

func (c *Client) UpdateAccountCache(instID string) error {
	if time.Since(c.cache.accountUpdated) < accountCacheDuration {
		return nil
	}

	balance, err := c.GetBalance("USDT")
	if err != nil {
		return fmt.Errorf("failed to query balance: %w", err)
	}

	balanceFloat, err := strconv.ParseFloat(balance.Available, 64)
	if err != nil {
		return fmt.Errorf("failed to parse balance: %w", err)
	}
	c.cache.balance = balanceFloat

	positions, err := c.GetPositions(instID)
	if err != nil {
		return fmt.Errorf("failed to query positions: %w", err)
	}

	if len(positions) > 0 {
		pos, err := strconv.ParseFloat(positions[0].Position, 64)
		if err == nil {
			c.cache.position = pos
		}

		upl, err := strconv.ParseFloat(positions[0].UnrealizedPnL, 64)
		if err == nil {
			c.cache.unrealizedPnL = upl
		}
	} else {
		c.cache.position = 0
		c.cache.unrealizedPnL = 0
	}

	c.cache.accountUpdated = time.Now()

	c.logger.Debug("Updated account cache",
		zap.Float64("balance", c.cache.balance),
		zap.Float64("position", c.cache.position),
		zap.Float64("unrealizedPnL", c.cache.unrealizedPnL))

	return nil
}

func (c *Client) GetCachedBalance() float64 {
	return c.cache.balance
}

func (c *Client) GetCachedPosition() float64 {
	return c.cache.position
}

func (c *Client) GetCachedUnrealizedPnL() float64 {
	return c.cache.unrealizedPnL
}
