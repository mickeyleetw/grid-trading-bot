# Grid Trading Bot

## Installation

```bash
go mod tidy
```

## Configuration

1. Copy the example config:

  ```bash
  cp configs/config.example.yaml configs/config.yaml
  ```

2. Edit `configs/config.yaml` to your customized version

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
