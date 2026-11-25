#!/usr/bin/env bash

set -e

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

declare -r agents_status_placeholder='\#{tmux_agents_status}'

declare -r key_dashboard_config='@tmux_agents_key_dashboard'
declare -r key_worktree_config='@tmux_agents_key_worktree'

declare -r debug_config='@tmux_agents_debug'

declare -r background_config='@tmux_agents_status_background'
declare -r done_foreground_config='@tmux_agents_status_done_foreground'
declare -r working_foreground_config='@tmux_agents_status_working_foreground'
declare -r blocked_foreground_config='@tmux_agents_status_blocked_foreground'

function tmux_option() {
    local -r option=$(tmux show-option -gqv "$1")
    local -r fallback="$2"
    echo "${option:-$fallback}"
}

function init_tmux_agents() {
    local -r \
        key_dashboard=$(tmux_option "$key_dashboard_config" "a") \
        key_worktree=$(tmux_option "$key_worktree_config" "W") \
        debug=$(tmux_option "$debug_config" "off")

    tmux set-environment -g TMUX_AGENTS_DEBUG "$debug"
    # Agent hooks call "$TMUX_AGENTS state <state>"; new panes inherit it, older ones read it via show-environment.
    tmux set-environment -g TMUX_AGENTS "$CURRENT_DIR/main.sh"

    tmux bind-key "$key_dashboard" run-shell "$CURRENT_DIR/main.sh agent_dashboard"
    tmux bind-key "$key_worktree" run-shell "$CURRENT_DIR/main.sh new_worktree"
}

function init_tmux_agents_status() {
    local -r \
        background=$(tmux_option "$background_config" "colour238") \
        done_foreground=$(tmux_option "$done_foreground_config" "green") \
        working_foreground=$(tmux_option "$working_foreground_config" "yellow") \
        blocked_foreground=$(tmux_option "$blocked_foreground_config" "red")

    local -r agents_status="#[default]#[bg=$background]#(\"$CURRENT_DIR/main.sh\" status_bar \"$done_foreground\" \"$working_foreground\" \"$blocked_foreground\")#[default]"

    local -r status_left_value="$(tmux_option "status-left")"
    tmux set-option -gq "status-left" "${status_left_value/$agents_status_placeholder/$agents_status}"

    local -r status_right_value="$(tmux_option "status-right")"
    tmux set-option -gq "status-right" "${status_right_value/$agents_status_placeholder/$agents_status}"
}

init_tmux_agents
init_tmux_agents_status
