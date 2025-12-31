#!/bin/sh
set -e

# Wrapper script that loads environment variables and starts the bot
# This script is run by systemd

ENV_FILE="/opt/telegram-tori-bot/.env"

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: Environment file not found at $ENV_FILE" >&2
    exit 1
fi

# Export all variables from .env file
set -a
. "$ENV_FILE"
set +a

exec /opt/telegram-tori-bot/telegram-tori-bot
