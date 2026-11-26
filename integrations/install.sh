#!/usr/bin/env bash
# Install or remove the tmux-agents integration of the given harnesses.
# Usage: integrations/install.sh install|uninstall omp|claude...

set -euo pipefail

INTEGRATIONS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OMP_EXTENSION="$HOME/.omp/agent/extensions/tmux-agents.ts"
CLAUDE_SETTINGS="$HOME/.claude/settings.json"
USAGE="Usage: $0 install|uninstall omp|claude..."

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
    mkdir -p "$(dirname "$CLAUDE_SETTINGS")"
    # Write in place (not mv) to keep the file's permissions.
    printf '%s\n' "$updated" >"$CLAUDE_SETTINGS"
}

function install_omp() {
    mkdir -p "$(dirname "$OMP_EXTENSION")"
    ln -sfn "$INTEGRATIONS_DIR/omp.ts" "$OMP_EXTENSION"
    echo "omp: linked $OMP_EXTENSION"
}

function install_claude() {
    # shellcheck disable=SC2016 # $ours and $e are jq variables
    write_claude_settings 'strip | .hooks = (reduce ($ours[0].hooks | to_entries[]) as $e (.hooks // {}; .[$e.key] += $e.value))'
    echo "claude: hooks added to $CLAUDE_SETTINGS"
}

function uninstall_omp() {
    # Only remove the link if it is ours (also after the plugin folder moved).
    if [[ -L $OMP_EXTENSION && $(readlink "$OMP_EXTENSION") == */integrations/omp.ts ]]; then
        rm "$OMP_EXTENSION"
        echo "omp: removed $OMP_EXTENSION"
    fi
}

function uninstall_claude() {
    if [[ -f $CLAUDE_SETTINGS ]]; then
        write_claude_settings 'strip'
        echo "claude: hooks removed from $CLAUDE_SETTINGS"
    fi
}

action="${1:-}"
[[ ($action == install || $action == uninstall) && $# -ge 2 ]] || {
    echo "$USAGE" >&2
    exit 1
}
shift
# Check them all first, so a typo does not leave the others half done.
for harness in "$@"; do
    [[ $harness == omp || $harness == claude ]] || {
        echo "Unknown harness: $harness. $USAGE" >&2
        exit 1
    }
done
for harness in "$@"; do
    "${action}_$harness"
done
