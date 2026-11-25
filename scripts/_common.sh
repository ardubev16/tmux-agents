#!/usr/bin/env bash
# tmux-free helpers sourced by main.sh: error handling, git/worktree logic, the fzf branch picker.
# Kept apart from main.sh so tests can source them without tmux.

set -Eeuo pipefail
shopt -s inherit_errexit

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

# Prints the main checkout of the repo containing $1, also when $1 is inside a worktree.
function repo_root() {
    local git_dir
    git_dir=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || {
        echo "Not a git repo." >&2
        return 1
    }
    dirname "$git_dir"
}

# Prints the picked branch, or the typed query for a new one; fails when cancelled (Esc/Ctrl-C).
function select_branch() {
    local branches fzf_out
    branches=$(git -C "$1" branch)
    # Exit 1 means no match: the query names a new branch. Anything else (130) is a cancel; return explicitly,
    # since callers run this in `|| …`, where errexit does not apply.
    fzf_out=$({
        grep '^\*' <<<"$branches"
        grep -v '^\*' <<<"$branches"
    } | fzf --print-query --prompt="⎇ branch: ") || [[ $? == 1 ]] || return 1

    local -r query=$(sed -n '1p' <<<"$fzf_out") selection=$(sed -n '2p' <<<"$fzf_out")
    if [[ -n $selection ]]; then
        echo "${selection:2}"
    else
        echo "$query"
    fi
}

# Prints the checkout path for $branch, creating a worktree under
# ${base_dir:-<project>/.worktrees}/<repo>/<branch> if none has it checked out.
function ensure_worktree() {
    local -r path="$1" branch="$2" base_dir="${3:-}"
    local repo
    repo=$(repo_root "$path")

    local existing
    existing=$(git -C "$repo" worktree list --porcelain |
        awk -v ref="branch refs/heads/$branch" '/^worktree /{ wt = substr($0, 10) } $0 == ref { print wt; exit }')
    if [[ -n $existing ]]; then
        echo "$existing"
        return 0
    fi

    local -r wt_path="${base_dir:-$(dirname "$repo")/.worktrees}/$(basename "$repo")/$branch"
    # Existing local or remote-tracking branch: let git check it out (DWIM), otherwise branch off HEAD.
    if git -C "$repo" show-ref --quiet "refs/heads/$branch" ||
        [[ -n $(git -C "$repo" for-each-ref --format=x "refs/remotes/*/$branch") ]]; then
        git -C "$repo" worktree add --quiet "$wt_path" "$branch" >&2
    else
        git -C "$repo" worktree add --quiet -b "$branch" "$wt_path" >&2
    fi

    echo "$wt_path"
}
