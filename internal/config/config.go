// Package config provides configuration management for the Grid Trading Bot.
package config

// Config represents the application configuration structure.
type Config struct {
	OKX      OKXConfig
	Telegram TelegramConfig
	Server   ServerConfig
}

// OKXConfig contains OKX API credentials and settings.
type OKXConfig struct {
	APIKey     string
	SecretKey  string
	Passphrase string
	TestNet    bool
}

// TelegramConfig contains Telegram bot settings.
type TelegramConfig struct {
	Enabled bool
	Token   string
	ChatID  int64 // int64 is required because Telegram Chat IDs can be large negative numbers (e.g., -1001234567890 for group chats)
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port     int
	APIToken string
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// OKX validation
	if c.OKX.APIKey == "" {
		return ErrMissingOKXAPIKey
	}
	if c.OKX.SecretKey == "" {
		return ErrMissingOKXSecretKey
	}
	if c.OKX.Passphrase == "" {
		return ErrMissingOKXPassphrase
	}

	// Telegram validation
	if c.Telegram.Enabled {
		if c.Telegram.Token == "" {
			return ErrMissingTelegramToken
		}
		if c.Telegram.ChatID == 0 {
			return ErrMissingTelegramChatID
		}
	}

	// Server validation
	if c.Server.Port < 1024 || c.Server.Port > 65535 {
		return ErrInvalidServerPort
	}
	if c.Server.APIToken == "" {
		return ErrMissingAPIToken
	}

	return nil
}
