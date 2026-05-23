# Spoiler Cleaner Discord Bot

A small Go Discord bot that deletes messages from one configured user when they contain a spoilered image attachment, and deletes messages from non-ignored users when content or embed metadata matches configured regex rules.

Discord marks spoilered attachments by prefixing the uploaded filename with `SPOILER_`. This bot deletes the whole message because bots cannot remove a single attachment from another user's message.

## Discord Setup

You need both Discord-side permissions and the privileged intent:

- Server permissions let the bot see channels and delete messages.
- Message Content Intent lets the bot read message text and embed metadata for regex matching.

In the Discord Developer Portal:

- Create an application.
- Open **Bot**, create a bot, and copy the bot token.
- Enable **Message Content Intent** under **Bot**.
- Open **OAuth2 > URL Generator**.
- Select the `bot` scope.
- Select these bot permissions: **View Channels**, **Read Message History**, and **Manage Messages**.

The invite URL should look like this, with your application ID:

```text
https://discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&permissions=74752&integration_type=0&scope=bot
```

The application ID is only needed for this invite URL. It is not needed in `.env`, `config.json`, or the bot code.

## Configure

Copy the example files and edit them:

```sh
cp config.example.json config.json
cp .env.example .env
$EDITOR config.json
$EDITOR .env
```

To get a Discord user ID, enable **User Settings > Advanced > Developer Mode**, then right-click the user and choose **Copy User ID**.

Regex rules are matched against message content and embed metadata such as title, description, author, provider, footer, fields, and URLs. Use `(?i)` for case-insensitive matching.

`config.json` and `.env` are ignored by Git so tokens and server-specific settings do not get pushed to a public repo.

## Run Manually

```sh
go run ./cmd/spoilercleaner -config config.json
```

## Install As A User Service

Build and install the binary:

```sh
go build -o spoiler-cleaner ./cmd/spoilercleaner
mkdir -p ~/.local/bin ~/.local/share/spoiler-cleaner ~/.config/systemd/user
cp spoiler-cleaner ~/.local/bin/spoiler-cleaner
cp config.example.json ~/.local/share/spoiler-cleaner/config.json
cp .env.example ~/.local/share/spoiler-cleaner/.env
cp spoiler-cleaner.service ~/.config/systemd/user/spoiler-cleaner.service
```

Edit the installed files:

```sh
$EDITOR ~/.local/share/spoiler-cleaner/config.json
$EDITOR ~/.local/share/spoiler-cleaner/.env
chmod 600 ~/.local/share/spoiler-cleaner/.env
```

Start it:

```sh
systemctl --user daemon-reload
systemctl --user enable --now spoiler-cleaner.service
systemctl --user status spoiler-cleaner.service
```

View logs:

```sh
journalctl --user -u spoiler-cleaner.service -f
```

Keep the user service running after logout:

```sh
loginctl enable-linger "$USER"
```

The bot acts on new messages and message edits it receives while running; it does not scan old channel history.
