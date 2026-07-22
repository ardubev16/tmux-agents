#!/usr/bin/env bash

set -Eeuo pipefail
shopt -s inherit_errexit

declare SCRIPTS_DIR
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

declare debug_enabled=""
[[ "${TMUX_HERDR_DEBUG:-}" == on ]] && debug_enabled=1

function _herdr_pause() {
    [[ -t 0 ]] || return 0
    read -rsn1 -p "Press any key to close this pane..." >&2
    echo >&2
}

function die() {
    echo "$1" >&2
    _herdr_pause
    exit 1
}

function _herdr_err_trap() {
    local -r exit_code=$? line=$1 command=$2

    if [[ -n $debug_enabled ]]; then
        echo "tmux-herdr: error (exit $exit_code) at line $line: $command" >&2
    else
        echo "tmux-herdr: unexpected error (exit $exit_code). Set '@tmux_herdr_debug on' for details." >&2
    fi
    _herdr_pause
    exit "$exit_code"
}
trap '_herdr_err_trap $LINENO "$BASH_COMMAND"' ERR

function get_repo_metadata() {
    local -n _repo_full_path="$1" _repo_name="$2" _project_name="$3"
    local -r _path="${4:-.}"

    local _git_dir
    _git_dir=$(git -C "$_path" rev-parse --absolute-git-dir 2>/dev/null) || {
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

function repo_prefix() {
    local -r project_name="$1" repo_name="$2"
    echo "${project_name}::${repo_name}"
}
