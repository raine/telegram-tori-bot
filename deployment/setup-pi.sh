#!/usr/bin/env bash
set -euo pipefail

# Initial setup script for Raspberry Pi
# Run this once to prepare the Pi for telegram-tori-bot deployment
#
# Usage: ./setup-pi.sh <PI_HOST>
# Example: ./setup-pi.sh pi@raspberrypi.local

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <PI_HOST>"
    echo "Example: $0 pi@raspberrypi.local"
    exit 1
fi

PI_HOST="$1"
APP_NAME="telegram-tori-bot"
INSTALL_DIR="/opt/$APP_NAME"
DATA_DIR="/var/lib/$APP_NAME"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

info "Setting up $APP_NAME on $PI_HOST..."

# Check if Pi is reachable
if ! ssh -o ConnectTimeout=5 "$PI_HOST" true 2>/dev/null; then
    error "Cannot connect to $PI_HOST"
    exit 1
fi

# Run setup on the Pi
ssh -T "$PI_HOST" bash -s <<'ENDSSH'
set -euo pipefail

APP_NAME="telegram-tori-bot"
INSTALL_DIR="/opt/$APP_NAME"
DATA_DIR="/var/lib/$APP_NAME"

echo "[INFO] Creating tori user..."
if ! id -u tori &>/dev/null; then
    sudo useradd --system --shell /usr/sbin/nologin --home-dir "$DATA_DIR" tori
    echo "[INFO] Created tori user"
else
    echo "[INFO] tori user already exists"
fi

echo "[INFO] Creating directories..."
sudo mkdir -p "$INSTALL_DIR"
sudo mkdir -p "$DATA_DIR"

echo "[INFO] Setting permissions..."
sudo chown -R tori:tori "$INSTALL_DIR"
sudo chown -R tori:tori "$DATA_DIR"
sudo chmod 750 "$INSTALL_DIR"
sudo chmod 750 "$DATA_DIR"

echo ""
echo "====================================="
echo "Setup complete!"
echo "====================================="
echo ""
echo "Next steps:"
echo "1. Create deployment/.env with your configuration"
echo "2. Run: ./deployment/deploy.sh $USER@$(hostname -I | awk '{print $1}')"
echo ""
ENDSSH

info "Pi setup complete!"
info ""
info "Next steps:"
info "1. Create deployment/.env with your configuration"
info "2. Run: ./deployment/deploy.sh $PI_HOST"
