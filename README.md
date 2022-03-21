# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Takes advantage of Telegram's photo sending and bot
features like Custom Reply Keyboards and Inline Keyboards.

## usage

1. Compile the binary with `go build .`
2. Run the program with env variables and user config set up
3. Tell your bot what you want to sell

### env vars

- `BOT_TOKEN`: Telegram bot's token. You get this from @botfather. **required**
- `USER_CONFIG_PATH`: Path to user config. See `user_config.json.example` for an
  example. **required**

## development

The Go Toolchain is required for development.

The project uses [`just`](https://github.com/casey/just) as a command runner (or
make alternative).

See `just -l` for recipes.
