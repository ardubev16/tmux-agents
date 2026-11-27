#!/usr/bin/env bats
# main.sh's agent list and state reporting against a stub tmux. Run: bats tests/test_main.bats
# shellcheck disable=SC2016 # the stub scripts are written with a literal $

bats_require_minimum_version 1.5.0 # run !

setup() {
    MAIN="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../main.sh"
    export LOG="$BATS_TEST_TMPDIR/calls" PANES="$BATS_TEST_TMPDIR/panes"
    mkdir -p "$BATS_TEST_TMPDIR/bin"
    cat >"$BATS_TEST_TMPDIR/bin/tmux" <<'EOF'
#!/bin/sh
echo "tmux $*" >>"$LOG"
case "$1" in
show-option) case "$*" in *default-shell*) echo /bin/zsh ;; esac ;;
list-panes) cat "$PANES" ;;
display-message) echo "$VISIBLE" ;;
esac
EOF
    printf '#!/bin/sh\necho "notify-send $*" >>"$LOG"\n' >"$BATS_TEST_TMPDIR/bin/notify-send"
    chmod +x "$BATS_TEST_TMPDIR/bin/"*
    export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}

@test "list_agents: blocked, unread done, done or just opened, working; skips non-agents and panes back at the shell" {
    printf '%s\t%s\t%s\t%s\t%s\t%s\t\t%s\t%s\n' \
        %1 working s:1.0 omp /a '' '' 100 \
        %2 "done" s:2.0 claude /b '' '' 100 \
        %3 blocked s:3.0 claude /c '' '' 100 \
        %4 "done" s:4.0 omp /d '' 1 100 \
        %5 '' s:5.0 zsh /tmp '' '' '' \
        %6 working s:6.0 zsh /tmp '' '' 100 \
        %7 '' s:7.0 omp /e /e.jsonl '' '' >"$PANES"
    run "$MAIN" list_agents
    [ "$status" -eq 0 ]
    [ "$(cut -f1 <<<"$output" | tr '\n' ' ')" = "%3 %4 %2 %7 %1 " ]
    [ "$(cut -f9 <<<"$output" | sort -u | tr '\n' ' ')" = " 100 " ]
}

@test "list_agents: within a section, the one waiting longest first, unknown wait last" {
    printf '%s\t%s\t%s\t%s\t%s\t%s\t\t%s\t%s\n' \
        %1 blocked s:1.0 omp /a '' '' 300 \
        %2 blocked s:2.0 omp /b '' '' 100 \
        %3 "done" s:3.0 omp /c '' '' '' \
        %4 "done" s:4.0 omp /d '' '' 200 >"$PANES"
    run "$MAIN" list_agents
    [ "$(cut -f1 <<<"$output" | tr '\n' ' ')" = "%2 %1 %4 %3 " ]
}

@test "set_note: tabs and newlines become spaces, an empty note is removed" {
    run "$MAIN" set_note %1 $'wait for\tthe API\ndecision'
    [ "$status" -eq 0 ]
    grep -qx 'tmux set-option -p -t %1 @tmux_agents_note wait for the API decision' "$LOG"
    run "$MAIN" set_note %1 '  '
    grep -qx 'tmux set-option -pu -t %1 @tmux_agents_note' "$LOG"
}

@test "status_bar: only counters above 0 by default, every counter with hide_zero off" {
    printf '%s\t%s\t%s\t%s\t%s\t\t\t\t\n' %1 working s:1.0 omp /a %2 working s:2.0 omp /b >"$PANES"
    run "$MAIN" status_bar green yellow red off
    [ "$output" = "#[fg=green] ○ 0 #[fg=yellow] ⦿ 2 #[fg=red] ● 0 " ]
    run "$MAIN" status_bar green yellow red
    [ "$output" = "#[fg=yellow] ⦿ 2 " ]
    : >"$PANES"
    run "$MAIN" status_bar green yellow red
    [ -z "$output" ]
}

@test "state: the clock restarts only when the state changes" {
    TMUX_PANE=%1 VISIBLE=1 run "$MAIN" state working
    grep -q 'set-option -p -t %1 @tmux_agents_since [0-9]' "$LOG"

    # The stub reports the pane's current state as working now.
    : >"$LOG"
    printf '#!/bin/sh\necho "tmux $*" >>"$LOG"\ncase "$*" in *@tmux_agents_state*) echo working ;; esac\n' >"$BATS_TEST_TMPDIR/bin/tmux"
    TMUX_PANE=%1 VISIBLE=1 run "$MAIN" state working
    [ "$status" -eq 0 ]
    run ! grep -q @tmux_agents_since "$LOG"
}

@test "state done: marks unread and notifies only when the pane is not on screen" {
    TMUX_PANE=%1 VISIBLE=0 run "$MAIN" state "done"
    [ "$status" -eq 0 ]
    grep -q 'set-option -p -t %1 @tmux_agents_unread 1' "$LOG"
    grep -q 'notify-send .*finished' "$LOG"

    : >"$LOG"
    TMUX_PANE=%1 VISIBLE=1 run "$MAIN" state "done"
    [ "$status" -eq 0 ]
    grep -q 'set-option -pu -t %1 @tmux_agents_unread' "$LOG"
    run ! grep -q notify-send "$LOG"
}

@test "notify: a failing notify-send neither fails the hook nor falls back to the status line" {
    printf '#!/bin/sh\nexit 1\n' >"$BATS_TEST_TMPDIR/bin/notify-send"
    TMUX_PANE=%1 VISIBLE=0 run "$MAIN" state "done"
    [ "$status" -eq 0 ]
    run ! grep -q 'display-message -c' "$LOG"
}

@test "notify on macOS: osascript gets the message as an argument, not as script" {
    printf '#!/bin/sh\necho Darwin\n' >"$BATS_TEST_TMPDIR/bin/uname"
    printf '#!/bin/sh\nfor a; do echo "osascript arg: $a" >>"$LOG"; done\n' >"$BATS_TEST_TMPDIR/bin/osascript"
    chmod +x "$BATS_TEST_TMPDIR/bin/"*
    TMUX_PANE=%1 VISIBLE=0 run "$MAIN" state "done"
    [ "$status" -eq 0 ]
    grep -qx 'osascript arg: 0 finished' "$LOG" # the stub names every pane "0"
    run ! grep -q notify-send "$LOG"
}

@test "claude_hook: each Claude Code event sets the matching state" {
    for case in SessionStart:transcript UserPromptSubmit:working PostToolUse:working PreToolUse:blocked \
        Notification:blocked Stop:done; do
        : >"$LOG"
        printf '{"hook_event_name":"%s","transcript_path":"/t.jsonl"}' "${case%:*}" |
            TMUX_PANE=%1 VISIBLE=1 "$MAIN" claude_hook
        if [ "${case#*:}" = transcript ]; then
            grep -q 'set-option -p -t %1 @tmux_agents_transcript /t.jsonl' "$LOG"
        else
            grep -q "set-option -p -t %1 @tmux_agents_state ${case#*:}" "$LOG"
        fi
    done
}

@test "dashboard: downloads the release for this version and platform once, then runs it" {
    # A copy of the plugin, so the real bin/ stays untouched.
    local -r plugin="$BATS_TEST_TMPDIR/plugin"
    mkdir -p "$plugin"
    cp "$MAIN" "$plugin/"
    printf '#!/bin/sh\ncase $1 in -s) echo Linux ;; -m) echo aarch64 ;; esac\n' >"$BATS_TEST_TMPDIR/bin/uname"
    cat >"$BATS_TEST_TMPDIR/bin/curl" <<'EOF'
#!/bin/sh
echo "curl $*" >>"$LOG"
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
printf '#!/bin/sh\necho "dashboard $*"\n' >"$out"
EOF
    chmod +x "$BATS_TEST_TMPDIR/bin/"*
    version=$(sed -n 's/^declare -r version="\(.*\)".*/\1/p' "$plugin/main.sh")

    run "$plugin/main.sh" dashboard
    [ "$status" -eq 0 ]
    [[ "$output" == *"dashboard $plugin/main.sh" ]]
    grep -q "releases/download/v$version/tmux-agents_linux_arm64" "$LOG"

    : >"$LOG"
    run "$plugin/main.sh" dashboard
    [[ "$output" == "dashboard $plugin/main.sh" ]]
    run ! grep -q curl "$LOG"
}

@test "dashboard with @tmux_agents_dashboard_build on: builds from the checkout, rebuilds after source changes" {
    local -r plugin="$BATS_TEST_TMPDIR/plugin"
    mkdir -p "$plugin/cmd" "$plugin/internal"
    touch "$plugin/go.mod" "$plugin/go.sum"
    cp "$MAIN" "$plugin/"
    printf '#!/bin/sh\necho "tmux $*" >>"$LOG"\ncase "$*" in *@tmux_agents_dashboard_build*) echo on ;; esac\n' \
        >"$BATS_TEST_TMPDIR/bin/tmux"
    cat >"$BATS_TEST_TMPDIR/bin/go" <<'EOF'
#!/bin/sh
echo "go $*" >>"$LOG"
printf '#!/bin/sh\necho "dashboard $*"\n' >"$3"
chmod +x "$3"
EOF
    printf '#!/bin/sh\necho "curl $*" >>"$LOG"; exit 1\n' >"$BATS_TEST_TMPDIR/bin/curl"
    chmod +x "$BATS_TEST_TMPDIR/bin/"*

    run "$plugin/main.sh" dashboard
    [[ "$output" == *"dashboard $plugin/main.sh" ]]
    [ "$(grep -c '^go build' "$LOG")" = 1 ]

    run "$plugin/main.sh" dashboard # nothing changed: no rebuild
    [ "$(grep -c '^go build' "$LOG")" = 1 ]

    sleep 1 && touch "$plugin/cmd/main.go"
    run "$plugin/main.sh" dashboard
    [ "$(grep -c '^go build' "$LOG")" = 2 ]
    run ! grep -q '^curl' "$LOG"
}
