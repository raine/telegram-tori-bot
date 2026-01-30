# Go project checks

set positional-arguments
set shell := ["bash", "-euo", "pipefail", "-c"]

# List available commands
default:
    @just --list

# Run format, vet, build, and tests
check: format vet build test

# Format Go files
format:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Build the project
build:
    go build -ldflags "-X github.com/raine/telegram-tori-bot/internal/bot.Version=$(git rev-parse --short HEAD) -X 'github.com/raine/telegram-tori-bot/internal/bot.BuildTime=$(date -u '+%Y-%m-%d %H:%M:%S')'" .

# Run tests
test +FLAGS='./...':
    richgo test {{FLAGS}}

# Watch and run on changes
dev:
    fd .go | entr -r go run .

# Watch and test on changes
test-w +FLAGS='./...':
    fd .go | entr richgo test {{FLAGS}}
