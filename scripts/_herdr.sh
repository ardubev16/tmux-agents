#!/usr/bin/env bash

set -Eeuo pipefail
shopt -s inherit_errexit

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/_common.sh"

declare -r HERDR_SESSION_NAME=tmux-herdr
declare -r MINIMUM_HERDR_VERSION="0.7.1"

function _herdr_interactive() {
    HERDR_SESSION="$HERDR_SESSION_NAME" \
        HERDR_CONFIG_PATH="$SCRIPTS_DIR/../herdr/config.toml" \
        herdr "$@"
}

function _herdr() {
    local out
    out=$(_herdr_interactive "$@")

    [[ -n $debug_enabled ]] && echo -e "$*\n$(echo "$out" | jq --color-output)\n" >&2

    [[ -z $out ]] && return 1
    echo "$out"
}

function check_min_version() {
    # src: https://gist.github.com/jonlabelle/6691d740f404b9736116c22195a8d706
    function version_lt() { test "$(echo "$@" | tr " " "\n" | sort -rV | head -n 1)" != "$1"; }

    local herdr_version
    herdr_version=$(_herdr --version | awk '{print $2}')

    if version_lt "$herdr_version" "$MINIMUM_HERDR_VERSION"; then
        die "Herdr version $herdr_version is too old. Minimum supported version: $MINIMUM_HERDR_VERSION"
    fi
}

# HACK: This is a workaround since at the time of writing there doesn't seem to
# be a way to start a server in the background in a headless way.
function start_session() {
    _herdr 2>/dev/null &
    pid="$!"
    # NOTE: If in interactive mode reset the terminal before something breaks
    # from the weird TUI that tries to spawn. Useful for debugging.
    [[ $- == *i* ]] && reset

    wait_for_server
    kill -9 "$pid"

    _herdr server reload-config
}

function open_dashboard() {
    _herdr_interactive || true
}

function attach_agent() {
    local -r name="$1"
    [[ -n $debug_enabled ]] && echo "tmux-herdr: attaching to agent '$name'" >&2
    _herdr_interactive agent attach "$name" || true
}

function wait_for_server() {
    local -r max_retries=10

    local session_running="" retries=0
    while [[ $session_running != "true" && $retries -lt $max_retries ]]; do
        session_running=$(_herdr session list --json |
            jq --raw-output \
                --arg name "$HERDR_SESSION_NAME" \
                '.sessions | map(select(.name == $name).running) | .[]')
        retries=$((retries + 1))
        sleep 0.1
    done

    [[ $session_running != "true" ]] && return 1

    return 0
}

function get_agent_counts() {
    _herdr agent list |
        jq --raw-output 'reduce .result.agents[].agent_status as $status ({}; .[$status] += 1) |
            ((.idle // 0) + (.done // 0)),
            (.working // 0),
            (.blocked // 0)' | xargs
}

function _create_workspace() {
    local -r name="$1" path="$2"

    local ws_id
    ws_id=$(_herdr workspace list |
        jq --raw-output \
            --arg ws_label "$name" \
            '.result.workspaces | map(select(.label == $ws_label).workspace_id)[0]')

    if [[ $ws_id == "null" ]]; then
        ws_id=$(_herdr workspace create --label "$name" --cwd "$path" |
            jq --raw-output '.result.workspace.workspace_id')
    fi

    echo "$ws_id"
}

function _create_worktree() {
    local -r ws_id="$1" branch="$2" path="$3"

    local wt_id
    wt_id=$(_herdr worktree list --workspace "$ws_id" |
        jq --raw-output \
            --arg branch "$branch" \
            '.result.worktrees | map(select(.branch == $branch).open_workspace_id)[0]')

    if [[ $wt_id == "null" ]]; then
        local wt_path
        wt_path=$(_herdr worktree list --cwd "$path" |
            jq --raw-output \
                --arg branch "$branch" \
                '.result.worktrees | map(select(.branch == $branch).path)[0]')

        if [[ $wt_path == "null" ]]; then
            wt_id=$(_herdr worktree create --cwd "$path" --branch "$branch" --label "$branch" |
                jq --raw-output '.result.worktree.open_workspace_id')
        else
            wt_id=$(_herdr worktree open --cwd "$path" --path "$wt_path" --label "$branch" |
                jq --raw-output '.result.worktree.open_workspace_id')
        fi
    fi

    echo "$wt_id"
}

function _cleanup_extra_panes() {
    local -r ws_id="$1" pane_id="$2"

    local -a extra_panes
    mapfile -t extra_panes < <(_herdr pane list --workspace "$ws_id" |
        jq --raw-output \
            --arg pane_id "$pane_id" \
            '.result.panes | map(select(.pane_id != $pane_id).pane_id) | .[]')

    for pane in "${extra_panes[@]}"; do
        _herdr pane close "$pane" >/dev/null
    done
}

function _start_agent() {
    local -r ws_id="$1" name="$2" repo_path="$3" harness="$4"

    local pane_id
    pane_id=$(_herdr agent list |
        jq --raw-output \
            --arg name "$name" \
            '.result.agents | map(select(.name == $name).pane_id)[0]')

    if [[ $pane_id == "null" ]]; then
        local cwd
        cwd=$(_herdr workspace list |
            jq --raw-output \
                --arg ws_id "$ws_id" \
                '.result.workspaces | map(select(.workspace_id == $ws_id).worktree.checkout_path)[0]')

        if [[ $cwd == "null" ]]; then
            cwd="$repo_path"
        fi
        pane_id=$(_herdr agent start "$name" --workspace "$ws_id" --cwd "$cwd" -- "$harness" |
            jq --raw-output '.result.agent.pane_id')

        _cleanup_extra_panes "$ws_id" "$pane_id"
    fi
}

function new_agent() {
    local -r harness="$1" branch_name="$2"

    local repo_full_path repo_name project_name
    get_repo_metadata repo_full_path repo_name project_name

    local -r workspace_name="$(repo_prefix "$project_name" "$repo_name")"
    local -r agent_name="${workspace_name}::${branch_name}"

    local ws_id wt_id
    ws_id=$(_create_workspace "$workspace_name" "$repo_full_path")
    wt_id=$(_create_worktree "$ws_id" "$branch_name" "$repo_full_path")

    _start_agent "$wt_id" "$agent_name" "$repo_full_path" "$harness"

    _herdr agent focus "$agent_name" >/dev/null

    echo "$agent_name"
}
