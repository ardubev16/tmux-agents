#!/usr/bin/env bats
# Self-check for the herdr API layer in scripts/_herdr.sh.
# No tmux/herdr binaries required - herdr is mocked. Run: bats tests/test_herdr.bats

setup() {
    SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../scripts"
}

# Runs $2 in a throwaway bash subprocess with herdr mocked and stdin pinned
# to /dev/null, after sourcing _common.sh and _herdr.sh. Uses bats' run, so
# callers assert on $status/$output.
run_case() {
    local -r debug="$1" snippet="$2"
    TMUX_HERDR_DEBUG="$debug" run bash -c '
        function herdr() { echo "{\"result\":true}"; }
        . "'"$SCRIPTS_DIR"'/_common.sh"
        . "'"$SCRIPTS_DIR"'/_herdr.sh"
        '"$snippet"'
    ' </dev/null
}

@test "debug on: _herdr dumps raw I/O" {
    run_case on '_herdr agent list'
    [[ "$output" == *"result"* ]]
}

@test "_cleanup_extra_panes: pane close result does not leak to caller's stdout" {
    run_case off '
        function herdr() {
            if [[ "$1" == pane && "$2" == list ]]; then
                echo "{\"result\":{\"panes\":[{\"pane_id\":\"p1\"},{\"pane_id\":\"p2\"}]}}"
            elif [[ "$1" == pane && "$2" == close ]]; then
                echo "{\"id\":\"cli:pane:close\",\"result\":{\"type\":\"ok\"}}"
            fi
        }
        out=$(_cleanup_extra_panes ws1 p1)
        echo "captured=[$out]"
    '
    [[ "$output" != *"cli:pane:close"* ]]
    [[ "$output" == *"captured=[]"* ]]
}
