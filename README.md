# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Putting stuff for sale with this thing is actually a
joy. Takes advantage of Telegram's photo sending and bot features like Custom
Reply Keyboards and Inline Keyboards.

<video src="https://user-images.githubusercontent.com/11027/161634069-6462e726-bfe6-4340-8bec-1ae41a21ae6c.mp4"></video>

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

## Install

The Go Toolchain is required.

```sh
go install github.com/raine/telegram-tori-bot@latest
```

## Usage

1. Create a bot on Telegram by talking to [@botfather](https://t.me/botfather)
   and save the bot token it gives you.
   - https://core.telegram.org/bots#creating-a-new-bot
2. Get a Gemini API key from
   [Google AI Studio](https://aistudio.google.com/apikey)
3. Run `telegram-tori-bot` with the required environment variables:

   ```sh
   BOT_TOKEN=<bot_token_from_step_1> \
   GEMINI_API_KEY=<your_gemini_api_key> \
   TORI_TOKEN_KEY=<any_secret_passphrase> \
   ADMIN_TELEGRAM_ID=<your_telegram_user_id> \
      telegram-tori-bot
   ```

4. Search for your bot in Telegram with the username you gave to it.
5. `/start` a conversation with the bot, then use `/login` to authenticate with
   your Tori account.
6. Send a photo of the item you want to sell.

## Environment variables

- `BOT_TOKEN`: Telegram bot's token. You get this from @botfather. **required**
- `GEMINI_API_KEY`: Google Gemini API key for vision analysis and LLM features.
  **required**
- `TORI_TOKEN_KEY`: Secret passphrase used to encrypt stored Tori authentication
  tokens. Can be any string. **required**
- `ADMIN_TELEGRAM_ID`: Your Telegram user ID. The admin can add/remove users
  allowed to use the bot. **required**
- `TORI_DB_PATH`: Path to SQLite database file. Defaults to `sessions.db`.
  optional

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

The project uses [`just`](https://github.com/casey/just) as a command runner (or
make alternative).

See `just -l` for recipes.
