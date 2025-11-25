package config

import "errors"

var (
	// ErrMissingOKXAPIKey indicates that OKX API key is missing.
	ErrMissingOKXAPIKey = errors.New("OKX API key is required")

	// ErrMissingOKXSecretKey indicates that OKX secret key is missing.
	ErrMissingOKXSecretKey = errors.New("OKX secret key is required")

	// ErrMissingOKXPassphrase indicates that OKX passphrase is missing.
	ErrMissingOKXPassphrase = errors.New("OKX passphrase is required")

	// ErrInvalidServerPort indicates that server port is out of valid range.
	ErrInvalidServerPort = errors.New("server port must be between 1024 and 65535")

	// ErrMissingAPIToken indicates that API token is missing.
	ErrMissingAPIToken = errors.New("API token is required")

	// ErrMissingTelegramToken indicates that Telegram bot token is missing when Telegram is enabled.
	ErrMissingTelegramToken = errors.New("Telegram token is required when Telegram is enabled")

	// ErrMissingTelegramChatID indicates that Telegram chat ID is missing when Telegram is enabled.
	ErrMissingTelegramChatID = errors.New("Telegram chat ID is required when Telegram is enabled")
)
