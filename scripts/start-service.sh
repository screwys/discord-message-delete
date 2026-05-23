#!/usr/bin/env sh
set -eu

systemctl --user daemon-reload
systemctl --user enable --now spoiler-cleaner.service
