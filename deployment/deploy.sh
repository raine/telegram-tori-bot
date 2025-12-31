#!/usr/bin/env bash
set -euo pipefail

# Deployment script for telegram-tori-bot to Raspberry Pi
# Downloads the latest release and deploys to the Pi via SSH
#
# Usage: ./deploy.sh <PI_HOST> [command]
# Example: ./deploy.sh pi@raspberrypi.local

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

APP_NAME="telegram-tori-bot"
GITHUB_REPO="raine/telegram-tori-bot"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

usage() {
    echo "Usage: $0 <PI_HOST> [command]"
    echo ""
    echo "Commands:"
    echo "  deploy   Download latest release and deploy to Pi (default)"
    echo "  logs     Show recent logs from Pi"
    echo "  logs-f   Follow logs in real-time (Ctrl+C to exit)"
    echo "  status   Show service status on Pi"
    echo ""
    echo "Example: $0 pi@raspberrypi.local"
    exit 1
}

if [[ $# -lt 1 ]]; then
    usage
fi

PI_HOST="$1"
COMMAND="${2:-deploy}"

# Check remote architecture and download the latest release binary
download_binary() {
    info "Checking remote architecture..."
    if ! ARCH=$(ssh -o ConnectTimeout=5 "$PI_HOST" "uname -m"); then
        error "Cannot connect to $PI_HOST"
        exit 1
    fi

    case "$ARCH" in
        aarch64)
            BINARY_NAME="telegram-tori-bot-linux-arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH (only ARM64 is supported)"
            exit 1
            ;;
    esac
    info "Detected $ARCH, using $BINARY_NAME"

    info "Downloading latest $APP_NAME release..."
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/latest/download/$BINARY_NAME"

    if ! curl -fSL "$DOWNLOAD_URL" -o "$SCRIPT_DIR/$APP_NAME"; then
        error "Failed to download from $DOWNLOAD_URL"
        exit 1
    fi

    chmod +x "$SCRIPT_DIR/$APP_NAME"
    info "Downloaded: $SCRIPT_DIR/$APP_NAME"
    ls -lh "$SCRIPT_DIR/$APP_NAME"
}

# Deploy to the Pi
deploy() {
    info "Deploying to $PI_HOST..."

    # Check if .env exists
    if [[ ! -f "$SCRIPT_DIR/.env" ]]; then
        error ".env file not found in $SCRIPT_DIR"
        error "Copy .env.example to deployment/.env and fill in your values"
        exit 1
    fi

    # Check if Pi is reachable
    if ! ssh -o ConnectTimeout=5 "$PI_HOST" true 2>/dev/null; then
        error "Cannot connect to $PI_HOST"
        exit 1
    fi

    # Copy files to Pi
    info "Copying files..."
    scp "$SCRIPT_DIR/$APP_NAME" "$PI_HOST:/tmp/"
    scp "$SCRIPT_DIR/telegram-tori-bot.service" "$PI_HOST:/tmp/"
    scp "$SCRIPT_DIR/start.sh" "$PI_HOST:/tmp/"

    # Transfer .env via pipe to avoid shell expansion issues with special characters
    cat "$SCRIPT_DIR/.env" | ssh "$PI_HOST" "cat > /tmp/tori-env-tmp && chmod 600 /tmp/tori-env-tmp"

    # Install and restart service
    info "Installing service on Pi..."
    ssh -T "$PI_HOST" bash -s <<'ENDSSH'
set -euo pipefail

# Ensure target directory exists
sudo mkdir -p /opt/telegram-tori-bot
sudo chown tori:tori /opt/telegram-tori-bot

# Stop service first to release file handles
if systemctl is-active --quiet telegram-tori-bot; then
    echo "Stopping service..."
    sudo systemctl stop telegram-tori-bot
fi

# Install files with correct permissions
sudo install -m 755 -o tori -g tori /tmp/telegram-tori-bot /opt/telegram-tori-bot/telegram-tori-bot
sudo install -m 755 -o tori -g tori /tmp/start.sh /opt/telegram-tori-bot/start.sh
sudo install -m 644 /tmp/telegram-tori-bot.service /etc/systemd/system/telegram-tori-bot.service
sudo install -m 600 -o tori -g tori /tmp/tori-env-tmp /opt/telegram-tori-bot/.env

# Clean up temp files
rm -f /tmp/telegram-tori-bot /tmp/start.sh /tmp/telegram-tori-bot.service /tmp/tori-env-tmp

sudo systemctl daemon-reload
sudo systemctl enable telegram-tori-bot

# Start the service
echo "Starting service..."
sudo systemctl start telegram-tori-bot

# Wait for service to start and verify
sleep 2
if systemctl is-active --quiet telegram-tori-bot; then
    echo "Service is running!"
    sudo systemctl status telegram-tori-bot --no-pager
else
    echo "Service failed to start. Check logs with: sudo journalctl -u telegram-tori-bot -n 50"
    sudo systemctl status telegram-tori-bot --no-pager || true
    exit 1
fi
ENDSSH

    info "Deployment complete!"
}

# Show logs from the Pi
show_logs() {
    ssh -t "$PI_HOST" "sudo journalctl -u telegram-tori-bot -n 100 --no-pager"
}

# Follow logs in real-time
follow_logs() {
    ssh -t "$PI_HOST" "sudo journalctl -u telegram-tori-bot -f"
}

# Show service status
show_status() {
    ssh "$PI_HOST" "sudo systemctl status telegram-tori-bot --no-pager" || true
}

# Main
case "$COMMAND" in
    deploy)
        download_binary
        deploy
        rm -f "$SCRIPT_DIR/$APP_NAME"
        ;;
    logs)
        show_logs
        ;;
    logs-f)
        follow_logs
        ;;
    status)
        show_status
        ;;
    *)
        usage
        ;;
esac
