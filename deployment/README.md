# Raspberry Pi Deployment

Deploy telegram-tori-bot to a Raspberry Pi as a systemd service.

## Overview

```
GitHub Releases                       Raspberry Pi
───────────────                       ──────────────────────────────
                                      /opt/telegram-tori-bot/
ARM64 binary ─────────────────────►     telegram-tori-bot (binary)
                                        start.sh (wrapper script)
Local Machine
─────────────
.env file ────────────────────────►     .env (configuration)

                                      /var/lib/telegram-tori-bot/
                                        sessions.db (SQLite database)

                                      /etc/systemd/system/
                                        telegram-tori-bot.service
```

## Prerequisites

**Local machine:**

- SSH access to the Pi
- curl (for downloading releases)

**Raspberry Pi:**

- Raspberry Pi OS or Ubuntu Server (ARM64)
- SSH server running
- User with sudo access

## Quick Start

```bash
# 1. First-time Pi setup (creates user, directories)
./deployment/setup-pi.sh pi@raspberrypi.local

# 2. Create .env file with your configuration
cp .env.example deployment/.env
# Edit deployment/.env with your values

# 3. Deploy
./deployment/deploy.sh pi@raspberrypi.local
```

## Configuration

Create a `.env` file in the `deployment/` directory with your configuration:

```bash
cp .env.example deployment/.env
```

Edit `deployment/.env` with your values:

```
BOT_TOKEN=your-telegram-bot-token
GEMINI_API_KEY=your-gemini-api-key
TORI_TOKEN_KEY=your-secret-passphrase
ADMIN_TELEGRAM_ID=your-telegram-user-id
TORI_DB_PATH=/var/lib/telegram-tori-bot/sessions.db
```

See the [main README](../README.md#configuration) for details on each variable.

## Scripts

### `setup-pi.sh` - Initial Pi Setup

Run **once** when setting up a new Pi. Creates:

- `tori` system user (runs the bot with minimal privileges)
- `/opt/telegram-tori-bot/` directory (application files)
- `/var/lib/telegram-tori-bot/` directory (database)

```bash
./deployment/setup-pi.sh pi@raspberrypi.local
```

### `deploy.sh` - Deploy Application

Run **every time** you want to deploy or update:

1. Downloads the latest release from GitHub
2. Copies binary, start.sh, .env, and service file to Pi
3. Restarts the systemd service

```bash
./deployment/deploy.sh pi@raspberrypi.local              # Full deploy
./deployment/deploy.sh pi@raspberrypi.local status       # Show service status
./deployment/deploy.sh pi@raspberrypi.local logs         # Show last 100 log lines
./deployment/deploy.sh pi@raspberrypi.local logs-f       # Follow logs (Ctrl+C to exit)
```

### `teardown-pi.sh` - Remove Installation

Completely removes telegram-tori-bot from the Pi:

- Stops and removes the systemd service
- Removes `/opt/telegram-tori-bot/` (binary, .env)
- Removes `/var/lib/telegram-tori-bot/` (database)
- Removes `tori` user

```bash
./deployment/teardown-pi.sh pi@raspberrypi.local
```

**Note:** Requires confirmation before proceeding.

## File Reference

| File                        | Purpose                                |
| --------------------------- | -------------------------------------- |
| `setup-pi.sh`               | Initial Pi setup (run once)            |
| `deploy.sh`                 | Build and deploy (run for each deploy) |
| `teardown-pi.sh`            | Remove installation from Pi            |
| `start.sh`                  | Wrapper script that loads .env         |
| `telegram-tori-bot.service` | systemd unit file                      |

## Managing the Service

From your local machine:

```bash
./deployment/deploy.sh pi@raspberrypi.local status
./deployment/deploy.sh pi@raspberrypi.local logs
./deployment/deploy.sh pi@raspberrypi.local logs-f
```

Or directly on the Pi:

```bash
sudo systemctl status telegram-tori-bot
sudo systemctl restart telegram-tori-bot
sudo systemctl stop telegram-tori-bot
sudo journalctl -u telegram-tori-bot -f    # Follow logs
```

## Troubleshooting

**Service won't start:**

```bash
# Check logs for errors
sudo journalctl -u telegram-tori-bot -n 50

# Verify .env exists and has correct permissions
ls -la /opt/telegram-tori-bot/.env
```

**Cannot connect to Pi:**

```bash
# Test SSH connection
ssh -v pi@raspberrypi.local

# Check Pi is on network
ping raspberrypi.local
```

**Login fails with reCAPTCHA error:**

The bot is running from an IP that Tori doesn't recognize. Log into Tori from
the Pi's IP address using a browser first, then try again.
