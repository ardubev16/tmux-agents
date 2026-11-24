#!/usr/bin/env bats
# integrations/install.sh against a throwaway $HOME. Run: bats tests/test_install.bats

setup() {
    INSTALL="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/../integrations/install.sh"
    export HOME="$BATS_TEST_TMPDIR/home"
    mkdir -p "$HOME/.omp/agent" "$HOME/.claude"
    # A user hook on an event we also use, plus an unrelated setting.
    cat >"$HOME/.claude/settings.json" <<'EOF'
{ "model": "sonnet", "hooks": { "Stop": [{ "hooks": [{ "type": "command", "command": "mine.sh" }] }] } }
EOF
}

@test "install is idempotent and keeps the user's own hooks" {
    "$INSTALL" install
    "$INSTALL" install
    run jq -c '[.model, (.hooks.Stop | map(.hooks[0].command)), (.hooks.UserPromptSubmit | length)]' "$HOME/.claude/settings.json"
    [ "$output" = '["sonnet",["mine.sh","[ -z \"$TMUX_AGENTS\" ] || \"$TMUX_AGENTS\" state done"],1]' ]
    [ -L "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
}

@test "uninstall restores the original settings and removes only our link" {
    "$INSTALL" install
    touch "$HOME/.omp/agent/extensions/other.ts"
    "$INSTALL" uninstall
    run jq -c . "$HOME/.claude/settings.json"
    [ "$output" = '{"model":"sonnet","hooks":{"Stop":[{"hooks":[{"type":"command","command":"mine.sh"}]}]}}' ]
    [ ! -e "$HOME/.omp/agent/extensions/tmux-agents.ts" ]
    [ -e "$HOME/.omp/agent/extensions/other.ts" ]
}

@test "symlinked (externally managed) settings are left untouched" {
    mv "$HOME/.claude/settings.json" "$HOME/real.json"
    ln -s "$HOME/real.json" "$HOME/.claude/settings.json"
    run "$INSTALL" install
    [ "$status" -eq 0 ]
    [[ "$output" == *"is a symlink"* ]]
    run jq -c '.hooks | keys' "$HOME/real.json"
    [ "$output" = '["Stop"]' ]
}
