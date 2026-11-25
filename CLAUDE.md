# tmux-agents

A tmux plugin that tracks coding agents (omp, Claude Code, …) running in ordinary tmux panes:
status bar counters (done/working/blocked), notifications on state changes, a popup dashboard to
read/switch/reply, and a keybinding to open a branch worktree as its own session.
See README.md for user-facing usage, keybindings, and configuration options.

## Architecture

- `main.tmux` — plugin entrypoint sourced by tmux/TPM. Reads `@tmux_agents_*` options,
  binds keys, exports `TMUX_AGENTS` (path to `main.sh`) for agent hooks, wires up the
  status bar placeholder.
- `main.sh` — the single entry point: every key binding, hook, popup body and dashboard callback
  is a subcommand (`state`, `session`, `claude_hook`, `status_bar`, `agent_dashboard`/`dashboard`,
  `new_worktree`/`pick_worktree`, the dashboard's `list_agents`/`switch_agent`/…). Popups run
  `main.sh <subcommand>` directly, so no state is passed back through temp files or exit codes.
  `dashboard` downloads the binary released for `version` (bumped by release-please) into `bin/`, then
  execs it; with `@tmux_agents_dashboard_build on` it builds `bin/tmux-agents-local` from the checkout
  instead, rebuilding when the Go sources change.
  Per-pane options: `@tmux_agents_state`, plus what `session` reports: the agent's session
  file (`@tmux_agents_transcript`) and, for omp, its socket (`@tmux_agents_socket`).
  `@tmux_agents_unread` marks a pane that finished while not visible; tmux hooks set in
  `main.tmux` clear it (`main.sh seen`) when the pane is shown.
- `scripts/_common.sh` — tmux-free helpers sourced by `main.sh` (error trap/`die`, `repo_root`,
  `ensure_worktree`, the fzf `select_branch`), kept apart so tests can source them without tmux.
- `cmd/tmux-agents/` — the dashboard TUI (Go, Bubble Tea). Calls back into `main.sh` for anything
  touching tmux, so tmux formats and pane options live in one place (`list_agents` also feeds the
  status bar).
  - `internal/transcript` — omp and Claude Code JSONL session files → Markdown blocks, read from
    the end (only the newest part is parsed; polls parse just the appended lines).
  - `internal/chat` — scroll view rendering those blocks with Glamour at the view width
    (hard-wraps what Glamour cannot, so nothing is cut; cached per width) plus a live tail.
  - `internal/agentsocket` — NDJSON client for the agent socket (draft get/set, live stream).
  - `internal/term` — runs a program (lazygit) in a pty in part of the view: a terminal emulator
    (`charmbracelet/x/vt`) keeps its screen, Bubble Tea keys are forwarded to it.
- `integrations/` — harness hooks that call `"$TMUX_AGENTS" state|session …`. `omp.ts` also
  serves the agent socket; Claude Code has no equivalent, so it gets no streaming or drafts.

Rule of thumb: tmux logic goes in `main.sh`; logic worth testing without tmux goes in `_common.sh`.

## Testing

Use [bats-core](https://github.com/bats-core/bats-core) (`bats`) for tests. See
`tests/test_common.bats` for the pattern: source the script under test standalone in a
`bash -c` snippet (stub `tmux` as a shell function if needed) and assert on `$status`/
`$output` from bats' `run`. Run with `bats tests/`.

The dashboard is tested with `go test ./...`: transcript parsing, chat wrapping, and the model
driven key by key against a fake `main.sh` and a fake agent socket (`cmd/tmux-agents/main_test.go`).

## Releases

release-please (`.github/workflows/release.yaml`) keeps a release PR open from the Conventional Commits on
`main`; merging it tags the release, bumps `version` in `main.sh`, and attaches the dashboard built for
linux/darwin × amd64/arm64, which `main.sh dashboard` downloads.

## Commits

Commit on your own whenever a change is complete and its checks pass; no need to ask. Every commit uses
[Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `test:`, `docs:`, `ci:`,
`chore:`, `refactor:`, optional scope, e.g. `feat(dashboard): …`). One logical change per commit.
