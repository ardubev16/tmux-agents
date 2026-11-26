#!/usr/bin/env bats
# integrations/install.sh against a throwaway $HOME. Run: bats tests/test_install.bats

setup() {
    INTEGRATIONS="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../integrations"
    INSTALL="$INTEGRATIONS/install.sh"
    export HOME="$BATS_TEST_TMPDIR/home"
    mkdir -p "$HOME/.omp/agent" "$HOME/.claude"
    # A user hook on an event we also use, plus an unrelated setting.
    cat >"$HOME/.claude/settings.json" <<'EOF'
{ "model": "sonnet", "hooks": { "Stop": [{ "hooks": [{ "type": "command", "command": "mine.sh" }] }] } }
EOF
}

@test "install is idempotent and keeps the user's own hooks" {
    "$INSTALL" install omp claude
    "$INSTALL" install omp claude
    run jq -c '[.model, (.hooks.Stop | map(.hooks[0].command)), (.hooks.UserPromptSubmit | length)]' "$HOME/.claude/settings.json"
    ours=$(jq -c '.hooks.Stop[0].hooks[0].command' "$INTEGRATIONS/claude-hooks.json")
    [ "$output" = "[\"sonnet\",[\"mine.sh\",$ours],1]" ]
    [ -L "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
}

@test "uninstall restores the original settings and removes only our link" {
    "$INSTALL" install omp claude
    touch "$HOME/.omp/agent/extensions/other.ts"
    "$INSTALL" uninstall omp claude
    run jq -c . "$HOME/.claude/settings.json"
    [ "$output" = '{"model":"sonnet","hooks":{"Stop":[{"hooks":[{"type":"command","command":"mine.sh"}]}]}}' ]
    [ ! -e "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
    [ -e "$HOME/.omp/agent/extensions/other.ts" ]
}

@test "symlinked (externally managed) settings are left untouched" {
    mv "$HOME/.claude/settings.json" "$HOME/real.json"
    ln -s "$HOME/real.json" "$HOME/.claude/settings.json"
    run "$INSTALL" install claude
    [ "$status" -eq 0 ]
    [[ "$output" == *"is a symlink"* ]]
    run jq -c '.hooks | keys' "$HOME/real.json"
    [ "$output" = '["Stop"]' ]
}

@test "only the named harnesses are installed, and an unknown one installs nothing" {
    "$INSTALL" install claude
    [ ! -e "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
    run "$INSTALL" install omp nope
    [ "$status" -eq 1 ]
    [[ "$output" == *"Unknown harness: nope"* ]]
    [ ! -e "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
    run "$INSTALL" install
    [ "$status" -eq 1 ]
}

@test "every claude hook runs main.sh claude_hook, found in tmux's global environment" {
    [ "$(jq -r '[.hooks[][].hooks[].command] | unique | length' "$INTEGRATIONS/claude-hooks.json")" = 1 ]
    command=$(jq -r '.hooks.Stop[0].hooks[0].command' "$INTEGRATIONS/claude-hooks.json")
    mkdir -p "$BATS_TEST_TMPDIR/bin"
    printf '#!/bin/sh\necho "$@" >"%s/called"\n' "$BATS_TEST_TMPDIR" >"$BATS_TEST_TMPDIR/main.sh"
    printf '#!/bin/sh\necho "TMUX_AGENTS=%s/main.sh"\n' "$BATS_TEST_TMPDIR" >"$BATS_TEST_TMPDIR/bin/tmux"
    chmod +x "$BATS_TEST_TMPDIR/main.sh" "$BATS_TEST_TMPDIR/bin/tmux"

    # Always the path from tmux, also when the pane's environment has a stale one.
    run env TMUX_AGENTS=/stale/main.sh TMUX_PANE=%1 PATH="$BATS_TEST_TMPDIR/bin:$PATH" sh -c "$command"
    [ "$status" -eq 0 ]
    [ "$(cat "$BATS_TEST_TMPDIR/called")" = "claude_hook" ]

    # Outside tmux the hook is a silent no-op.
    run env -u TMUX_AGENTS -u TMUX_PANE sh -c "$command"
    [ "$status" -eq 0 ]
    [ -z "$output" ]
}
