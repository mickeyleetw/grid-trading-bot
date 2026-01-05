package okx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"
)

const (
	baseURL = "https://www.okx.com"
)

func (c *Client) doRequest(ctx context.Context, method, path, body string) ([]byte, error) {
	url := baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	headers := c.addAuthHeaders(method, path, body, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		c.logger.Error("API request failed",
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)))

		if resp.StatusCode == 429 {
			return nil, fmt.Errorf("rate limited (HTTP 429)")
		}
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("server error (HTTP %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("client error (HTTP %d)", resp.StatusCode)
	}

	c.logger.Debug("API request succeeded",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status", resp.StatusCode))

	return respBody, nil
}

func (c *Client) GetBalance(ctx context.Context, currency string) (*Balance, error) {
	path := "/api/v5/account/balance"
	if currency != "" {
		path += "?ccy=" + currency
	}

	respBody, err := c.doRequest(ctx, "GET", path, "")
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

func (c *Client) GetPositions(ctx context.Context, instID string) ([]Position, error) {
	path := "/api/v5/account/positions"
	if instID != "" {
		path += "?instId=" + instID
	}

	respBody, err := c.doRequest(ctx, "GET", path, "")
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

func (c *Client) PlaceOrder(ctx context.Context, req *OrderRequest) (string, error) {
	path := "/api/v5/trade/order"

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to serialize order request: %w", err)
	}

	respBody, err := c.doRequest(ctx, "POST", path, string(body))
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

func (c *Client) CancelOrder(ctx context.Context, instID, orderID string) error {
	path := "/api/v5/trade/cancel-order"

	req := CancelOrderRequest{
		InstID:  instID,
		OrderID: orderID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to serialize cancel order request: %w", err)
	}

	respBody, err := c.doRequest(ctx, "POST", path, string(body))
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

func (c *Client) CancelBatchOrders(ctx context.Context, orders []CancelOrderRequest) error {
	path := "/api/v5/trade/cancel-batch-orders"

	body, err := json.Marshal(orders)
	if err != nil {
		return fmt.Errorf("failed to serialize batch cancel order request: %w", err)
	}

	respBody, err := c.doRequest(ctx, "POST", path, string(body))
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

func (c *Client) GetPendingOrders(ctx context.Context, instID string) ([]Order, error) {
	path := "/api/v5/trade/orders-pending"
	if instID != "" {
		path += "?instId=" + instID
	}

	respBody, err := c.doRequest(ctx, "GET", path, "")
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

func (c *Client) GetTicker(ctx context.Context, instID string) (*Ticker, error) {
	path := "/api/v5/market/ticker?instId=" + instID

	respBody, err := c.doRequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}

	var tickerResp TickerResponse
	if err := json.Unmarshal(respBody, &tickerResp); err != nil {
		return nil, fmt.Errorf("failed to parse ticker response: %w", err)
	}

	if err := c.validateAPIResponse(tickerResp.Code, tickerResp.Msg); err != nil {
		return nil, err
	}

	if len(tickerResp.Data) == 0 {
		return nil, fmt.Errorf("no ticker data")
	}

	return &tickerResp.Data[0], nil
}
