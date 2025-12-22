package okx

import (
	"context"
	"encoding/json"
	"errors"
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

	// Ping interval (OKX recommends < 30s)
	pingInterval = 25 * time.Second

	// Read timeout - if no message received within this time, consider disconnected
	readTimeout = 60 * time.Second

	// Handshake timeout for WebSocket connection
	handshakeTimeout = 10 * time.Second

	// Maximum reconnection attempts
	maxReconnectAttempts = 3

	// Initial reconnection delay
	initialReconnectDelay = 1 * time.Second

	// Channel type constants
	channelPublic  = "public"
	channelPrivate = "private"
)

// wsDialer is a custom dialer with timeout settings
var wsDialer = websocket.Dialer{
	HandshakeTimeout: handshakeTimeout,
}

// WSConfig holds WebSocket client configuration
type WSConfig struct {
	APIKey     string
	SecretKey  string
	Passphrase string
	Simulated  bool // if true, uses demo trading environment
}

// WebSocketClient manages WebSocket connections to OKX
type WebSocketClient struct {
	config *WSConfig
	logger *zap.Logger

	publicConn  *websocket.Conn
	privateConn *websocket.Conn

	// Connection state
	publicConnected  bool
	privateConnected bool
	authenticated    bool
	stopped          bool // Tracks if Disconnect() has been called

	// Subscriptions to restore on reconnect
	publicSubscriptions  []WSSubscribeArg
	privateSubscriptions []WSSubscribeArg

	// Event callbacks
	onTickerUpdate func(ticker *WSTicker)
	onOrderUpdate  func(order *WSOrder)
	onConnected    func(channelType string)
	onDisconnected func(channelType string, err error)

	// Control channels
	stopChan    chan struct{}
	publicDone  chan struct{}
	privateDone chan struct{}

	// Reconnection tracking
	publicReconnectCount  int
	privateReconnectCount int

	mu sync.RWMutex
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(config *WSConfig, logger *zap.Logger) *WebSocketClient {
	return &WebSocketClient{
		config:               config,
		logger:               logger,
		publicSubscriptions:  make([]WSSubscribeArg, 0),
		privateSubscriptions: make([]WSSubscribeArg, 0),
		stopChan:             make(chan struct{}),
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
	if ws.publicConnected {
		ws.mu.Unlock()
		return nil
	}
	ws.mu.Unlock()

	url := wsPublicURL
	if ws.config.Simulated {
		url = wsDemoPublicURL
	}

	ws.logger.Info("Connecting to public WebSocket", zap.String("url", url))

	conn, resp, err := wsDialer.DialContext(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		ws.logger.Error("Failed to connect to public WebSocket", zap.Error(err))
		return fmt.Errorf("failed to connect to public WebSocket: %w", err)
	}

	ws.mu.Lock()
	ws.publicConn = conn
	ws.publicConnected = true
	ws.publicReconnectCount = 0
	ws.publicDone = make(chan struct{})
	ws.mu.Unlock()

	ws.logger.Info("Public WebSocket connected")

	// Start message reader and heartbeat
	go ws.readPump(channelPublic, conn, ws.publicDone)
	go ws.heartbeat(channelPublic, conn, ws.publicDone)

	// Notify connection established
	ws.mu.RLock()
	handler := ws.onConnected
	ws.mu.RUnlock()
	if handler != nil {
		handler(channelPublic)
	}

	// Restore subscriptions
	ws.restoreSubscriptions(channelPublic)

	return nil
}

// ConnectPrivate establishes connection to private WebSocket channel with authentication
func (ws *WebSocketClient) ConnectPrivate(ctx context.Context) error {
	ws.mu.Lock()
	if ws.privateConnected {
		ws.mu.Unlock()
		return nil
	}
	ws.mu.Unlock()

	url := wsPrivateURL
	if ws.config.Simulated {
		url = wsDemoPrivateURL
	}

	ws.logger.Info("Connecting to private WebSocket", zap.String("url", url))

	conn, resp, err := wsDialer.DialContext(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		ws.logger.Error("Failed to connect to private WebSocket", zap.Error(err))
		return fmt.Errorf("failed to connect to private WebSocket: %w", err)
	}

	ws.logger.Info("Private WebSocket connected, authenticating...")

	// Authenticate BEFORE setting connected state to avoid resource leak
	if err := ws.authenticate(conn); err != nil {
		_ = conn.Close() // Close connection directly without closeConnection()
		ws.logger.Error("Private WebSocket authentication failed", zap.Error(err))
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	// Only set state after successful authentication
	ws.mu.Lock()
	ws.privateConn = conn
	ws.privateConnected = true
	ws.authenticated = true
	ws.privateReconnectCount = 0
	ws.privateDone = make(chan struct{})
	ws.mu.Unlock()

	ws.logger.Info("Private WebSocket authenticated")

	// Start message reader and heartbeat
	go ws.readPump(channelPrivate, conn, ws.privateDone)
	go ws.heartbeat(channelPrivate, conn, ws.privateDone)

	// Notify connection established
	ws.mu.RLock()
	handler := ws.onConnected
	ws.mu.RUnlock()
	if handler != nil {
		handler(channelPrivate)
	}

	// Restore subscriptions
	ws.restoreSubscriptions(channelPrivate)

	return nil
}

// authenticate sends login request to private channel
func (ws *WebSocketClient) authenticate(conn *websocket.Conn) error {
	loginReq := buildLoginRequest(ws.config.APIKey, ws.config.SecretKey, ws.config.Passphrase)

	if err := conn.WriteJSON(loginReq); err != nil {
		return fmt.Errorf("failed to send login request: %w", err)
	}

	// Wait for login response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read login response: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{}) // Clear deadline

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
// IMPORTANT: Caller must hold at least RLock before calling this function.
func (ws *WebSocketClient) hasSubscription(subs []WSSubscribeArg, channel, instID string) bool {
	for _, sub := range subs {
		if sub.Channel == channel && sub.InstID == instID {
			return true
		}
	}
	return false
}

// SubscribeTicker subscribes to ticker channel for the given instrument.
// If the connection is not established, returns an error. Subscriptions are
// automatically restored after reconnection via restoreSubscriptions().
// TODO: Consider waiting for server confirmation before storing subscription
// to handle cases where the server rejects the subscription request.
func (ws *WebSocketClient) SubscribeTicker(instID string) error {
	ws.mu.RLock()
	conn := ws.publicConn
	connected := ws.publicConnected
	alreadySubscribed := ws.hasSubscription(ws.publicSubscriptions, "tickers", instID)
	ws.mu.RUnlock()

	if !connected || conn == nil {
		return errors.New("public WebSocket not connected")
	}

	if alreadySubscribed {
		ws.logger.Debug("Already subscribed to ticker", zap.String("instId", instID))
		return nil
	}

	req := buildSubscribeRequest("tickers", instID)
	if err := conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe to ticker: %w", err)
	}

	// Store subscription for reconnect (avoid duplicates)
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
// Requires private channel to be connected and authenticated.
// Subscriptions are automatically restored after reconnection via restoreSubscriptions().
// TODO: Consider waiting for server confirmation before storing subscription
// to handle cases where the server rejects the subscription request.
func (ws *WebSocketClient) SubscribeOrders(instID string) error {
	ws.mu.RLock()
	conn := ws.privateConn
	connected := ws.privateConnected
	authenticated := ws.authenticated
	alreadySubscribed := ws.hasSubscription(ws.privateSubscriptions, "orders", instID)
	ws.mu.RUnlock()

	if !connected || conn == nil {
		return errors.New("private WebSocket not connected")
	}
	if !authenticated {
		return errors.New("private WebSocket not authenticated")
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

	if err := conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to subscribe to orders: %w", err)
	}

	// Store subscription for reconnect (avoid duplicates)
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
	var conn *websocket.Conn
	if channelType == channelPublic {
		subs = ws.publicSubscriptions
		conn = ws.publicConn
	} else {
		subs = ws.privateSubscriptions
		conn = ws.privateConn
	}
	ws.mu.RUnlock()

	for _, sub := range subs {
		req := &WSRequest{
			Op:   WSOpSubscribe,
			Args: []any{sub},
		}
		if err := conn.WriteJSON(req); err != nil {
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

// readPump continuously reads messages from WebSocket.
// Note: The select with default ensures we check stop signals before each read.
// ReadMessage() will block for up to readTimeout (60s) before timing out,
// so there may be a delay of up to 60s before responding to stop signals.
func (ws *WebSocketClient) readPump(channelType string, conn *websocket.Conn, done chan struct{}) {
	defer func() {
		ws.handleDisconnect(channelType, nil)
	}()

	for {
		// Check stop signals before blocking on read
		select {
		case <-done:
			return
		case <-ws.stopChan:
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				ws.logger.Info("WebSocket closed normally", zap.String("channel", channelType))
				return
			}
			ws.logger.Error("WebSocket read error",
				zap.String("channel", channelType),
				zap.Error(err))
			ws.handleDisconnect(channelType, err)
			return
		}

		ws.handleMessage(channelType, message)
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
	// First, peek at the channel type to avoid unnecessary parsing
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

// heartbeat sends periodic ping messages to keep connection alive
func (ws *WebSocketClient) heartbeat(channelType string, conn *websocket.Conn, done chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ws.stopChan:
			return
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
				ws.logger.Error("Failed to send ping",
					zap.String("channel", channelType),
					zap.Error(err))
				return
			}
			ws.logger.Debug("Sent ping", zap.String("channel", channelType))
		}
	}
}

// handleDisconnect handles disconnection and triggers reconnection
func (ws *WebSocketClient) handleDisconnect(channelType string, err error) {
	ws.mu.Lock()
	var wasConnected bool
	if channelType == channelPublic {
		wasConnected = ws.publicConnected
		ws.publicConnected = false
	} else {
		wasConnected = ws.privateConnected
		ws.privateConnected = false
		ws.authenticated = false
	}
	ws.mu.Unlock()

	if !wasConnected {
		return
	}

	ws.logger.Warn("WebSocket disconnected",
		zap.String("channel", channelType),
		zap.Error(err))

	// Notify disconnection
	ws.mu.RLock()
	handler := ws.onDisconnected
	ws.mu.RUnlock()
	if handler != nil {
		handler(channelType, err)
	}

	// Attempt reconnection
	go ws.reconnect(channelType)
}

// reconnect attempts to reconnect with exponential backoff
func (ws *WebSocketClient) reconnect(channelType string) {
	ws.mu.Lock()
	var reconnectCount *int
	if channelType == channelPublic {
		reconnectCount = &ws.publicReconnectCount
	} else {
		reconnectCount = &ws.privateReconnectCount
	}
	*reconnectCount++
	count := *reconnectCount
	ws.mu.Unlock()

	if count > maxReconnectAttempts {
		ws.logger.Error("Max reconnection attempts reached, giving up",
			zap.String("channel", channelType),
			zap.Int("attempts", count),
			zap.Int("maxAttempts", maxReconnectAttempts))
		return
	}

	// Exponential backoff: 1s, 2s, 4s
	delay := initialReconnectDelay * time.Duration(1<<(count-1))
	ws.logger.Info("Attempting reconnection",
		zap.String("channel", channelType),
		zap.Int("attempt", count),
		zap.Int("maxAttempts", maxReconnectAttempts),
		zap.Duration("delay", delay))

	select {
	case <-time.After(delay):
	case <-ws.stopChan:
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	if channelType == channelPublic {
		err = ws.ConnectPublic(ctx)
	} else {
		err = ws.ConnectPrivate(ctx)
	}

	if err != nil {
		ws.logger.Error("Reconnection failed",
			zap.String("channel", channelType),
			zap.Int("attempt", count),
			zap.Error(err))
		// Try again
		ws.handleDisconnect(channelType, err)
	}
}

// closeConnection closes a WebSocket connection safely (prevents double close)
func (ws *WebSocketClient) closeConnection(channelType string, conn *websocket.Conn) {
	if conn == nil {
		return
	}

	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()

	ws.mu.Lock()
	defer ws.mu.Unlock()

	if channelType == channelPublic {
		ws.publicConnected = false
		ws.publicConn = nil
		if ws.publicDone != nil {
			close(ws.publicDone)
			ws.publicDone = nil // Prevent double close
		}
	} else {
		ws.privateConnected = false
		ws.privateConn = nil
		ws.authenticated = false
		if ws.privateDone != nil {
			close(ws.privateDone)
			ws.privateDone = nil // Prevent double close
		}
	}
}

// Disconnect closes all WebSocket connections safely
func (ws *WebSocketClient) Disconnect() {
	ws.mu.Lock()
	if ws.stopped {
		ws.mu.Unlock()
		return // Already disconnected
	}
	ws.stopped = true

	// Close stopChan safely
	select {
	case <-ws.stopChan:
		// Already closed
	default:
		close(ws.stopChan)
	}

	publicConn := ws.publicConn
	privateConn := ws.privateConn
	ws.mu.Unlock()

	ws.logger.Info("Disconnecting WebSocket client")

	ws.closeConnection(channelPublic, publicConn)
	ws.closeConnection(channelPrivate, privateConn)

	ws.logger.Info("WebSocket client disconnected")
}

// IsPublicConnected returns true if public channel is connected
func (ws *WebSocketClient) IsPublicConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.publicConnected
}

// IsPrivateConnected returns true if private channel is connected
func (ws *WebSocketClient) IsPrivateConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.privateConnected
}

// IsAuthenticated returns true if private channel is authenticated
func (ws *WebSocketClient) IsAuthenticated() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.authenticated
}
