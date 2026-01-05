package okx

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ErrNotConnected is returned when attempting to send on a disconnected transport
var ErrNotConnected = errors.New("websocket not connected")

// TransportConfig holds configuration for WSTransport
type TransportConfig struct {
	URL                   string
	HandshakeTimeout      time.Duration
	PingInterval          time.Duration
	ReadTimeout           time.Duration
	MaxReconnectAttempts  int
	InitialReconnectDelay time.Duration

	// CustomPing allows custom heartbeat implementation.
	// If nil, standard WebSocket PingMessage is used.
	// Example: OKX requires sending "ping" as text message.
	CustomPing func(conn *websocket.Conn) error
}

// DefaultTransportConfig returns sensible defaults
func DefaultTransportConfig(url string) *TransportConfig {
	return &TransportConfig{
		URL:                   url,
		HandshakeTimeout:      10 * time.Second,
		PingInterval:          25 * time.Second,
		ReadTimeout:           60 * time.Second,
		MaxReconnectAttempts:  3,
		InitialReconnectDelay: 1 * time.Second,
	}
}

// WSTransport manages a single WebSocket connection.
// It handles connection lifecycle, heartbeat, and automatic reconnection.
// This is a generic transport layer with no exchange-specific logic.
type WSTransport struct {
	config *TransportConfig
	logger *zap.Logger

	conn      *websocket.Conn
	connected bool
	stopped   bool
	pumping   bool // tracks if readPump is running

	// Callbacks set by the client
	OnMessage      func([]byte)
	OnConnected    func()
	OnDisconnected func(error)
	// OnBeforePump is called after reconnect but before starting the pump.
	// Use this for operations that must complete before message processing starts
	// (e.g., authentication). If it returns an error, the connection is closed.
	OnBeforePump func() error

	// Reconnection tracking
	reconnectCount int

	// Control channels
	done     chan struct{}
	stopChan chan struct{}

	mu sync.RWMutex
}

// NewWSTransport creates a new WebSocket transport
func NewWSTransport(config *TransportConfig, logger *zap.Logger) *WSTransport {
	return &WSTransport{
		config:   config,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection and starts background goroutines
func (t *WSTransport) Connect(ctx context.Context) error {
	if err := t.ConnectOnly(ctx); err != nil {
		return err
	}
	t.StartPump()
	return nil
}

// ConnectOnly establishes connection without starting readPump/heartbeat.
// Use this when you need to perform synchronous operations (like authentication)
// before starting the message pump. Call StartPump() after.
func (t *WSTransport) ConnectOnly(ctx context.Context) error {
	t.mu.Lock()
	if t.connected {
		t.mu.Unlock()
		return nil
	}
	if t.stopped {
		t.stopChan = make(chan struct{})
		t.stopped = false
	}
	t.mu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: t.config.HandshakeTimeout,
	}

	t.logger.Info("Connecting to WebSocket", zap.String("url", t.config.URL))

	conn, resp, err := dialer.DialContext(ctx, t.config.URL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.logger.Error("Failed to connect to WebSocket", zap.Error(err))
		return err
	}

	t.mu.Lock()
	t.conn = conn
	t.connected = true
	t.reconnectCount = 0
	t.done = make(chan struct{})
	t.mu.Unlock()

	t.logger.Info("WebSocket connected")

	return nil
}

// StartPump starts the readPump and heartbeat goroutines.
// Should be called after ConnectOnly and any synchronous operations.
func (t *WSTransport) StartPump() {
	t.mu.Lock()
	if t.pumping || !t.connected {
		t.mu.Unlock()
		return
	}
	t.pumping = true
	t.mu.Unlock()

	go t.readPump()
	go t.heartbeat()

	// Notify connection established
	if t.OnConnected != nil {
		t.OnConnected()
	}
}

// Disconnect closes the WebSocket connection
func (t *WSTransport) Disconnect() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true

	select {
	case <-t.stopChan:
	default:
		close(t.stopChan)
	}

	conn := t.conn
	t.mu.Unlock()

	t.logger.Info("Disconnecting WebSocket")
	t.close(conn)
	t.logger.Info("WebSocket disconnected")
}

// Send sends raw bytes through the WebSocket
func (t *WSTransport) Send(data []byte) error {
	t.mu.RLock()
	conn := t.conn
	connected := t.connected
	t.mu.RUnlock()

	if !connected || conn == nil {
		return ErrNotConnected
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// SendJSON sends a JSON-encoded message
func (t *WSTransport) SendJSON(v any) error {
	t.mu.RLock()
	conn := t.conn
	connected := t.connected
	t.mu.RUnlock()

	if !connected || conn == nil {
		return ErrNotConnected
	}

	return conn.WriteJSON(v)
}

// ReadOnce reads a single message with timeout.
// This is for synchronous operations like authentication.
// Should only be called before StartPump() to avoid race conditions.
func (t *WSTransport) ReadOnce(timeout time.Duration) ([]byte, error) {
	t.mu.RLock()
	conn := t.conn
	connected := t.connected
	pumping := t.pumping
	t.mu.RUnlock()

	if !connected || conn == nil {
		return nil, ErrNotConnected
	}
	if pumping {
		return nil, errors.New("cannot use ReadOnce while pump is running")
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	_, message, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Time{}) // Clear deadline

	return message, nil
}

// IsConnected returns true if the transport is connected
func (t *WSTransport) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.connected
}

// readPump continuously reads messages from WebSocket
func (t *WSTransport) readPump() {
	var disconnectErr error

	defer func() {
		t.mu.Lock()
		t.pumping = false
		t.mu.Unlock()
		t.handleDisconnect(disconnectErr)
	}()

	for {
		select {
		case <-t.done:
			return
		case <-t.stopChan:
			return
		default:
		}

		t.mu.RLock()
		conn := t.conn
		t.mu.RUnlock()

		if conn == nil {
			return
		}

		_ = conn.SetReadDeadline(time.Now().Add(t.config.ReadTimeout))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.logger.Info("WebSocket closed normally")
				return
			}
			t.logger.Error("WebSocket read error", zap.Error(err))
			disconnectErr = err
			return
		}

		if t.OnMessage != nil {
			t.OnMessage(message)
		}
	}
}

// heartbeat sends periodic ping messages
func (t *WSTransport) heartbeat() {
	ticker := time.NewTicker(t.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.mu.RLock()
			conn := t.conn
			connected := t.connected
			t.mu.RUnlock()

			if !connected || conn == nil {
				return
			}

			var err error
			if t.config.CustomPing != nil {
				err = t.config.CustomPing(conn)
			} else {
				err = conn.WriteMessage(websocket.PingMessage, nil)
			}
			if err != nil {
				t.logger.Error("Failed to send ping", zap.Error(err))
				return
			}
			t.logger.Debug("Sent ping")
		}
	}
}

// handleDisconnect handles disconnection and triggers reconnection
func (t *WSTransport) handleDisconnect(err error) {
	t.mu.Lock()
	wasConnected := t.connected
	t.connected = false
	t.mu.Unlock()

	if !wasConnected {
		return
	}

	t.logger.Warn("WebSocket disconnected", zap.Error(err))

	if t.OnDisconnected != nil {
		t.OnDisconnected(err)
	}

	go t.reconnect()
}

// reconnect attempts to reconnect with exponential backoff
func (t *WSTransport) reconnect() {
	t.mu.Lock()
	t.reconnectCount++
	count := t.reconnectCount
	stopped := t.stopped
	t.mu.Unlock()

	if stopped {
		return
	}

	if count > t.config.MaxReconnectAttempts {
		t.logger.Error("Max reconnection attempts reached",
			zap.Int("attempts", count),
			zap.Int("maxAttempts", t.config.MaxReconnectAttempts))
		return
	}

	delay := t.config.InitialReconnectDelay * time.Duration(1<<(count-1))
	t.logger.Info("Attempting reconnection",
		zap.Int("attempt", count),
		zap.Int("maxAttempts", t.config.MaxReconnectAttempts),
		zap.Duration("delay", delay))

	select {
	case <-time.After(delay):
	case <-t.stopChan:
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.config.HandshakeTimeout)
	defer cancel()

	// Use ConnectOnly so we can run OnBeforePump before starting the pump
	if err := t.ConnectOnly(ctx); err != nil {
		t.logger.Error("Reconnection failed",
			zap.Int("attempt", count),
			zap.Error(err))
		t.handleDisconnect(err)
		return
	}

	// Run pre-pump operations (e.g., authentication)
	if t.OnBeforePump != nil {
		if err := t.OnBeforePump(); err != nil {
			t.logger.Error("Pre-pump operation failed after reconnect",
				zap.Int("attempt", count),
				zap.Error(err))
			t.Disconnect()
			t.handleDisconnect(err)
			return
		}
	}

	t.StartPump()
}

// close safely closes a connection
func (t *WSTransport) close(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected = false
	t.pumping = false
	t.conn = nil
	if t.done != nil {
		close(t.done)
		t.done = nil
	}
}
