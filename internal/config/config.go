package config

type Config struct {
	App      AppConfig
	OKX      OKXConfig
	Telegram TelegramConfig
	Server   ServerConfig
}

type AppConfig struct {
	Environment string
}

type OKXConfig struct {
	APIKey     string
	SecretKey  string
	Passphrase string
	TestNet    bool
}

type TelegramConfig struct {
	Enabled bool
	Token   string
	ChatID  int64
}

type ServerConfig struct {
	Port     int
	APIToken string
}
