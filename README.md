# telegram-tori-bot

Telegram bot created with the intent of making selling stuff on tori.fi as
streamlined as possible. Putting stuff for sale with this thing is actually a
joy. Takes advantage of Telegram's photo sending and bot features like Custom
Reply Keyboards and Inline Keyboards.

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

1. Create a bot on Telegram by talking to [@botfather](https://t.me/botfather)
   - https://core.telegram.org/bots#creating-a-new-bot
2. Run `telegram-tori-bot` with env variables and [user config](#env-vars) set
   up:

   ```sh
   BOT_TOKEN=bottoken \
   USER_CONFIG_PATH=path/to/user_config.toml \
      telegram-tori-bot
   ```

3. Tell your bot what you want to sell

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
session. It will work as long as the cookie is readable in JS.

```js
const { access_token, token_type, account_id } = JSON.parse(
  atob(
    Object.fromEntries(
      document.cookie.split('; ').map((v) => v.split(/=(.*)/s))
    )['sessioninfo']
  )
)
console.log(
  `token = '${token_type} ${access_token}'\ntoriAccountId = '${
    account_id.split('/')[3]
  }'`
)
```

## faq

### how do i start over when making some kind of mistake that cannot be reversed?

Use the command `/peru`. It will forget everything from the current listing
creation. Note that subject and description can be edited by editing the
original message.

### which of the uploaded photos will be used as primary picture in listing?

The first uploaded picture. When uploading multiple photos in Telegram client,
photos can be reorded both on desktop and mobile. Or you can just upload them
separately -- that way works also.

### bot is failing to guess the correct category based on listing subject, what do?

The bot comes up with a subject for your listing based on the subject you enter.
This works by searching tori.fi for existing listings with parts of the entered
subject. If none can be found, there is currently no fallback to select the
category manually. What you can do, however, is enter a noun as the last word in
the subject. The bot will always search the last word of the subject
independently if all else fails.

For example,

**Good**: Lake CX 176 maantiekengät

Searching for "maantiekengät" is almost guaranteed to provide a result that we
can get the correct category from.

**Bad**: Lake CX 176

If no one is currently selling Lake CX 176 road cycling shoes, we can't get a
category.

### does it add a phone number to listing?

No. Adding phone number to listing is an invitation for annoying Whatsapp scam
messages.

## development

The project uses [`just`](https://github.com/casey/just) as a command runner (or
make alternative).

See `just -l` for recipes.
