package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Load reads and validates configuration from the specified file path.
func Load(path string) (*Config, error) {
	// Create a new viper instance instead of using the global one
	// Reasons:
	//   1. Avoid global state pollution: multiple tests or concurrent loads won't interfere with each other
	//   2. Better encapsulation: each Load() call gets a clean viper instance
	//   3. Testability: loading different configs in tests won't conflict
	v := viper.New()

	// Set config file path
	v.SetConfigFile(path)

	// Set config type explicitly
	// Even though SetConfigFile() specifies the filename, viper needs to know
	// the format explicitly to select the correct parser
	v.SetConfigType("yaml")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal config into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}
