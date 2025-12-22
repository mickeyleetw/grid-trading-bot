package okx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"time"
)

// generateWSSignature generates HMAC-SHA256 signature for WebSocket login
// The signature is calculated as: base64(hmac-sha256(timestamp + "GET" + "/users/self/verify"))
func generateWSSignature(timestamp string, secretKey string) string {
	message := timestamp + "GET" + "/users/self/verify"
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// getWSTimestamp returns Unix timestamp in seconds as string
func getWSTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// buildLoginRequest creates a login request for private channel authentication
func buildLoginRequest(apiKey, secretKey, passphrase string) *WSRequest {
	timestamp := getWSTimestamp()
	sign := generateWSSignature(timestamp, secretKey)

	return &WSRequest{
		Op: WSOpLogin,
		Args: []any{
			WSLoginArg{
				APIKey:     apiKey,
				Passphrase: passphrase,
				Timestamp:  timestamp,
				Sign:       sign,
			},
		},
	}
}

// buildSubscribeRequest creates a subscription request
func buildSubscribeRequest(channel, instID string) *WSRequest {
	arg := WSSubscribeArg{
		Channel: channel,
		InstID:  instID,
	}

	return &WSRequest{
		Op:   WSOpSubscribe,
		Args: []any{arg},
	}
}
