#!/usr/bin/env bats
# ensure_worktree against throwaway git repos. Run: bats tests/test_worktree.bats

setup() {
    SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../scripts"
    PROJECT="$BATS_TEST_TMPDIR/project"
    REPO="$PROJECT/repo"
    git init -q -b main "$REPO"
    git -C "$REPO" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
}

worktree() { run "$SCRIPTS_DIR/worktree.sh" "$@" </dev/null; }

@test "new branch: created off HEAD under <project>/.worktrees/<repo>/<branch>" {
    worktree "$REPO" feat/x
    [ "$status" -eq 0 ]
    [ "$output" = "$PROJECT/.worktrees/repo/feat/x" ]
    [ "$(git -C "$output" branch --show-current)" = feat/x ]
}

@test "existing local branch: checked out, not recreated" {
    git -C "$REPO" branch old
    worktree "$REPO" old
    [ "$status" -eq 0 ]
    [ "$(git -C "$output" branch --show-current)" = old ]
}

@test "already checked out: reuses that checkout (main repo included)" {
    worktree "$REPO" main
    [ "$output" = "$REPO" ]
    worktree "$REPO" feat
    first="$output"
    worktree "$REPO" feat
    [ "$output" = "$first" ]
}

@test "called from inside a worktree: resolves to the main repo, not .git/worktrees" {
    worktree "$REPO" a
    worktree "$output" b
    [ "$status" -eq 0 ]
    [ "$output" = "$PROJECT/.worktrees/repo/b" ]
}

@test "custom base dir" {
    worktree "$REPO" c "$BATS_TEST_TMPDIR/wt"
    [ "$output" = "$BATS_TEST_TMPDIR/wt/repo/c" ]
}
