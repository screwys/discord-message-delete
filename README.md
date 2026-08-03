# Discord Message Delete

A small Go Discord bot that deletes messages according to configurable rules. Its spoiler cleaner deletes messages from one configured user when they contain spoilered visual media, while regex rules delete matching messages from non-ignored users.

Discord can represent spoilered media through attachment flags, visual components, or forwarded-message snapshots. The bot also recognizes the older `SPOILER_` filename convention. It deletes the whole message because bots cannot remove a single attachment from another user's message.

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

Regex rules are matched case-insensitively against message content and embed metadata such as title, description, author, provider, footer, fields, and URLs. Each rule is checked against both the original text and a Unicode confusable-folded copy, with invisible formatting characters removed from the folded copy.

`config.json` and `.env` are ignored by Git so tokens and server-specific settings do not get pushed to a public repo.

## Run Manually

```sh
go run ./cmd/discord-message-delete -config config.json
```

## Install As A User Service

After `config.json` and `.env` exist in this repo, build the binary and install the service:

```sh
go build -o discord-message-delete ./cmd/discord-message-delete
mkdir -p ~/.config/systemd/user
cp discord-message-delete.service ~/.config/systemd/user/discord-message-delete.service
chmod 600 .env
```

Start it:

```sh
./scripts/start-service.sh
discord-message-delete status
```

The start script disables and removes the former `spoiler-cleaner.service` unit if it is still installed, preventing both names from running at once.

When the service starts, it creates `~/.local/bin/discord-message-delete` as a link to the built binary. Most Linux desktop sessions include `~/.local/bin` in `PATH`. If yours does not, add that directory to your shell's `PATH` once; a service cannot change the environment of an already-running shell.

The command accepts `start`, `stop`, `restart`, `status`, `enable`, `disable`, and `logs`. For example:

```sh
discord-message-delete restart
discord-message-delete logs
```

View logs:

```sh
discord-message-delete logs
```

For the configured spoiler target, each new message or edit produces a privacy-safe summary like:

```text
spoiler check event=create attachments=2 flags=1 legacy_markers=0 images=2 matching_attachments=1 visual_components=false delete=true
delete succeeded event=create rule=spoiler_media
```

`flags` counts Discord's spoiler attachment flag, `legacy_markers` counts the older filename marker, and `visual_components` covers spoilered component or forwarded-snapshot media. The logs omit message text, filenames, URLs, user and channel IDs, regex contents, config, and tokens.

Keep the user service running after logout:

```sh
loginctl enable-linger "$USER"
```

The bot acts on new messages and message edits it receives while running; it does not scan old channel history.
