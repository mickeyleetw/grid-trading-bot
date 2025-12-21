package okx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// generateSignature generates HMAC-SHA256 signature for OKX API
func (c *Client) generateSignature(timestamp, method, requestPath, body string) string {
	message := timestamp + method + requestPath + body
	mac := hmac.New(sha256.New, []byte(c.secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (c *Client) getTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.999Z")
}

func (c *Client) addAuthHeaders(method, requestPath, body string, headers map[string]string) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}

	timestamp := c.getTimestamp()
	signature := c.generateSignature(timestamp, method, requestPath, body)

	headers["OK-ACCESS-KEY"] = c.apiKey
	headers["OK-ACCESS-SIGN"] = signature
	headers["OK-ACCESS-TIMESTAMP"] = timestamp
	headers["OK-ACCESS-PASSPHRASE"] = c.passphrase
	headers["Content-Type"] = "application/json"

	if c.simulated {
		headers["x-simulated-trading"] = "1"
	}

	return headers
}

func (c *Client) validateAPIResponse(code, msg string) error {
	if code != "0" {
		return fmt.Errorf("OKX API error: code=%s, msg=%s", code, msg)
	}
	return nil
}
