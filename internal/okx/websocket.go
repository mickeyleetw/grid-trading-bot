package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// WebSocket URLs for production
	wsPublicURL  = "wss://ws.okx.com:8443/ws/v5/public"
	wsPrivateURL = "wss://ws.okx.com:8443/ws/v5/private"

	// WebSocket URLs for demo trading
	wsDemoPublicURL  = "wss://wspap.okx.com:8443/ws/v5/public?brokerId=9999"
	wsDemoPrivateURL = "wss://wspap.okx.com:8443/ws/v5/private?brokerId=9999"

	// Channel type constants
	channelPublic  = "public"
	channelPrivate = "private"

	// Authentication timeout
	authTimeout = 10 * time.Second
)

// WSConfig holds WebSocket client configuration
type WSConfig struct {
	APIKey     string
	SecretKey  string
	Passphrase string
	Simulated  bool // if true, uses demo trading environment
}

// WebSocketClient manages WebSocket connections to OKX.
// It handles OKX-specific logic: authentication, subscriptions, and message parsing.
// Connection management is delegated to WSTransport.
type WebSocketClient struct {
	config *WSConfig
	logger *zap.Logger

	publicTransport  *WSTransport
	privateTransport *WSTransport

	// OKX specific state
	authenticated bool

	// Subscriptions to restore on reconnect
	publicSubscriptions  []WSSubscribeArg
	privateSubscriptions []WSSubscribeArg

	// Event callbacks
	onTickerUpdate func(ticker *WSTicker)
	onOrderUpdate  func(order *WSOrder)
	onConnected    func(channelType string)
	onDisconnected func(channelType string, err error)

	mu sync.RWMutex
}

// okxPing sends OKX-specific ping message (plain text "ping")
func okxPing(conn *websocket.Conn) error {
	return conn.WriteMessage(websocket.TextMessage, []byte("ping"))
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(config *WSConfig, logger *zap.Logger) *WebSocketClient {
	return &WebSocketClient{
		config:               config,
		logger:               logger,
		publicSubscriptions:  make([]WSSubscribeArg, 0),
		privateSubscriptions: make([]WSSubscribeArg, 0),
	}
}

// SetTickerHandler sets the callback for ticker updates
func (ws *WebSocketClient) SetTickerHandler(handler func(ticker *WSTicker)) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.onTickerUpdate = handler
}

// SetOrderHandler sets the callback for order updates
func (ws *WebSocketClient) SetOrderHandler(handler func(order *WSOrder)) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.onOrderUpdate = handler
}

// SetConnectedHandler sets the callback for connection established
func (ws *WebSocketClient) SetConnectedHandler(handler func(channelType string)) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.onConnected = handler
}

// SetDisconnectedHandler sets the callback for disconnection
func (ws *WebSocketClient) SetDisconnectedHandler(handler func(channelType string, err error)) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.onDisconnected = handler
}

// ConnectPublic establishes connection to public WebSocket channel
func (ws *WebSocketClient) ConnectPublic(ctx context.Context) error {
	ws.mu.Lock()
	if ws.publicTransport != nil && ws.publicTransport.IsConnected() {
		ws.mu.Unlock()
		return nil
	}

	url := wsPublicURL
	if ws.config.Simulated {
		url = wsDemoPublicURL
	}

	config := DefaultTransportConfig(url)
	config.CustomPing = okxPing

	transport := NewWSTransport(config, ws.logger)
	transport.OnMessage = func(msg []byte) {
		ws.handleMessage(channelPublic, msg)
	}
	transport.OnConnected = func() {
		ws.mu.RLock()
		handler := ws.onConnected
		ws.mu.RUnlock()
		if handler != nil {
			handler(channelPublic)
		}
		ws.restoreSubscriptions(channelPublic)
	}
	transport.OnDisconnected = func(err error) {
		ws.mu.RLock()
		handler := ws.onDisconnected
		ws.mu.RUnlock()
		if handler != nil {
			handler(channelPublic, err)
		}
	}

	ws.publicTransport = transport
	ws.mu.Unlock()

	return transport.Connect(ctx)
}

// ConnectPrivate establishes connection to private WebSocket channel with authentication
func (ws *WebSocketClient) ConnectPrivate(ctx context.Context) error {
	ws.mu.Lock()
	if ws.privateTransport != nil && ws.privateTransport.IsConnected() {
		ws.mu.Unlock()
		return nil
	}

	url := wsPrivateURL
	if ws.config.Simulated {
		url = wsDemoPrivateURL
	}

	config := DefaultTransportConfig(url)
	config.CustomPing = okxPing

	transport := NewWSTransport(config, ws.logger)

	// Set up callbacks before connecting
	transport.OnMessage = func(msg []byte) {
		ws.handleMessage(channelPrivate, msg)
	}
	transport.OnConnected = func() {
		ws.mu.RLock()
		handler := ws.onConnected
		ws.mu.RUnlock()
		if handler != nil {
			handler(channelPrivate)
		}
		ws.restoreSubscriptions(channelPrivate)
	}
	transport.OnDisconnected = func(err error) {
		ws.mu.Lock()
		ws.authenticated = false
		ws.mu.Unlock()

		ws.mu.RLock()
		handler := ws.onDisconnected
		ws.mu.RUnlock()
		if handler != nil {
			handler(channelPrivate, err)
		}
	}
	// OnBeforePump handles re-authentication after reconnect
	transport.OnBeforePump = func() error {
		ws.logger.Info("Re-authenticating after reconnect...")
		if err := ws.authenticate(); err != nil {
			ws.logger.Error("Re-authentication failed", zap.Error(err))
			return err
		}
		ws.mu.Lock()
		ws.authenticated = true
		ws.mu.Unlock()
		ws.logger.Info("Re-authentication successful")
		return nil
	}

	ws.privateTransport = transport
	ws.mu.Unlock()

	// Connect without starting pump (so we can authenticate first)
	if err := transport.ConnectOnly(ctx); err != nil {
		return err
	}

	// Authenticate synchronously before starting pump
	ws.logger.Info("Private WebSocket connected, authenticating...")
	if err := ws.authenticate(); err != nil {
		transport.Disconnect()
		ws.logger.Error("Private WebSocket authentication failed", zap.Error(err))
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	ws.mu.Lock()
	ws.authenticated = true
	ws.mu.Unlock()

	ws.logger.Info("Private WebSocket authenticated")

	// Now start the pump - this will trigger OnConnected callback
	transport.StartPump()

	return nil
}

// authenticate sends login request to private channel
func (ws *WebSocketClient) authenticate() error {
	ws.mu.RLock()
	transport := ws.privateTransport
	ws.mu.RUnlock()

	if transport == nil {
		return ErrNotConnected
	}

	loginReq := buildLoginRequest(ws.config.APIKey, ws.config.SecretKey, ws.config.Passphrase)

	if err := transport.SendJSON(loginReq); err != nil {
		return fmt.Errorf("failed to send login request: %w", err)
	}

	// Wait for login response using ReadOnce (before pump is started)
	message, err := transport.ReadOnce(authTimeout)
	if err != nil {
		return fmt.Errorf("failed to read login response: %w", err)
	}

	var resp WSLoginResponse
	if err := json.Unmarshal(message, &resp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	if resp.Event != WSEventLogin || resp.Code != "0" {
		return fmt.Errorf("login failed: code=%s, msg=%s", resp.Code, resp.Msg)
	}

	return nil
}

// hasSubscription checks if a subscription already exists.
func (ws *WebSocketClient) hasSubscription(subs []WSSubscribeArg, channel, instID string) bool {
	for _, sub := range subs {
		if sub.Channel == channel && sub.InstID == instID {
			return true
		}
	}
	return false
}

// SubscribeTicker subscribes to ticker channel for the given instrument.
func (ws *WebSocketClient) SubscribeTicker(instID string) error {
	ws.mu.RLock()
	transport := ws.publicTransport
	alreadySubscribed := ws.hasSubscription(ws.publicSubscriptions, "tickers", instID)
	ws.mu.RUnlock()

	if transport == nil || !transport.IsConnected() {
		return ErrNotConnected
	}

	if alreadySubscribed {
		ws.logger.Debug("Already subscribed to ticker", zap.String("instId", instID))
		return nil
	}

	req := buildSubscribeRequest("tickers", instID)
	if err := transport.SendJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe to ticker: %w", err)
	}

	ws.mu.Lock()
	ws.publicSubscriptions = append(ws.publicSubscriptions, WSSubscribeArg{
		Channel: "tickers",
		InstID:  instID,
	})
	ws.mu.Unlock()

	ws.logger.Info("Subscribed to ticker", zap.String("instId", instID))
	return nil
}

// SubscribeOrders subscribes to orders channel for the given instrument.
func (ws *WebSocketClient) SubscribeOrders(instID string) error {
	ws.mu.RLock()
	transport := ws.privateTransport
	authenticated := ws.authenticated
	alreadySubscribed := ws.hasSubscription(ws.privateSubscriptions, "orders", instID)
	ws.mu.RUnlock()

	if transport == nil || !transport.IsConnected() {
		return ErrNotConnected
	}
	if !authenticated {
		return fmt.Errorf("private WebSocket not authenticated")
	}

	if alreadySubscribed {
		ws.logger.Debug("Already subscribed to orders", zap.String("instId", instID))
		return nil
	}

	arg := WSSubscribeArg{
		Channel:  "orders",
		InstType: "SWAP",
		InstID:   instID,
	}

	req := &WSRequest{
		Op:   WSOpSubscribe,
		Args: []any{arg},
	}

	if err := transport.SendJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe to orders: %w", err)
	}

	ws.mu.Lock()
	ws.privateSubscriptions = append(ws.privateSubscriptions, arg)
	ws.mu.Unlock()

	ws.logger.Info("Subscribed to orders", zap.String("instId", instID))
	return nil
}

// restoreSubscriptions re-subscribes to all channels after reconnect
func (ws *WebSocketClient) restoreSubscriptions(channelType string) {
	ws.mu.RLock()
	var subs []WSSubscribeArg
	var transport *WSTransport
	if channelType == channelPublic {
		subs = ws.publicSubscriptions
		transport = ws.publicTransport
	} else {
		subs = ws.privateSubscriptions
		transport = ws.privateTransport
	}
	ws.mu.RUnlock()

	if transport == nil {
		return
	}

	for _, sub := range subs {
		req := &WSRequest{
			Op:   WSOpSubscribe,
			Args: []any{sub},
		}
		if err := transport.SendJSON(req); err != nil {
			ws.logger.Error("Failed to restore subscription",
				zap.String("channel", sub.Channel),
				zap.String("instId", sub.InstID),
				zap.Error(err))
		} else {
			ws.logger.Info("Restored subscription",
				zap.String("channel", sub.Channel),
				zap.String("instId", sub.InstID))
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (ws *WebSocketClient) handleMessage(channelType string, message []byte) {
	// Check for pong response (plain text)
	if string(message) == "pong" {
		ws.logger.Debug("Received pong", zap.String("channel", channelType))
		return
	}

	// Try to parse as generic response first
	var resp WSResponse
	if err := json.Unmarshal(message, &resp); err != nil {
		ws.logger.Warn("Failed to parse message",
			zap.String("channel", channelType),
			zap.String("message", string(message)),
			zap.Error(err))
		return
	}

	// Handle event responses
	if resp.Event != "" {
		ws.handleEventResponse(channelType, resp, message)
		return
	}

	// Handle data messages
	ws.handleDataMessage(channelType, message)
}

// handleEventResponse processes event-type messages
func (ws *WebSocketClient) handleEventResponse(channelType string, resp WSResponse, raw []byte) {
	switch resp.Event {
	case WSEventSubscribe:
		var subResp WSSubscribeResponse
		if err := json.Unmarshal(raw, &subResp); err == nil && subResp.Arg != nil {
			ws.logger.Info("Subscription confirmed",
				zap.String("channel", subResp.Arg.Channel),
				zap.String("instId", subResp.Arg.InstID))
		}
	case WSEventError:
		ws.logger.Error("WebSocket error",
			zap.String("channelType", channelType),
			zap.String("code", resp.Code),
			zap.String("msg", resp.Msg))
	default:
		ws.logger.Debug("Received event",
			zap.String("channelType", channelType),
			zap.String("event", string(resp.Event)))
	}
}

// channelPreview is a lightweight struct to peek at channel type
type channelPreview struct {
	Arg struct {
		Channel string `json:"channel"`
	} `json:"arg"`
}

// handleDataMessage processes data push messages
func (ws *WebSocketClient) handleDataMessage(channelType string, message []byte) {
	var preview channelPreview
	if err := json.Unmarshal(message, &preview); err != nil {
		ws.logger.Debug("Failed to preview message channel", zap.Error(err))
		return
	}

	switch preview.Arg.Channel {
	case "tickers":
		var tickerMsg WSTickerMessage
		if err := json.Unmarshal(message, &tickerMsg); err != nil {
			ws.logger.Warn("Failed to parse ticker message", zap.Error(err))
			return
		}

		ws.mu.RLock()
		handler := ws.onTickerUpdate
		ws.mu.RUnlock()

		if handler != nil && len(tickerMsg.Data) > 0 {
			for i := range tickerMsg.Data {
				handler(&tickerMsg.Data[i])
			}
		}

	case "orders":
		var orderMsg WSOrderMessage
		if err := json.Unmarshal(message, &orderMsg); err != nil {
			ws.logger.Warn("Failed to parse order message", zap.Error(err))
			return
		}

		ws.mu.RLock()
		handler := ws.onOrderUpdate
		ws.mu.RUnlock()

		if handler != nil && len(orderMsg.Data) > 0 {
			for i := range orderMsg.Data {
				handler(&orderMsg.Data[i])
			}
		}

	default:
		ws.logger.Debug("Unhandled message",
			zap.String("channelType", channelType),
			zap.String("dataChannel", preview.Arg.Channel))
	}
}

// Disconnect closes all WebSocket connections
func (ws *WebSocketClient) Disconnect() {
	ws.logger.Info("Disconnecting WebSocket client")

	ws.mu.Lock()
	publicTransport := ws.publicTransport
	privateTransport := ws.privateTransport
	ws.authenticated = false
	ws.mu.Unlock()

	if publicTransport != nil {
		publicTransport.Disconnect()
	}
	if privateTransport != nil {
		privateTransport.Disconnect()
	}

	ws.logger.Info("WebSocket client disconnected")
}

// IsPublicConnected returns true if public channel is connected
func (ws *WebSocketClient) IsPublicConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.publicTransport != nil && ws.publicTransport.IsConnected()
}

// IsPrivateConnected returns true if private channel is connected
func (ws *WebSocketClient) IsPrivateConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.privateTransport != nil && ws.privateTransport.IsConnected()
}

// IsAuthenticated returns true if private channel is authenticated
func (ws *WebSocketClient) IsAuthenticated() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.authenticated
}
