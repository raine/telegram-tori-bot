#!/usr/bin/env bash
set -euo pipefail

# Teardown script for Raspberry Pi
# Removes telegram-tori-bot installation (inverse of setup-pi.sh)
#
# Usage: ./teardown-pi.sh <PI_HOST>
# Example: ./teardown-pi.sh pi@raspberrypi.local

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <PI_HOST>"
    echo "Example: $0 pi@raspberrypi.local"
    exit 1
fi

PI_HOST="$1"
APP_NAME="telegram-tori-bot"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

warn "This will remove telegram-tori-bot from $PI_HOST"
warn "Including: service, binary, .env, and tori user"
echo ""
read -p "Are you sure? Type 'yes' to confirm: " -r
if [[ "$REPLY" != "yes" ]]; then
    error "Aborted"
    exit 1
fi

echo ""
read -p "Also delete database (sessions.db)? [y/N] " -n 1 -r DELETE_DATA
echo ""

# Check if Pi is reachable
if ! ssh -o ConnectTimeout=5 "$PI_HOST" true 2>/dev/null; then
    error "Cannot connect to $PI_HOST"
    exit 1
fi

info "Tearing down $APP_NAME on $PI_HOST..."

ssh -T "$PI_HOST" bash -s -- "$DELETE_DATA" <<'ENDSSH'
set -euo pipefail

DELETE_DATA="$1"
APP_NAME="telegram-tori-bot"
INSTALL_DIR="/opt/$APP_NAME"
DATA_DIR="/var/lib/$APP_NAME"

# Safety check
if [[ -z "$INSTALL_DIR" || "$INSTALL_DIR" == "/" || -z "$DATA_DIR" || "$DATA_DIR" == "/" ]]; then
    echo "[ERROR] Safety check failed - refusing to delete"
    exit 1
fi

echo "[INFO] Stopping and disabling service..."
if systemctl is-active --quiet "$APP_NAME" 2>/dev/null; then
    sudo systemctl stop "$APP_NAME"
fi
if systemctl is-enabled --quiet "$APP_NAME" 2>/dev/null; then
    sudo systemctl disable "$APP_NAME"
fi

echo "[INFO] Removing systemd service file..."
sudo rm -f "/etc/systemd/system/$APP_NAME.service"
sudo systemctl daemon-reload

echo "[INFO] Removing install directory ($INSTALL_DIR)..."
sudo rm -rf "$INSTALL_DIR"

if [[ "$DELETE_DATA" =~ ^[Yy]$ ]]; then
    echo "[INFO] Removing data directory ($DATA_DIR)..."
    sudo rm -rf "$DATA_DIR"
else
    echo "[INFO] Keeping data directory ($DATA_DIR)"
fi

echo "[INFO] Removing tori user..."
if id -u tori &>/dev/null; then
    sudo userdel tori
    echo "[INFO] Removed tori user"
else
    echo "[INFO] tori user does not exist"
fi

echo ""
echo "====================================="
echo "Teardown complete!"
echo "====================================="
ENDSSH

info "Teardown complete!"
