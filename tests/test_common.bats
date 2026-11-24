#!/usr/bin/env bats
# Self-check for the debug/error-handling helpers in scripts/_common.sh.
# No tmux binary required - _common.sh has no knowledge of it.
# Run: bats tests/test_common.bats

setup() {
    SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../scripts"
}

# Runs $2 in a throwaway bash subprocess with debug/stdin set, after sourcing
# _common.sh. Uses bats' run, so callers assert on $status/$output.
run_case() {
    local -r debug="$1" snippet="$2"
    TMUX_AGENTS_DEBUG="$debug" run bash -c '
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
