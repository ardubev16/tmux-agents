#!/usr/bin/env bash

set -Eeuo pipefail
shopt -s inherit_errexit

declare SCRIPTS_DIR
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

declare debug_enabled=""
[[ "${TMUX_AGENTS_DEBUG:-}" == on ]] && debug_enabled=1

function _pause() {
    [[ -t 0 ]] || return 0
    read -rsn1 -p "Press any key to close this pane..." >&2
    echo >&2
}

function die() {
    echo "$1" >&2
    _pause
    exit 1
}

function _err_trap() {
    local -r exit_code=$? line=$1 command=$2

    if [[ -n $debug_enabled ]]; then
        echo "tmux-agents: error (exit $exit_code) at line $line: $command" >&2
    else
        echo "tmux-agents: unexpected error (exit $exit_code). Set '@tmux_agents_debug on' for details." >&2
    fi
    _pause
    exit "$exit_code"
}
trap '_err_trap $LINENO "$BASH_COMMAND"' ERR

function get_repo_metadata() {
    local -n _repo_full_path="$1" _repo_name="$2" _project_name="$3"
    local -r _path="${4:-.}"

    # Common dir points at the main checkout even when called from inside a worktree.
    local _git_dir
    _git_dir=$(git -C "$_path" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || {
        echo "Not a git repo." >&2
        return 1
    }

    _repo_full_path=$(dirname "$_git_dir")
    _repo_name=$(basename "$_repo_full_path")
    _project_name=$(basename "$(dirname "$_repo_full_path")")
}

function select_branch() {
    local -r path="$1"
    local fzf_out fzf_exit=0
    branches=$(git -C "$path" branch)
    fzf_out=$({
        grep '^\*' <<<"$branches"
        grep -v '^\*' <<<"$branches"
    } | fzf --print-query --prompt="⎇ branch: ") || fzf_exit=$?

    # NOTE: Ctrl-C returns 130
    [[ $fzf_exit == 130 ]] && return 130

    local -r query=$(sed -n '1p' <<<"$fzf_out")
    local -r selection=$(sed -n '2p' <<<"$fzf_out")

    local branch_name
    if [[ -n "$selection" ]]; then
        branch_name="${selection:2}"
    else
        branch_name="$query"
    fi

    echo "$branch_name"
}

# Prints the checkout path for $branch, creating a worktree under
# ${base_dir:-<project>/.worktrees}/<repo>/<branch> if none has it checked out.
function ensure_worktree() {
    local -r path="$1" branch="$2" base_dir="${3:-}"

    local repo_full_path repo_name project_name
    get_repo_metadata repo_full_path repo_name project_name "$path"

    local existing
    existing=$(git -C "$repo_full_path" worktree list --porcelain |
        awk -v ref="branch refs/heads/$branch" '/^worktree /{ wt = substr($0, 10) } $0 == ref { print wt; exit }')
    if [[ -n $existing ]]; then
        echo "$existing"
        return 0
    fi

    local -r wt_path="${base_dir:-$(dirname "$repo_full_path")/.worktrees}/$repo_name/$branch"
    # Existing local or remote-tracking branch: let git check it out (DWIM), otherwise branch off HEAD.
    if git -C "$repo_full_path" show-ref --quiet "refs/heads/$branch" ||
        [[ -n $(git -C "$repo_full_path" for-each-ref --format=x "refs/remotes/*/$branch") ]]; then
        git -C "$repo_full_path" worktree add --quiet "$wt_path" "$branch" >&2
    else
        git -C "$repo_full_path" worktree add --quiet -b "$branch" "$wt_path" >&2
    fi

    echo "$wt_path"
}

# Agent dashboard: $1 = main.sh (callbacks for list/preview/reply/switch).
function agent_dashboard() {
    local -r main="$1"
    "$main" list_agents | fzf \
        --delimiter='\t' --with-nth=2.. --no-sort --reverse \
        --prompt='agent> ' \
        --header='enter: switch · ctrl-r: reply · ctrl-l: refresh · esc: close' \
        --preview="'$main' preview_agent {1}" \
        --preview-window='right,65%,follow' \
        --bind="enter:become('$main' switch_agent {1})" \
        --bind="ctrl-r:execute('$main' reply_agent {1})+reload('$main' list_agents)+refresh-preview" \
        --bind="ctrl-l:reload('$main' list_agents)+refresh-preview" || true
}
