# tmux-agents

A tmux plugin that tracks coding agents (omp, Claude Code, …) running in ordinary tmux panes:
status bar counters (done/working/blocked), a bell on state changes, a popup dashboard to
read/switch/reply, and a keybinding to open a branch worktree as its own session.
See README.md for user-facing usage, keybindings, and configuration options.

## Architecture

- `main.tmux` — plugin entrypoint sourced by tmux/TPM. Reads `@tmux_agents_*` options,
  binds keys, exports `TMUX_AGENTS` (path to `main.sh`) for agent hooks, wires up the
  status bar placeholder.
- `main.sh` — the only file allowed to call `tmux` directly. Dispatches subcommands
  (`state`, `session`, `status_bar`, `agent_dashboard` and its callbacks, `new_worktree`).
  Per-pane options: `@tmux_agents_state`, plus what `session` reports: the agent's session
  file (`@tmux_agents_transcript`) and, for omp, its socket (`@tmux_agents_socket`).
- `scripts/` — everything here must run standalone, with no dependency on tmux being
  present or active.
  - `_common.sh` — generic helpers (git metadata, worktree creation, fzf branch picker,
    error trap/`die`). Sourced by every other script in this directory.
  - `dashboard.sh`, `select_branch.sh`, `worktree.sh` — thin entry points run in popups or
    by `main.sh`. `dashboard.sh` launches the Python dashboard via `uv run`.
- `src/tmux_agents/` — the dashboard TUI (Textual, uv package). Calls back into `main.sh` for
  anything touching tmux, so it stays tmux-free like `scripts/`.
  - `transcript.py` — omp and Claude Code JSONL session files → Markdown blocks.
  - `chat.py` — line-API scroll view rendering those blocks with Rich Markdown at the view
    width (wraps prose, code and tables; cached per width) plus a live streaming tail.
  - `agent_socket.py` — NDJSON client for the agent socket (draft get/set, live stream).
- `integrations/` — harness hooks that call `"$TMUX_AGENTS" state|session …`. `omp.ts` also
  serves the agent socket; Claude Code has no equivalent, so it gets no streaming or drafts.

Rule of thumb: `main.sh` = tmux; everything in `scripts/` = tmux-free glue.

## Testing

Use [bats-core](https://github.com/bats-core/bats-core) (`bats`) for tests. See
`tests/test_common.bats` for the pattern: source the script under test standalone in a
`bash -c` snippet (stub `tmux` as a shell function if needed) and assert on `$status`/
`$output` from bats' `run`. Run with `bats tests/`.

The dashboard is tested with pytest + Textual's `run_test` pilot against a fake `main.sh`
(`tests/test_dashboard.py`). Run with `uv run pytest`.
