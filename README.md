# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Putting stuff for sale with this thing is actually a
joy. Takes advantage of Telegram's photo sending and bot features like Custom
Reply Keyboards and Inline Keyboards.

## Features

### Vision-based listing creation

Send a photo and the bot uses Gemini Vision API to automatically generate a
title and description for your listing. Multiple photos (albums) are analyzed
together for better context.

### AI-powered automation

- **Auto-category selection**: LLM automatically selects the most appropriate
  category based on the item
- **Auto-attribute selection**: Category-specific attributes (size, color,
  condition, etc.) are automatically filled in
- **Price recommendations**: Shows prices of similar listings to help you price
  your item competitively

### Natural language editing

Edit your listing draft by typing in Finnish, e.g., "vaihda hinnaksi 40e" or
"lisää että koiran taloudesta". The bot understands and applies the changes.

### Bulk listing mode

Create multiple listings at once using `/era`. Send photos (single photos create
separate drafts, albums create one draft per album), then use `/valmis` to
review and edit all drafts before publishing.

### Giveaway mode

List items for free by selecting the "Annetaan" button when prompted for price.
The description is automatically rewritten to use "Annetaan" language.

### Description templates

Save a description template with `/malli` that gets appended to all your
listings. Supports conditional text for shipping:

```
/malli Nouto Kannelmäestä{{#if shipping}} tai postitus{{/end}}. Mobilepay/käteinen.
```

### Other features

- **Built-in login flow**: Login directly through the bot with `/login` (email
  - verification code)
- **Postal code management**: Set your postal code once with `/postinumero` and
  it's remembered for all listings
- **Category re-selection**: Use `/osasto` to change the category if
  auto-selection was wrong
- **Vision analysis caching**: Photo analysis results are cached in SQLite to
  avoid re-analyzing the same photos

## Quick Start

### Prerequisites

- **Go 1.25+**: [Install Go](https://go.dev/doc/install)

### Installation

```sh
go install github.com/raine/telegram-tori-bot@latest
```

### Setup

1. **Create a Telegram bot**
   - Message [@BotFather](https://t.me/botfather) on Telegram
   - Send `/newbot` and follow the prompts
   - Save the bot token you receive

2. **Get a Gemini API key**
   - Visit [Google AI Studio](https://aistudio.google.com/apikey)
   - Create a new API key

3. **Find your Telegram user ID**
   - Message [@userinfobot](https://t.me/userinfobot) on Telegram
   - It will reply with your user ID

4. **Configure environment variables**

   ```sh
   export BOT_TOKEN="your_bot_token"
   export GEMINI_API_KEY="your_gemini_key"
   export TORI_TOKEN_KEY="any_secret_passphrase"
   export ADMIN_TELEGRAM_ID="123456789"
   ```

5. **Run the bot**

   ```sh
   telegram-tori-bot
   ```

6. **Start using the bot**
   - Find your bot on Telegram by its username
   - Send `/start`, then `/login` to connect your Tori account
   - Send a photo of something you want to sell

## Environment Variables

| Variable            | Required | Description                                       |
| ------------------- | -------- | ------------------------------------------------- |
| `BOT_TOKEN`         | Yes      | Telegram bot token from @BotFather                |
| `GEMINI_API_KEY`    | Yes      | Google Gemini API key for vision/LLM features     |
| `TORI_TOKEN_KEY`    | Yes      | Secret passphrase for encrypting Tori auth tokens |
| `ADMIN_TELEGRAM_ID` | Yes      | Your Telegram user ID (becomes admin)             |
| `TORI_DB_PATH`      | No       | SQLite database path (default: `sessions.db`)     |

## Deployment

Tori's login uses reCAPTCHA validation based on IP reputation. The bot must run
from an IP address where you have previously logged into Tori via browser or the
official app. Untrusted IPs will fail with "reCaptcha was invalid" errors during
login. A Raspberry Pi on your home network is an easy option since you likely
already use Tori from that IP.

## User access control

The bot uses a whitelist system. Only the admin (specified by
`ADMIN_TELEGRAM_ID`) and explicitly allowed users can interact with the bot.
Unauthorized users receive no response.

### Admin commands

The admin can manage allowed users with these commands (not shown in bot menu):

- `/admin users add <user_id>` - Add a user to the whitelist
- `/admin users remove <user_id>` - Remove a user from the whitelist
- `/admin users list` - List all allowed users

## Commands

| Command        | Description                         |
| -------------- | ----------------------------------- |
| `/login`       | Login to your Tori account          |
| `/peru`        | Cancel current listing creation     |
| `/laheta`      | Publish the listing                 |
| `/era`         | Enter bulk mode (multiple listings) |
| `/valmis`      | Finish adding photos in bulk mode   |
| `/poistakuvat` | Remove listing photos               |
| `/osasto`      | Change category                     |
| `/malli`       | View or set description template    |
| `/poistamalli` | Remove description template         |
| `/postinumero` | View or change postal code          |

## FAQ

### How do I start over when making some kind of mistake that cannot be reversed?

Use the command `/peru`. It will forget everything from the current listing
creation and delete any draft created on Tori.

### Which of the uploaded photos will be used as primary picture in listing?

The first uploaded picture. When uploading multiple photos in Telegram client,
photos can be reorded both on desktop and mobile. Or you can just upload them
separately -- that way works also.

### Does it add a phone number to listing?

No. Adding phone number to listing is an invitation for annoying Whatsapp scam
messages.

## Development

The project uses [`just`](https://github.com/casey/just) as a command runner.

**Install just:** `brew install just` (Mac) | `scoop install just` (Windows) |
[other options](https://github.com/casey/just#installation)

```sh
git clone https://github.com/raine/telegram-tori-bot.git
cd telegram-tori-bot
just build    # Build the project
just check    # Run format, vet, build, and tests
just test     # Run tests only
just run      # Run the bot
```

Run `just -l` to see all available commands.
