#!/usr/bin/env bash
# Install the preference model worker for the current user and start its systemd timer.
# Re-run after changing taste.py to deploy the new version.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
DEST="$HOME/.local/share/thumbnailer-taste"
VENV="$DEST/venv"

mkdir -p "$DEST" "$HOME/.config/systemd/user"

if [ ! -x "$VENV/bin/python" ]; then
  # torch wheels lag behind new Python releases; 3.12 is a safe choice
  uv venv --python 3.12 "$VENV"
  uv pip install --python "$VENV/bin/python" --index-url https://download.pytorch.org/whl/cpu torch torchvision
  uv pip install --python "$VENV/bin/python" -r "$HERE/requirements.txt"
fi

install -m 755 "$HERE/taste.py" "$DEST/taste.py"
install -m 644 "$HERE/systemd/thumbnailer-taste.service" "$HERE/systemd/thumbnailer-taste.timer" \
  "$HOME/.config/systemd/user/"

systemctl --user daemon-reload
systemctl --user enable --now thumbnailer-taste.timer
systemctl --user list-timers thumbnailer-taste.timer --no-pager
