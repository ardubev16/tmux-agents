#!/usr/bin/env bash

set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/_common.sh"

MAIN="$(realpath "${BASH_SOURCE[0]}")"
declare -r MAIN
declare -r state_option='@tmux_agents_state'
declare -r transcript_option='@tmux_agents_transcript'
declare -r socket_option='@tmux_agents_socket'
declare -r unread_option='@tmux_agents_unread'
declare -r since_option='@tmux_agents_since'

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
        for option in "$state_option" "$transcript_option" "$socket_option" "$unread_option" "$since_option"; do
            tmux set-option -pu -t "$TMUX_PANE" "$option"
        done
    else
        # Hooks repeat a state (Claude Code reports working after every tool): only a change restarts the clock.
        [[ $(tmux show-option -pqv -t "$TMUX_PANE" "$state_option") == "$state" ]] ||
            tmux set-option -p -t "$TMUX_PANE" "$since_option" "$EPOCHSECONDS"
        tmux set-option -p -t "$TMUX_PANE" "$state_option" "$state"
    fi

    # Only when you are not looking at the pane: mark it unread (done) and notify (done/blocked).
    local -r visible_format="#{&&:#{pane_active},#{&&:#{window_active},#{session_attached}}}"
    local -r visible=$(tmux display-message -p -t "$TMUX_PANE" "$visible_format")
    if [[ $state == done && $visible != 1 ]]; then
        tmux set-option -p -t "$TMUX_PANE" "$unread_option" 1
    elif [[ $state != clear ]]; then
        tmux set-option -pu -t "$TMUX_PANE" "$unread_option"
    fi
    if [[ ($state == done || $state == blocked) && $visible != 1 ]]; then
        notify "$(agent_name "$TMUX_PANE") $([[ $state == done ]] && echo finished || echo 'needs input')"
    fi

    local client
    while read -r client; do
        tmux refresh-client -S -t "$client"
    done < <(tmux list-clients -F '#{client_name}')
}

# The session name the harness gave the agent (omp: title line, Claude Code: last ai-title), else its window.
function agent_name() {
    local -r transcript=$(tmux show-option -pqv -t "$1" "$transcript_option")
    local name=""
    [[ -f $transcript ]] && name=$(grep -h -e '"type":"title"' -e '"type":"ai-title"' "$transcript" |
        tail -n 1 | jq -r '.title // .aiTitle // empty' 2>/dev/null)
    echo "${name:-$(tmux display-message -p -t "$1" '#{session_name}:#{window_index}')}"
}

# @tmux_agents_notify: system (desktop notification, falls back to tmux) | tmux (status line message) | off.
function notify() {
    local -r message="$1" mode=$(tmux_option @tmux_agents_notify system)
    [[ $mode == off ]] && return 0
    if [[ $mode == system ]] && command -v notify-send >/dev/null &&
        notify-send --app-name=tmux-agents tmux-agents "$message" 2>/dev/null; then
        return 0
    fi
    local client
    while read -r client; do
        tmux display-message -c "$client" -d 5000 "tmux-agents: $message"
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
# transcript \t socket \t unread \t since (epoch seconds the state began)
function list_agents() {
    local -r shell="$(basename "$(tmux show-option -gqv default-shell)")"
    # shortcut: an agent killed without its exit hook keeps its state until its pane is back at the shell prompt.
    tmux list-panes -a -F "#{pane_id}	#{$state_option}	#{session_name}:#{window_index}.#{pane_index}	#{pane_current_command}	#{pane_current_path}	#{$transcript_option}	#{$socket_option}	#{$unread_option}	#{$since_option}" |
        awk -F '\t' -v shell="$shell" '
            BEGIN { OFS = "\t"; rank["blocked"] = 0; rank["done"] = 2; rank["working"] = 3 }
            $2 != "" && $4 != shell {
                r = ($2 == "done" && $8 != "") ? 1 : rank[$2]
                print r, $0
            }' |
        sort -s -n -k1,1 | cut -f2-
}

# hide_zero=on leaves out counters at 0, so nothing shows while no agent runs.
function status_bar() {
    local -r done_foreground="$1" working_foreground="$2" blocked_foreground="$3" hide_zero="${4:-off}"
    list_agents | awk -F '\t' \
        -v d="$done_foreground" -v w="$working_foreground" -v b="$blocked_foreground" -v hide="$hide_zero" '
        function counter(color, glyph, count) {
            return (hide == "on" && !count) ? "" : sprintf("#[fg=%s] %s %d ", color, glyph, count)
        }
        { n[$2]++ }
        END { printf "%s", counter(d, "○", n["done"]) counter(w, "⦿", n["working"]) counter(b, "●", n["blocked"]) }'
}

function agent_dashboard() {
    tmux display-popup -w 90% -h 90% -E "'$MAIN' dashboard"
}

# Popup body: builds the Go dashboard on first use and after the sources change (e.g. a plugin update).
function dashboard() {
    local -r root="$(dirname "$MAIN")"
    local -r bin="$root/bin/tmux-agents"
    if [[ ! -x $bin || -n $(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$bin" -print -quit) ]]; then
        command -v go &>/dev/null || die "tmux-agents: the dashboard needs Go to build (https://go.dev/dl/)."
        echo "Building the dashboard…"
        (cd "$root" && go build -o "$bin" ./cmd/tmux-agents) || die "tmux-agents: building the dashboard failed."
    fi
    exec "$bin" "$MAIN"
}

function switch_agent() {
    tmux switch-client -t "$1"
    seen "$1"
}

# Stops the agent like a normal exit, then closes its pane. omp and Claude Code both handle SIGTERM by running
# their shutdown hooks (which clear the pane's state) and saving the session.
function close_agent() {
    local -r pane="$1"
    local -r shell="$(basename "$(tmux show-option -gqv default-shell)")"
    local pane_pid job
    pane_pid=$(tmux display-message -p -t "$pane" '#{pane_pid}')
    # The terminal's foreground process group is the running agent (or the shell when nothing runs).
    job=$(ps -o tpgid= -p "$pane_pid" | tr -d ' ')
    if [[ -n $job && $job -gt 0 && $(ps -o comm= -p "$job") != "$shell" ]]; then
        kill -TERM -- "-$job" 2>/dev/null || true
        local _
        for _ in {1..50}; do
            kill -0 -- "-$job" 2>/dev/null || break
            sleep 0.1
        done
    fi
    tmux kill-pane -t "$pane" 2>/dev/null || true
}

# Sends $2 followed by Enter; an empty $2 sends just Enter.
function reply_agent() {
    local -r pane="$1" text="$2"
    [[ -n $text ]] && tmux send-keys -t "$pane" -l "$text"
    tmux send-keys -t "$pane" Enter
}

function new_worktree() {
    local -r current_path=$(tmux display-message -p '#{pane_current_path}')
    # The path goes in as the popup's directory, not into its command line, so quotes in it are harmless.
    tmux display-popup -d "$current_path" -E "'$MAIN' pick_worktree"
}

# Popup body: pick a branch, create or reuse its worktree, switch to a session rooted there. ctrl-x on a branch
# removes its worktree instead, then the picker opens again. Errors stay on screen until a key is pressed
# (die/the error trap pause in a popup).
function pick_worktree() {
    local where="$PWD" repo picked branch
    repo=$(repo_root "$where")
    while true; do
        picked=$(select_branch "$where") || return 0 # cancelled
        branch=${picked#* }
        [[ ${picked%% *} == remove ]] || break
        [[ -z $branch ]] || remove_branch_worktree "$repo" "$branch"
        where=$repo # the worktree the picker started in may be gone
    done
    [[ -n $branch ]] || return 0

    local wt_path
    wt_path=$(ensure_worktree "$repo" "$branch" "$(tmux_option @tmux_agents_worktree_dir "")")

    # Reuse any session already rooted there (e.g. the main checkout opened by a sessionizer).
    local session
    session=$(tmux list-sessions -F '#{session_path}	#{session_name}' |
        awk -F '\t' -v path="$wt_path" '$1 == path { print $2; exit }')

    if [[ -z $session ]]; then
        session=$(tr '.:' '__' <<<"$(basename "$repo")@$branch")
        # Another repo with the same folder name has this branch open: tell them apart by their project folder.
        tmux has-session -t "=$session" 2>/dev/null && session=$(tr '.:' '__' <<<"$(basename "$(dirname "$repo")")-$session")
        tmux new-session -ds "$session" -c "$wt_path"
        local -r command="$(tmux_option @tmux_agents_worktree_command "")"
        [[ -n $command ]] && tmux send-keys -t "=$session:" "$command" Enter
    fi

    tmux switch-client -t "=$session"
}

# Removes $2's worktree (git refuses one with uncommitted changes) and kills the sessions rooted there; a
# refusal stays on screen until a key is pressed.
function remove_branch_worktree() {
    local wt
    if ! wt=$(remove_worktree "$1" "$2"); then
        _pause
        return 0
    fi
    local -r current=$(tmux display-message -p '#{session_id}')
    local id
    while read -r id; do
        # Killing the session this client shows would detach it: move the client elsewhere first.
        if [[ $id == "$current" ]]; then
            tmux switch-client -l 2>/dev/null || tmux switch-client -n
        fi
        tmux kill-session -t "$id"
    done < <(tmux list-sessions -F '#{session_path}	#{session_id}' | awk -F '\t' -v path="$wt" '$1 == path { print $2 }')
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
dashboard) dashboard "$@" ;;
list_agents) list_agents "$@" ;;
switch_agent) switch_agent "$@" ;;
close_agent) close_agent "$@" ;;
seen) seen "$@" ;;
reply_agent) reply_agent "$@" ;;
new_worktree) new_worktree "$@" ;;
pick_worktree) pick_worktree "$@" ;;
*) die "Unknown command: $command" ;;
esac
