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
"lisää että koirataloudesta". The bot understands and applies the changes.

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
  verification code)

## Quick start

1. **Download** the latest release for your platform from
   [Releases](https://github.com/raine/telegram-tori-bot/releases)

2. **Run** the downloaded file (double-click or run from terminal)

3. **Follow the setup wizard** - it will guide you through:
   - Creating a Telegram bot via @BotFather
   - Getting a Gemini API key
   - Finding your Telegram user ID

4. **Start using the bot**
   - Find your bot on Telegram by its username
   - Send `/start`, then `/login` to connect your Tori account
   - Send a photo of something you want to sell

### Windows users

Windows may show a "Windows protected your PC" warning for unsigned executables.
Click "More info" then "Run anyway" to proceed.

### Alternative: Install with Go

If you have Go installed, you can also install via:

```sh
go install github.com/raine/telegram-tori-bot@latest
```

## LLM costs

The bot uses Google's Gemini API for vision and text processing. The free tier
may work fine (10 RPM, 250 requests/day). Costs on paid tier are minimal - a
typical listing creation costs well under $0.01 USD:

```
INF image(s) analyzed cost=0.0008925 imageCount=1 title="LUMI Recovery Pod kylmäallas"
INF category selection llm call costUSD=0.000019725 inputTokens=223 model=gemini-2.5-flash-lite outputTokens=10
```

## Configuration

The setup wizard automatically creates a `.env` file with your configuration.
You can also set these as environment variables:

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
