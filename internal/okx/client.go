package okx

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	productionBaseURL = "https://www.okx.com"
	testnetBaseURL    = "https://www.okx.com"

	priceCacheDuration   = 5 * time.Second
	accountCacheDuration = 10 * time.Second

	maxRetries = 1
	retryDelay = 500 * time.Millisecond
)

type Client struct {
	apiKey     string
	secretKey  string
	passphrase string
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
	cache      cache
}

func NewClient(apiKey, secretKey, passphrase string, testNet bool, logger *zap.Logger) *Client {
	baseURL := productionBaseURL
	if testNet {
		baseURL = testnetBaseURL
	}

	return &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		baseURL:    baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) GetAPIKey() string {
	return c.apiKey
}

func (c *Client) GetBaseURL() string {
	return c.baseURL
}
