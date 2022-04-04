# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Takes advantage of Telegram's photo sending and bot
features like Custom Reply Keyboards and Inline Keyboards.

<video src="https://user-images.githubusercontent.com/11027/161634069-6462e726-bfe6-4340-8bec-1ae41a21ae6c.mp4"></video>

## install

```sh
go install github.com/raine/telegram-tori-bot
```

## usage

1. Run `telegram-tori-bot` with env variables and user config set up
2. Tell your bot what you want to sell

### env vars

- `BOT_TOKEN`: Telegram bot's token. You get this from @botfather. **required**
- `USER_CONFIG_PATH`: Path to user config. See `user_config.json.example` for an
  example. If your telegram user id is not found in the user config, the bot
  will disregard your message. **required**

## development

The Go Toolchain is required for development.

The project uses [`just`](https://github.com/casey/just) as a command runner (or
make alternative).

See `just -l` for recipes.
