# tmux-agents

A tmux plugin that keeps track of the coding agents (omp, Claude Code, …) running in your panes:

- status bar counters of agents that are done, working and blocked;
- a notification when an agent finishes or needs you while its pane is not on screen;
- a popup dashboard to read every agent's chat, jump to it, reply to it, or open lazygit on its repo;
- a key to open a branch in its own git worktree and tmux session.

Agents keep running wherever you start them (e.g. a split next to nvim) and report their state through their
own hooks, so nothing has to run inside a separate multiplexer.

## Installation

Requirements: tmux ≥ 3.2, `git`, `fzf`, `jq` and [Go](https://go.dev/dl/) (the dashboard is built on first
launch). Optional: `notify-send` for desktop notifications on Linux (macOS uses `osascript`),
[lazygit](https://github.com/jesseduffield/lazygit) for the dashboard's git view.

With [TPM](https://github.com/tmux-plugins/tpm), add the plugin and press `prefix + I`:

```tmux
set -g @plugin 'ardubev16/tmux-agents'
```

Or clone it and load it yourself:

```sh
git clone https://github.com/ardubev16/tmux-agents.git ~/.tmux/plugins/tmux-agents
```

```tmux
run-shell ~/.tmux/plugins/tmux-agents/main.tmux
```

### Agent hooks

Agents report to the plugin from inside their pane: `"$TMUX_AGENTS" state <working|blocked|done|clear>` on
every state change, and `"$TMUX_AGENTS" session <session file> [socket]` when a session starts, so the dashboard
can render its chat. `TMUX_AGENTS` (the path to `main.sh`) comes from tmux's global environment:
`tmux show-environment -g TMUX_AGENTS`. Install the bundled hooks once:

```sh
~/.tmux/plugins/tmux-agents/integrations/install.sh install    # or: uninstall
```

- **omp / pi**: links [`integrations/omp.ts`](integrations/omp.ts) into `~/.omp/agent/extensions/`.
- **Claude Code**: merges [`integrations/claude-hooks.json`](integrations/claude-hooks.json) into
  `~/.claude/settings.json`, keeping your own hooks. A symlinked file (e.g. from Home Manager) is left alone.
  Every hook runs `main.sh claude_hook`, which reads the event from stdin and decides what it means.
- **Anything else**: call the commands above from the hooks your harness provides.

Both integrations follow plugin updates on their own: omp's is a link, and Claude Code's hooks only name the
events. Re-run `install` when `claude-hooks.json` itself changes. Run `uninstall` before removing the plugin:
TPM has no removal hook.

## Usage

### Status bar

Put `#{tmux_agents_status}` anywhere in `status-left` or `status-right`, set **before** the plugin loads:

```tmux
set -g status-right '#{tmux_agents_status}'
```

It shows ○ done (green), ⦿ working (yellow) and ● blocked (red) agents; counters at 0 are hidden.

### Notifications

When an agent finishes or becomes blocked while its pane is not on screen, you get a desktop notification
(`notify-send` on Linux, `osascript` on macOS) named after the agent's session, e.g. "Fix the login bug
finished". With `@tmux_agents_notify 'tmux'`, the message shows in the status line of every attached client
instead. (omp's own notifications only get through tmux from hidden panes with `allow-passthrough all`.)

### Dashboard (`prefix + O`)

Agents are on the left, named after their session, in three sections: **Waiting** for your input, **Idle**, and
**Running** (with how long they have been running). Idle agents that finished while you were away are marked
**finished** until you visit their pane.

On the right is the highlighted agent's chat, rendered from its session file: it wraps to the popup width and
stays readable while the pane shows something else. Questions the agent asks you (omp's `ask`, Claude Code's
`AskUserQuestion`) appear with their choices, without the answers the agent prefilled. omp agents also stream
their reply as it is written, what they are doing, and prompts queued while they work. The header shows the
agent's uncommitted changes to tracked files (`3 files +40 −12`) and unpushed commits (`⇡2`).

| Key | Action |
| --- | --- |
| `↑`/`↓`, click | Highlight an agent |
| `Enter` | Switch to the agent's pane |
| `Ctrl-r` | Reply (see below) |
| `/` | Fuzzy-filter the list by name, pane or command; `Enter` keeps the filter, `Esc` clears it |
| `Tab` | Run lazygit in the agent's folder in place of the chat; keys and mouse go to it until you quit it |
| `PgUp`/`PgDn`, wheel | Scroll the chat; scrolling to the top loads older messages |
| `x`, `x` | Stop the agent like a normal exit (SIGTERM, so it saves its session) and close its pane |
| `q`/`Esc` | Close the dashboard |

**Replying:** for omp agents, `Ctrl-r` opens their half-typed prompt; `Enter` submits it, `Ctrl-j` adds a
newline, `Ctrl`/`Alt` + arrows move by word, and `Esc` puts your edits back into the agent's prompt. If the
prompt cannot be reached, nothing is sent and the reply stays open. For other agents, and for any agent waiting
on a dialog, the reply is typed into the pane followed by `Enter`; an empty reply sends just `Enter`, which
accepts the highlighted option of a permission prompt. Agents without a session name are shown by their pane
(`session:window.pane`), in the dashboard and in notifications alike.

### Worktrees (`prefix + W`)

Pick a branch with fzf (or type a new name) to switch to a tmux session rooted in its worktree, reusing any
session already there. Worktrees go to `<project>/.worktrees/<repo>/<branch>`, where `<project>` is the folder
containing the repo (e.g. `~/Repos/work/.worktrees/api/feat-x`). A branch already checked out somewhere reuses
that checkout; a new branch starts from the current `HEAD`.

`Ctrl-x` on a branch removes its worktree and kills the sessions rooted there, keeping the branch. git refuses
when the worktree has uncommitted changes, and the main checkout is never removed.

## Configuration

Options and their defaults:

```tmux
# Status bar colors
set -g @tmux_agents_status_background 'colour238'
set -g @tmux_agents_status_done_foreground 'green'
set -g @tmux_agents_status_working_foreground 'yellow'
set -g @tmux_agents_status_blocked_foreground 'red'

# Hide counters at 0 ('off' always shows all three)
set -g @tmux_agents_status_hide_zero 'on'

# Notifications: 'system' (desktop: notify-send on Linux, osascript on macOS), 'tmux' (status line) or 'off'
set -g @tmux_agents_notify 'system'

# Worktree base folder ('' = <project>/.worktrees); worktrees go to <dir>/<repo>/<branch>
set -g @tmux_agents_worktree_dir ''

# Command typed into a new worktree session, e.g. 'nvim .'
set -g @tmux_agents_worktree_command ''

# Keys
set -g @tmux_agents_key_dashboard 'O'
set -g @tmux_agents_key_worktree 'W'

# Show error details
set -g @tmux_agents_debug 'off'
```
