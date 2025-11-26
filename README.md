# Grid Trading Bot

## Installation

```bash
go mod tidy
```

## Configuration

1. Copy the example configuration file:
   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

2. Edit `configs/config.yaml` with your settings:

   - `app.environment`: Environment mode (`production` / `testing` / `development`)
   - `okx.*`: OKX API credentials and settings
   - `telegram.*`: Telegram bot configuration
   - `server.*`: HTTP server settings

**Note:** `config.yaml` contains sensitive information and is excluded from version control.

## Usage

```bash
go build -o build/bot cmd/bot/main.go
./build/bot
```

## Project Structure

```
grid-trading-bot/
├── cmd/
│   └── bot/              # Main application entry point
├── internal/
│   ├── config/           # Configuration management
│   ├── logger/           # Logging utilities
│   └── okx/              # OKX REST API client
├── configs/              # Configuration files
└── ui/                   # HTTP server and handlers
```

### Key Components

- **OKX Client** (`internal/okx/`): REST API client for OKX exchange
  - API authentication (HMAC-SHA256)
  - Account queries (balance, positions)
  - Order operations (place, cancel, query)
  - Market data (ticker prices)
  - Built-in caching mechanism
  - Error handling and retry logic

## Development

### Code Quality Checks

Before committing code, run linting checks (configured in `.golangci.yml`):

```bash
# Install golangci-lint (if not already installed)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run all linters
golangci-lint run

# Run with auto-fix for some issues
golangci-lint run --fix
```