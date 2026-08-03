#!/usr/bin/env sh
set -eu

if systemctl --user cat spoiler-cleaner.service >/dev/null 2>&1; then
	systemctl --user disable --now spoiler-cleaner.service
fi
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/spoiler-cleaner.service"

systemctl --user daemon-reload
systemctl --user enable --now discord-message-delete.service
