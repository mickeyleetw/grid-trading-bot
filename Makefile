.PHONY: help lint lint-fix test test-race test-coverage run run-emergency clean install-tools pre-commit

# Default target
help:
	@echo "Available targets:"
	@echo "  make run            - Run the bot"
	@echo "  make run-emergency  - Run emergency exit"
	@echo "  make lint           - Run golangci-lint"
	@echo "  make lint-fix       - Run golangci-lint with auto-fix"
	@echo "  make test           - Run tests"
	@echo "  make test-race      - Run tests with race detector"
	@echo "  make test-coverage  - Run tests with coverage"
	@echo "  make clean          - Clean artifacts"
	@echo "  make install-tools  - Install development tools"
	@echo "  make pre-commit     - Run pre-commit checks"

# Install development tools
install-tools:
	@echo "Installing golangci-lint..."
	@which golangci-lint > /dev/null || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
	@echo "Tools installed successfully"

# Linting
lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

# Testing
test:
	go test -v ./...

test-race:
	go test -race -short ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run
run:
	go run ./cmd/bot

run-emergency:
	go run ./cmd/bot --emergency

# Clean
clean:
	rm -f coverage.out coverage.html

# Pre-commit checks (run before committing)
pre-commit: lint test-race
	@echo "Pre-commit checks passed!"

# Setup git hooks
setup-hooks:
	@echo "Setting up git hooks..."
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'make pre-commit' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Git hooks installed"
