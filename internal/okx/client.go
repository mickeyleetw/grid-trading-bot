package okx

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

type Client struct {
	apiKey     string
	secretKey  string
	passphrase string
	simulated  bool // if true, uses simulated trading environment (x-simulated-trading: 1)
	httpClient *http.Client
	logger     *zap.Logger
}

func NewClient(apiKey, secretKey, passphrase string, simulated bool, logger *zap.Logger) *Client {
	return &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		simulated:  simulated,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

