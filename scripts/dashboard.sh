#!/usr/bin/env bash

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/_common.sh"

root="$(dirname "$SCRIPTS_DIR")"
bin="$root/bin/tmux-agents"

# Build on first use and after the sources change (e.g. a plugin update).
if [[ ! -x $bin || -n $(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$bin" -print -quit) ]]; then
    command -v go &>/dev/null || die "tmux-agents: the dashboard needs Go to build (https://go.dev/dl/)."
    echo "Building the dashboard…"
    (cd "$root" && go build -o "$bin" ./cmd/tmux-agents) || die "tmux-agents: building the dashboard failed."
fi

exec "$bin" "$1"
