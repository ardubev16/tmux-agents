#!/usr/bin/env bats
# Self-check for the debug/error-handling helpers in main.sh, sourced without tmux.
# Run: bats tests/test_common.bats

setup() {
    MAIN="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../main.sh"
}

# Runs $2 in a throwaway bash subprocess with debug/stdin set, after sourcing
# main.sh. Uses bats' run, so callers assert on $status/$output.
run_case() {
    local -r debug="$1" snippet="$2"
    TMUX_AGENTS_DEBUG="$debug" run bash -c '
        . "'"$MAIN"'"
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

@test "soft-fail: repo_root reports Not a git repo, then trap fires" {
    run_case off 'repo_root /nonexistent-path-xyz'
    [[ "$output" == *"Not a git repo."* ]]
    [[ "$output" == *"unexpected error"* ]]
}
