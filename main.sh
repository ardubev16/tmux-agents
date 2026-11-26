#!/usr/bin/env bash

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

MAIN="$(realpath "${BASH_SOURCE[0]}")"
declare -r MAIN
# The release this checkout belongs to; release-please bumps it, and the dashboard binary is downloaded for it.
declare -r version="0.1.0" # x-release-please-version
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
    if [[ $state == "done" && $visible != 1 ]]; then
        tmux set-option -p -t "$TMUX_PANE" "$unread_option" 1
    elif [[ $state != clear ]]; then
        tmux set-option -pu -t "$TMUX_PANE" "$unread_option"
    fi
    if [[ ($state == "done" || $state == blocked) && $visible != 1 ]]; then
        notify "$(agent_name "$TMUX_PANE") $([[ $state == "done" ]] && echo finished || echo 'needs input')"
    fi

    local client
    while read -r client; do
        tmux refresh-client -S -t "$client"
    done < <(tmux list-clients -F '#{client_name}')
}

# The session name the harness gave the agent (omp: title line, Claude Code: last ai-title), else its pane. Same
# rule as the dashboard's name() and transcript.Title, which read it incrementally instead.
function agent_name() {
    local -r transcript=$(tmux show-option -pqv -t "$1" "$transcript_option")
    local name=""
    [[ -f $transcript ]] && name=$(grep -h -e '"type":"title"' -e '"type":"ai-title"' "$transcript" |
        tail -n 1 | jq -r '.title // .aiTitle // empty' 2>/dev/null)
    echo "${name:-$(tmux display-message -p -t "$1" '#{session_name}:#{window_index}.#{pane_index}')}"
}

# @tmux_agents_notify: system (desktop notification) | tmux (status line message) | off.
function notify() {
    local -r message="$1" mode=$(tmux_option @tmux_agents_notify system)
    case $mode in
    system)
        # Best effort: a missing notifier or notification daemon must not fail the agent's hook.
        if [[ $(uname -s) == Darwin ]]; then
            # The message goes in as an argument, never into the script, so quotes in a session name are harmless.
            osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "tmux-agents"' \
                -e 'end run' "$message" >/dev/null 2>&1 || true
        else
            notify-send --app-name=tmux-agents tmux-agents "$message" 2>/dev/null || true
        fi
        ;;
    tmux)
        local client
        while read -r client; do
            tmux display-message -c "$client" -d 5000 "tmux-agents: $message"
        done < <(tmux list-clients -F '#{client_name}')
        ;;
    esac
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

# Every Claude Code hook (integrations/claude-hooks.json) runs this with the event as JSON on stdin, so what
# each event means lives here and updates with the plugin; only the events and matchers live in the hooks.
function claude_hook() {
    local event transcript
    IFS=$'\t' read -r event transcript < <(jq -r '[.hook_event_name, .transcript_path // ""] | @tsv')
    case $event in
    SessionStart) set_session "$transcript" ;;
    UserPromptSubmit | PostToolUse) set_state working ;;
    PreToolUse | Notification) set_state blocked ;; # AskUserQuestion; permission and MCP input dialogs
    Stop) set_state "done" ;;
    SessionEnd) set_state clear ;;
    esac
}

# Called by tmux hooks when a pane is shown: clears its unread mark.
function seen() {
    tmux set-option -pu -t "$1" "$unread_option" 2>/dev/null || true
}

# One agent per line (panes with a state, or a session reported but no prompt yet), blocked first, then unread
# done, then idle: pane_id \t state \t target \t command \t path \t transcript \t socket \t unread \t since (epoch
# seconds the state began)
function list_agents() {
    local -r shell="$(basename "$(tmux show-option -gqv default-shell)")"
    # shortcut: an agent killed without its exit hook keeps its state until its pane is back at the shell prompt.
    tmux list-panes -a -F "#{pane_id}	#{$state_option}	#{session_name}:#{window_index}.#{pane_index}	#{pane_current_command}	#{pane_current_path}	#{$transcript_option}	#{$socket_option}	#{$unread_option}	#{$since_option}" |
        awk -F '\t' -v shell="$shell" '
            BEGIN { OFS = "\t"; rank["blocked"] = 0; rank["done"] = 2; rank[""] = 2; rank["working"] = 3 }
            ($2 != "" || $6 != "") && $4 != shell {
                r = ($2 == "done" && $8 != "") ? 1 : rank[$2]
                print r, $0
            }' |
        sort -s -n -k1,1 | cut -f2-
}

# hide_zero=on leaves out counters at 0, so nothing shows while no agent runs.
function status_bar() {
    local -r done_foreground="$1" working_foreground="$2" blocked_foreground="$3" hide_zero="${4:-on}"
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

# Popup body: runs the dashboard released with this version of the plugin, downloading it on first use and after
# an update. With @tmux_agents_dashboard_build 'on', builds it from this checkout with Go instead (and again
# whenever the Go sources change), for working on the plugin itself.
function dashboard() {
    local -r root="$(dirname "$MAIN")"
    local -r dir="$root/bin"
    if [[ $(tmux_option @tmux_agents_dashboard_build off) == on ]]; then
        local -r built="$dir/tmux-agents-local"
        if [[ ! -x $built || -n $(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$built" -print -quit) ]]; then
            echo "Building the dashboard…"
            mkdir -p "$dir"
            (cd "$root" && go build -o "$built" ./cmd/tmux-agents) || die "tmux-agents: building the dashboard failed."
        fi
        exec "$built" "$MAIN"
    fi
    local -r bin="$dir/tmux-agents-$version"
    if [[ ! -x $bin ]]; then
        local os arch
        os=$(uname -s | tr '[:upper:]' '[:lower:]')
        case $(uname -m) in
        x86_64 | amd64) arch=amd64 ;;
        aarch64 | arm64) arch=arm64 ;;
        *) die "tmux-agents: no dashboard is built for $(uname -m)." ;;
        esac
        local -r url="https://github.com/ardubev16/tmux-agents/releases/download/v$version/tmux-agents_${os}_$arch"
        echo "Downloading the dashboard $version…"
        rm -rf "$dir" # earlier versions
        mkdir -p "$dir"
        curl -fL --progress-bar -o "$bin.part" "$url" || die "tmux-agents: downloading the dashboard failed: $url"
        chmod +x "$bin.part"
        mv "$bin.part" "$bin"
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

# Prints the main checkout of the repo containing $1, also when $1 is inside a worktree.
function repo_root() {
    local git_dir
    git_dir=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || {
        echo "Not a git repo." >&2
        return 1
    }
    dirname "$git_dir"
}

# Prints "open <branch>" for the picked branch or the typed query (a new branch), "remove <branch>" when
# ctrl-x was pressed on one; fails when cancelled (Esc/Ctrl-C).
function select_branch() {
    local branches fzf_out
    branches=$(git -C "$1" branch)
    # Exit 1 means no match: the query names a new branch. Anything else (130) is a cancel; return explicitly,
    # since callers run this in `|| …`, where errexit does not apply.
    fzf_out=$({
        grep '^\*' <<<"$branches"
        grep -v '^\*' <<<"$branches"
    } | fzf --print-query --expect=ctrl-x --prompt="⎇ branch: " --header="enter: open · ctrl-x: remove its worktree") ||
        [[ $? == 1 ]] || return 1

    local -r query=$(sed -n '1p' <<<"$fzf_out") key=$(sed -n '2p' <<<"$fzf_out") selection=$(sed -n '3p' <<<"$fzf_out")
    if [[ $key == ctrl-x ]]; then
        echo "remove ${selection:2}"
    elif [[ -n $selection ]]; then
        echo "open ${selection:2}"
    else
        echo "open $query"
    fi
}

# Prints the checkout of repo $1 that has branch $2 checked out, if any.
function worktree_of() {
    git -C "$1" worktree list --porcelain |
        awk -v ref="branch refs/heads/$2" '/^worktree /{ wt = substr($0, 10) } $0 == ref { print wt; exit }'
}

# Prints the checkout path for $branch, creating a worktree under
# ${base_dir:-<project>/.worktrees}/<repo>/<branch> if none has it checked out.
function ensure_worktree() {
    local -r path="$1" branch="$2" base_dir="${3:-}"
    local repo
    repo=$(repo_root "$path")

    local existing
    existing=$(worktree_of "$repo" "$branch")
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

# Removes the worktree that has $branch checked out and prints its path. Never the main checkout; git refuses
# a worktree with uncommitted changes. Returns explicitly: callers run this in `if`, where errexit does not apply.
function remove_worktree() {
    local -r path="$1" branch="$2"
    local repo wt
    repo=$(repo_root "$path") || return 1
    wt=$(worktree_of "$repo" "$branch")
    if [[ -z $wt || $wt == "$repo" ]]; then
        echo "$branch has no worktree to remove (the main checkout stays)." >&2
        return 1
    fi
    git -C "$repo" worktree remove "$wt" >&2 || return 1
    echo "$wt"
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

# Sourced (by the tests), main.sh only defines its functions; run, it is the command below.
[[ ${BASH_SOURCE[0]} == "$0" ]] || return 0

if [[ $# -lt 1 ]]; then
    die "Usage: main.sh <command> [args...]"
fi

command="$1"
shift

case "$command" in
state) set_state "$@" ;;
session) set_session "$@" ;;
claude_hook) claude_hook "$@" ;;
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
