#!/usr/bin/env bash

set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/_common.sh"

MAIN="$(realpath "${BASH_SOURCE[0]}")"
declare -r MAIN
declare -r state_option='@tmux_agents_state'
declare -r transcript_option='@tmux_agents_transcript'
declare -r socket_option='@tmux_agents_socket'
declare -r unread_option='@tmux_agents_unread'

function tmux_option() {
    local -r option=$(tmux show-option -gqv "$1")
    echo "${option:-$2}"
}

# Called by agent hooks from inside the agent's pane: working | blocked | done | clear.
function set_state() {
    local -r state="$1"
    [[ -n ${TMUX_PANE:-} ]] || return 0

    if [[ $state == clear ]]; then
        local option
        for option in "$state_option" "$transcript_option" "$socket_option" "$unread_option"; do
            tmux set-option -pu -t "$TMUX_PANE" "$option"
        done
    else
        tmux set-option -p -t "$TMUX_PANE" "$state_option" "$state"
    fi

    # Finished while you were not looking at it: the dashboard marks it until you visit the pane.
    local -r visible="#{&&:#{pane_active},#{&&:#{window_active},#{session_attached}}}"
    if [[ $state == done && $(tmux display-message -p -t "$TMUX_PANE" "$visible") != 1 ]]; then
        tmux set-option -p -t "$TMUX_PANE" "$unread_option" 1
    elif [[ $state != clear ]]; then
        tmux set-option -pu -t "$TMUX_PANE" "$unread_option"
    fi

    # Ringing the pane's bell lets tmux flag the window (! in the window/session list) and the terminal.
    if [[ $state == blocked || $state == done ]] && [[ $(tmux_option @tmux_agents_bell on) == on ]]; then
        printf '\a' >"$(tmux display-message -p -t "$TMUX_PANE" '#{pane_tty}')"
    fi

    local client
    while read -r client; do
        tmux refresh-client -S -t "$client"
    done < <(tmux list-clients -F '#{client_name}')
}

# Called by agent hooks when a session starts or switches: <session file> [socket].
# The session file lets the dashboard render the chat itself; the socket (omp) streams the
# in-progress reply and shares the prompt draft.
function set_session() {
    local -r transcript="$1" socket="${2:-}"
    [[ -n ${TMUX_PANE:-} && -n $transcript ]] || return 0
    tmux set-option -p -t "$TMUX_PANE" "$transcript_option" "$transcript"
    if [[ -n $socket ]]; then
        tmux set-option -p -t "$TMUX_PANE" "$socket_option" "$socket"
    else
        tmux set-option -pu -t "$TMUX_PANE" "$socket_option"
    fi
}

# Called by tmux hooks when a pane is shown: clears its unread mark.
function seen() {
    tmux set-option -pu -t "$1" "$unread_option" 2>/dev/null || true
}

# One agent per line, blocked first, then unread done: pane_id \t state \t target \t command \t path \t
# transcript \t socket \t unread
function list_agents() {
    local -r shell="$(basename "$(tmux show-option -gqv default-shell)")"
    # shortcut: an agent killed without its exit hook keeps its state until its pane is back at the shell prompt.
    tmux list-panes -a -F "#{pane_id}	#{$state_option}	#{session_name}:#{window_index}.#{pane_index}	#{pane_current_command}	#{pane_current_path}	#{$transcript_option}	#{$socket_option}	#{$unread_option}" |
        awk -F '\t' -v shell="$shell" -v home="$HOME" '
            BEGIN { OFS = "\t"; rank["blocked"] = 0; rank["done"] = 2; rank["working"] = 3 }
            $2 != "" && $4 != shell {
                sub("^" home, "~", $5)
                r = ($2 == "done" && $8 != "") ? 1 : rank[$2]
                print r, $0
            }' |
        sort -s -n -k1,1 | cut -f2-
}

function status_bar() {
    local -r done_foreground="$1" working_foreground="$2" blocked_foreground="$3"
    list_agents | awk -F '\t' \
        -v d="$done_foreground" -v w="$working_foreground" -v b="$blocked_foreground" '
        { n[$2]++ }
        END { printf "#[fg=%s] ○ %d #[fg=%s] ⦿ %d #[fg=%s] ● %d ", d, n["done"], w, n["working"], b, n["blocked"] }'
}

function agent_dashboard() {
    tmux display-popup -w 90% -h 90% -E "'$SCRIPTS_DIR/dashboard.sh' '$MAIN'"
}

function switch_agent() {
    tmux switch-client -t "$1"
    seen "$1"
}

# Stops the agent by closing its pane (tmux sends the harness SIGHUP).
function close_agent() {
    tmux kill-pane -t "$1"
}

# Sends $2 followed by Enter; an empty $2 sends just Enter.
function reply_agent() {
    local -r pane="$1" text="$2"
    [[ -n $text ]] && tmux send-keys -t "$pane" -l "$text"
    tmux send-keys -t "$pane" Enter
}

function new_worktree() {
    local -r current_path=$(tmux display-message -p '#{pane_current_path}')
    local -r tmpfile="$(mktemp)"

    local popup_exit=0
    tmux display-popup -E "'$SCRIPTS_DIR/select_branch.sh' '$current_path' '$tmpfile'" || popup_exit=$?

    local -r branch="$(<"$tmpfile")"
    rm -f "$tmpfile"

    [[ $popup_exit == 130 || -z $branch ]] && return 0

    local wt_path
    wt_path=$("$SCRIPTS_DIR/worktree.sh" "$current_path" "$branch" "$(tmux_option @tmux_agents_worktree_dir "")")

    # Reuse any session already rooted there (e.g. the main checkout opened by a sessionizer).
    local session
    session=$(tmux list-sessions -F '#{session_path}	#{session_name}' |
        awk -F '\t' -v path="$wt_path" '$1 == path { print $2; exit }')

    if [[ -z $session ]]; then
        local repo_full_path repo_name project_name
        get_repo_metadata repo_full_path repo_name project_name "$current_path"
        session=$(tr '.:' '__' <<<"$repo_name@$branch")

        tmux new-session -ds "$session" -c "$wt_path"
        local -r command="$(tmux_option @tmux_agents_worktree_command "")"
        [[ -n $command ]] && tmux send-keys -t "=$session:" "$command" Enter
    fi

    tmux switch-client -t "=$session"
}

if [[ $# -lt 1 ]]; then
    die "Usage: main.sh <command> [args...]"
fi

command="$1"
shift

case "$command" in
state) set_state "$@" ;;
session) set_session "$@" ;;
status_bar) status_bar "$@" ;;
agent_dashboard) agent_dashboard "$@" ;;
list_agents) list_agents "$@" ;;
switch_agent) switch_agent "$@" ;;
close_agent) close_agent "$@" ;;
seen) seen "$@" ;;
reply_agent) reply_agent "$@" ;;
new_worktree) new_worktree "$@" ;;
*) die "Unknown command: $command" ;;
esac
