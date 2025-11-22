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

```bash
go build -o build/bot cmd/bot/main.go
./build/bot
```