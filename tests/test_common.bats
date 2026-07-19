#!/usr/bin/env bats
# Self-check for the debug/error-handling helpers in scripts/_common.sh.
# No tmux/herdr binaries required - both are mocked. Run: bats tests/test_common.bats

setup() {
    SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../scripts"
}

# Runs $2 in a throwaway bash subprocess with tmux/herdr mocked and stdin
# pinned to /dev/null, after sourcing _common.sh. Uses bats' run, so callers
# assert on $status/$output.
run_case() {
    local -r debug="$1" snippet="$2"
    MOCK_DEBUG="$debug" run bash -c '
        function tmux() {
            [[ "$1" == show-option && "$3" == "@tmux_herdr_debug" ]] && echo "$MOCK_DEBUG"
            return 0
        }
        function herdr() { echo "{\"result\":true}"; }
        . "'"$SCRIPTS_DIR"'/_common.sh"
        '"$snippet"'
    ' </dev/null
}

@test "debug off: unexpected error gets generic message and exits 1" {
    run_case off false
    [ "$status" -eq 1 ]
    [[ "$output" == *"unexpected error"* ]]
}

@test "debug on: unexpected error shows line/command" {
    run_case on false
    [[ "$output" == *"at line"* ]]
}

@test "errtrace: failure inside a function still trips the trap" {
    run_case on 'function inner() { false; }; inner'
    [[ "$output" == *"at line"* ]]
}

@test "die(): prints its own message and exits 1" {
    run_case off 'die "custom problem"'
    [ "$status" -eq 1 ]
    [[ "$output" == *"custom problem"* ]]
}

@test "soft-fail: get_repo_metadata reports Not a git repo, then trap fires" {
    run_case off 'get_repo_metadata a b c /nonexistent-path-xyz'
    [[ "$output" == *"Not a git repo."* ]]
    [[ "$output" == *"unexpected error"* ]]
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
