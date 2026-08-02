# Spoiler Cleaner Discord Bot

A small Go Discord bot that deletes messages from one configured user when they contain spoilered visual media, and deletes messages from non-ignored users when content or embed metadata matches configured regex rules.

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

Regex rules are matched against message content and embed metadata such as title, description, author, provider, footer, fields, and URLs. Each rule is checked against both the original text and a Unicode confusable-folded copy, with invisible formatting characters removed from the folded copy. Use `(?i)` for case-insensitive matching.

`config.json` and `.env` are ignored by Git so tokens and server-specific settings do not get pushed to a public repo.

## Run Manually

```sh
go run ./cmd/spoilercleaner -config config.json
```

## Install As A User Service

After `config.json` and `.env` exist in this repo, build the binary and install the service:

```sh
go build -o spoiler-cleaner ./cmd/spoilercleaner
mkdir -p ~/.config/systemd/user
cp spoiler-cleaner.service ~/.config/systemd/user/spoiler-cleaner.service
chmod 600 .env
```

Start it:

```sh
./scripts/start-service.sh
systemctl --user status spoiler-cleaner.service
```

View logs:

```sh
journalctl --user -u spoiler-cleaner.service -f
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
