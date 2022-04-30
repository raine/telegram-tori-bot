# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Takes advantage of Telegram's photo sending and bot
features like Custom Reply Keyboards and Inline Keyboards.

<video src="https://user-images.githubusercontent.com/11027/161634069-6462e726-bfe6-4340-8bec-1ae41a21ae6c.mp4"></video>

## features

- Determines the listing category from subject, instead of having to browse
  through endless list of nested categories
- Add photos to listing by dragging them to chat at any point
- Edit listing subject and body by editing the original message

## install

The Go Toolchain is required.

```sh
go install github.com/raine/telegram-tori-bot@latest
```

## usage

1. Run `telegram-tori-bot` with env variables and user config set up
2. Tell your bot what you want to sell

## env vars

- `BOT_TOKEN`: Telegram bot's token. You get this from @botfather. **required**
- `USER_CONFIG_PATH`: Path to user config. See `user_config.toml.example` for an
  example. If your telegram user id is not found in the user config, the bot
  will disregard your message. **required**

## user config

No login mechanism with tori.fi (or whatever schibsted it is thesedays)
credentials is implemented as of yet.

Here's a JavaScript snippet to get the access token and tori account id for
`user_config.toml`. Run it in browser developer tools on tori.fi with active
session. It will works as long as the cookie is readable in JS.

```js
const { access_token, token_type, account_id } = JSON.parse(
  atob(
    Object.fromEntries(
      document.cookie
        .split('; ')
        .map((v) => v.split(/=(.*)/s).map(decodeURIComponent))
    )['sessioninfo']
  )
)
console.log(
  `token = '${token_type} ${access_token}'\ntoriAccountId = '${
    account_id.split('/')[3]
  }'`
)
```

## development

The project uses [`just`](https://github.com/casey/just) as a command runner (or
make alternative).

See `just -l` for recipes.
