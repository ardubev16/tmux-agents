# tmux-herdr

A tmux plugin for [herdr](https://herdr.dev/), a persistent multiplexer for coding
agents like Claude Code. It adds a status bar indicator for agent state (idle/working/blocked)
plus keybindings to open the dashboard, attach to an agent, or spin up a new one.
See README.md for user-facing usage, keybindings, and configuration options.

## Architecture

- `main.tmux` — plugin entrypoint sourced by tmux/TPM. Reads `@tmux_herdr_*` options,
  binds keys, wires up the status bar placeholder.
- `main.sh` — the only file allowed to call `tmux` directly. Dispatches subcommands
  (`agent_dashboard`, `new_agent`, `new_agent_branch`) and handles pane/session logic
  (finding, reusing, splitting panes). All tmux interaction lives here.
- `scripts/` — everything here must run standalone, with no dependency on tmux being
  present or active.
  - `_common.sh` — generic helpers with no knowledge of herdr (git metadata, fzf branch
    picker, error trap/`die`). Sourced by every other script in this directory.
  - `_herdr.sh` — the herdr API layer. All `herdr` CLI calls, `_func` (private) and
    `func` (public) wrappers live here (session bootstrap, workspace/worktree/agent
    creation, `jq` parsing of `herdr` JSON output). This is the only file that should
    know about `herdr`'s CLI shape.
  - `init.sh`, `agent_dashboard.sh`, `new_agent.sh`, `select_branch.sh`, `status_bar.sh`
    — task-specific scripts. Each sources `_common.sh` only, and calls into `_herdr.sh`
    through its public functions. They must not embed herdr-specific logic (e.g. raw
    `herdr` invocations or `jq` filters over herdr output) themselves — that belongs in
    `_herdr.sh`.

Rule of thumb: `main.sh` = tmux; `scripts/_herdr.sh` = herdr; everything else in
`scripts/` = generic, tmux-free, herdr-agnostic glue that consumes `_herdr.sh`'s API.

Note: as of this writing, herdr-specific functions (`_herdr`, `_herdr_interactive`,
`wait_for_server`, `_create_workspace`, `_create_worktree`, `_start_agent`, `new_agent`,
etc.) still live in `scripts/_common.sh`. New herdr-related code should go in
`scripts/_herdr.sh` instead; migrate existing functions there as you touch them.

## Testing

Use [bats-core](https://github.com/bats-core/bats-core) (`bats`) for tests. See
`tests/test_common.bats` for the pattern: stub `tmux`/`herdr` as shell functions inside a
`bash -c` snippet, source the script under test standalone, and assert on `$status`/
`$output` from bats' `run`. Run with `bats tests/`.
