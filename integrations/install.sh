#!/usr/bin/env bash
# Install or remove the tmux-agents harness integrations.
# Usage: integrations/install.sh install|uninstall

set -euo pipefail

INTEGRATIONS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OMP_EXTENSION="$HOME/.omp/agent/extensions/tmux-agents.ts"
CLAUDE_SETTINGS="$HOME/.claude/settings.json"

# Drops every hook entry that calls "$TMUX_AGENTS" (state/session), then any event left empty by that.
# shellcheck disable=SC2016 # $TMUX_AGENTS is matched literally.
STRIP='def strip: if .hooks then .hooks |= (
    with_entries(.value |= map(select(any(.hooks[]?; (.command // "") | contains("\"$TMUX_AGENTS\" ")) | not)))
    | with_entries(select(.value | length > 0))) else . end;'

function write_claude_settings() {
    local -r filter="$1"
    if [[ -L $CLAUDE_SETTINGS ]]; then
        echo "claude: $CLAUDE_SETTINGS is a symlink (managed elsewhere?), edit the hooks from integrations/claude-hooks.json there" >&2
        return 0
    fi

    local current='{}'
    [[ -f $CLAUDE_SETTINGS ]] && current=$(<"$CLAUDE_SETTINGS")

    local updated
    updated=$(jq --slurpfile ours "$INTEGRATIONS_DIR/claude-hooks.json" "$STRIP $filter" <<<"$current")
    # Write in place (not mv) to keep the file's permissions.
    printf '%s\n' "$updated" >"$CLAUDE_SETTINGS"
}

function install() {
    if [[ -d $HOME/.omp ]]; then
        mkdir -p "$(dirname "$OMP_EXTENSION")"
        ln -sfn "$INTEGRATIONS_DIR/omp.ts" "$OMP_EXTENSION"
        echo "omp: linked $OMP_EXTENSION"
    fi

    if [[ -d $HOME/.claude ]]; then
        write_claude_settings 'strip | .hooks = (reduce ($ours[0].hooks | to_entries[]) as $e (.hooks // {}; .[$e.key] += $e.value))'
        echo "claude: hooks added to $CLAUDE_SETTINGS"
    fi
}

function uninstall() {
    # Only remove the link if it is ours (also after the plugin folder moved).
    if [[ -L $OMP_EXTENSION && $(readlink "$OMP_EXTENSION") == */integrations/omp.ts ]]; then
        rm "$OMP_EXTENSION"
        echo "omp: removed $OMP_EXTENSION"
    fi

    if [[ -f $CLAUDE_SETTINGS ]]; then
        write_claude_settings 'strip'
        echo "claude: hooks removed from $CLAUDE_SETTINGS"
    fi
}

case "${1:-}" in
install) install ;;
uninstall) uninstall ;;
*)
    echo "Usage: $0 install|uninstall" >&2
    exit 1
    ;;
esac
