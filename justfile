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
    go build -ldflags "-X main.Version=$(git rev-parse --short HEAD) -X 'main.BuildTime=$(date -u '+%Y-%m-%d %H:%M:%S')'" .

# Run tests
test +FLAGS='./...':
    richgo test {{FLAGS}}

# Run the application
run *ARGS:
    go run . "$@"

# Watch and run on changes
run-w:
    fd .go | entr -r go run .

# Watch and test on changes
test-w +FLAGS='./...':
    fd .go | entr richgo test {{FLAGS}}
