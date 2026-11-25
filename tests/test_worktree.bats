#!/usr/bin/env bats
# ensure_worktree and select_branch against throwaway git repos. Run: bats tests/test_worktree.bats

setup() {
    SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../scripts"
    PROJECT="$BATS_TEST_TMPDIR/project"
    REPO="$PROJECT/repo"
    git init -q -b main "$REPO"
    git -C "$REPO" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
}

worktree() { run bash -c '. "$1/_common.sh"; shift; ensure_worktree "$@"' _ "$SCRIPTS_DIR" "$@" </dev/null; }

# select_branch with fzf stubbed: $1 is what fzf prints, $2 its exit status.
pick() {
    run bash -c '. "$1/_common.sh"; out=$2 code=$3; fzf() { cat >/dev/null; printf "%b" "$out"; return "$code"; }
        select_branch "$4" || echo cancelled' \
        _ "$SCRIPTS_DIR" "$1" "$2" "$REPO"
}

@test "select_branch: picked branch, typed new branch, ctrl-x, cancel" {
    pick 'quer\n\n  feat/y\n' 0
    [ "$output" = "open feat/y" ]
    pick 'new/z\n\n' 1
    [ "$output" = "open new/z" ]
    pick '\nctrl-x\n+ feat/y\n' 0
    [ "$output" = "remove feat/y" ]
    pick '' 130
    [ "$output" = cancelled ]
}

@test "remove_worktree: removes a clean worktree, never the main checkout or one with changes" {
    worktree "$REPO" gone
    wt="$output"
    run bash -c '. "$1/_common.sh"; remove_worktree "$2" gone' _ "$SCRIPTS_DIR" "$REPO"
    [ "$status" -eq 0 ]
    [ "$output" = "$wt" ]
    [ ! -e "$wt" ]

    run bash -c '. "$1/_common.sh"; remove_worktree "$2" main || echo refused' _ "$SCRIPTS_DIR" "$REPO"
    [[ "$output" == *refused ]]

    worktree "$REPO" dirty
    touch "$output/new-file"
    run bash -c '. "$1/_common.sh"; remove_worktree "$2" dirty || echo refused' _ "$SCRIPTS_DIR" "$REPO"
    [[ "$output" == *refused ]]
    [ -e "$PROJECT/.worktrees/repo/dirty/new-file" ]
}

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
