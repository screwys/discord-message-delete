# Discord Message Delete

A small Go Discord bot that deletes messages according to configurable rules. Its spoiler cleaner deletes messages from one configured user when they contain spoilered visual media, regex rules delete matching messages from non-ignored users, and emoji rules delete messages containing a configured emoji.

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

Emoji rules accept a standard shortcode such as `:thumbsup:`, a custom emoji name such as `:server_spade:`, the Unicode emoji itself, or a custom emoji mention copied from Discord such as `<:party:123456789012345678>`. Known standard shortcodes are stored as Unicode. Other shortcode names match custom emoji by name; custom mentions match by ID, which remains stable if the emoji is renamed.

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

The command accepts `start`, `stop`, `restart`, `reload`, `status`, `enable`, `disable`, and `logs`. For example:

```sh
discord-message-delete restart
discord-message-delete logs
```

Add or delete a case-insensitive blocked word, phrase, or regex from the active ruleset:

```sh
discord-message-delete rule add example
discord-message-delete rule add '\bblocked phrase\b'
discord-message-delete rule delete example
```

Add or delete an emoji rule:

```sh
discord-message-delete rule add emoji :thumbsup:
discord-message-delete rule add emoji :server_spade:
discord-message-delete rule add emoji '👍'
discord-message-delete rule add emoji '<:party:123456789012345678>'
discord-message-delete rule delete emoji :thumbsup:
```

Plain words and phrases are folded to their canonical form and kept together in the single `blocked words` pattern. Named regex rules remain separate. Common ASCII substitutions such as `@` or `4` for `a`, `1` for `l`, and Unicode confusables are handled by the matcher, so they do not need separate rules.

The command parses and validates the complete configuration, normalizes and deduplicates regex and emoji rules, preserves the other settings, and atomically writes properly formatted JSON. Startup performs the same normalization, so restarting also repairs an older split rule list. After a successful command it restarts the service so the saved rules and running process cannot diverge. Quote values that contain spaces or shell metacharacters.

Emoji rules also remove matching reactions as they are added. Discord sends reaction events over the existing gateway connection, and the bot removes all reactions with that emoji from only the affected message. It does not poll or scan channel history, so reactions on untouched old messages remain until someone adds that emoji to the message again.

View logs:

```sh
discord-message-delete logs
```

Keep the user service running after logout:

```sh
loginctl enable-linger "$USER"
```

The bot acts on new messages, message edits, and reaction additions it receives while running; it does not scan old channel history.
