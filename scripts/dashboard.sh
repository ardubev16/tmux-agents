#!/usr/bin/env bash

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/_common.sh"

command -v uv &>/dev/null || die "tmux-agents: the dashboard needs uv (https://docs.astral.sh/uv/)."
# First run creates the plugin's .venv from uv.lock, later runs start straight away.
uv run --quiet --frozen --project "$SCRIPTS_DIR/.." tmux-agents "$1"
