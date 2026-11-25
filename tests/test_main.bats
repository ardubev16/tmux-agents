#!/usr/bin/env bats
# main.sh's agent list and state reporting against a stub tmux. Run: bats tests/test_main.bats

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

@test "list_agents: blocked, unread done, done, working; skips non-agents and panes back at the shell" {
    printf '%s\t%s\t%s\t%s\t%s\t\t\t%s\t%s\n' \
        %1 working s:1.0 omp /a '' 100 \
        %2 done s:2.0 claude /b '' 100 \
        %3 blocked s:3.0 claude /c '' 100 \
        %4 done s:4.0 omp /d 1 100 \
        %5 '' s:5.0 zsh /tmp '' '' \
        %6 working s:6.0 zsh /tmp '' 100 >"$PANES"
    run "$MAIN" list_agents
    [ "$status" -eq 0 ]
    [ "$(cut -f1 <<<"$output" | tr '\n' ' ')" = "%3 %4 %2 %1 " ]
    [ "$(cut -f9 <<<"$output" | sort -u)" = 100 ]
}

@test "status_bar: every counter, or with hide_zero only the ones above 0" {
    printf '%s\t%s\t%s\t%s\t%s\t\t\t\t\n' %1 working s:1.0 omp /a %2 working s:2.0 omp /b >"$PANES"
    run "$MAIN" status_bar green yellow red
    [ "$output" = "#[fg=green] ○ 0 #[fg=yellow] ⦿ 2 #[fg=red] ● 0 " ]
    run "$MAIN" status_bar green yellow red on
    [ "$output" = "#[fg=yellow] ⦿ 2 " ]
    : >"$PANES"
    run "$MAIN" status_bar green yellow red on
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
    ! grep -q @tmux_agents_since "$LOG"
}

@test "state done: marks unread and notifies only when the pane is not on screen" {
    TMUX_PANE=%1 VISIBLE=0 run "$MAIN" state done
    [ "$status" -eq 0 ]
    grep -q 'set-option -p -t %1 @tmux_agents_unread 1' "$LOG"
    grep -q 'notify-send .*finished' "$LOG"

    : >"$LOG"
    TMUX_PANE=%1 VISIBLE=1 run "$MAIN" state done
    [ "$status" -eq 0 ]
    grep -q 'set-option -pu -t %1 @tmux_agents_unread' "$LOG"
    ! grep -q notify-send "$LOG"
}
