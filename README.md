# Grid Trading Bot

Automated grid trading bot for OKX ETHUSDT perpetual futures.

## Features

- Automated grid trading with neutral strategy
- Real-time market monitoring via Websocket
- Built-in risk management (stop-loss & exposure control)
- Terminal-based interface
- Emergency exit functionality

## Prerequisites

- Go 1.21+
- OKX account with API credentials
- golangci-lint (for development)

## Installation

```bash
# Clone the repository
git clone https://github.com/mickeyleetw/grid-trading-bot.git
cd grid-trading-bot

# Install dependencies
go mod download

# Install development tools
make install-tools
```

## Configuration

1. Copy the example config:
```bash
cp configs/config.example.yaml configs/config.yaml
```

2. Edit `configs/config.yaml` with your OKX API credentials:
```yaml
okx:
  api_key: "your-api-key"
  secret_key: "your-secret-key"
  passphrase: "your-passphrase"
  testnet: true  # Set to false for production
```

## Usage

### Development (go run)
```bash
# Run the bot
make run

# Emergency exit
make run-emergency
```

### Production (build binary)
```bash
# Build binary
make build

# Run the binary
./grid-trading-bot

# Emergency exit
./grid-trading-bot --emergency
```

## Development

```bash
# Run linter
make lint

# Run tests
make test

# Run tests with race detector
make test-race

# Setup git hooks
make setup-hooks
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed development guide.

## Project Structure

```
grid-trading-bot/
├── cmd/bot/           # Main application entry point
├── internal/
│   ├── config/        # Configuration management
│   ├── trading/       # Trading engine (grid, orders, risk)
│   └── okx/           # OKX API client (REST + Websocket)
├── configs/           # Configuration files
├── .golangci.yml      # Linter configuration
├── Makefile           # Development and build commands
└── README.md
```

**Note**:
- Development: Use `make run` (no binary created)
- Production: Use `make build` (binary output to root directory)

## Risk Warning

⚠️ **Trading cryptocurrencies carries significant risk. This bot is provided as-is without any warranty. Always test thoroughly on testnet before using real funds.**

## License

MIT License

## Contributing

Contributions are welcome! Please read [DEVELOPMENT.md](DEVELOPMENT.md) before submitting PRs.
