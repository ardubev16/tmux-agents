# tmux-agents

A tmux plugin to keep track of coding agents (omp, Claude Code, …) running in your tmux panes:

- a status bar counter of agents that are done / working / blocked,
- a notification when an agent finishes or needs you while you are not looking at it,
- a popup dashboard to read every agent's output, jump to it, or send it a quick reply,
- a keybinding to open a git worktree for a branch in a fixed folder, as its own tmux session.

Agents keep running wherever you start them (e.g. a split next to nvim). They report their state
through their own hooks, so nothing has to run through a separate multiplexer.

## Usage

### Agent hooks

Agents call `"$TMUX_AGENTS" state <working|blocked|done|clear>` from inside their pane, and
`"$TMUX_AGENTS" session <session file> [socket]` when a session starts so the dashboard can render its chat. The plugin
exports `TMUX_AGENTS` to tmux's global environment; the bundled hooks read it from there when their pane
predates the plugin, so agents in existing panes work after a restart. Set up the hooks once:

```sh
~/.config/tmux/plugins/tmux-agents/integrations/install.sh install    # or: uninstall
```

- **omp / pi**: symlinks [`integrations/omp.ts`](integrations/omp.ts) into `~/.omp/agent/extensions/`.
- **Claude Code**: merges [`integrations/claude-hooks.json`](integrations/claude-hooks.json) into
  `~/.claude/settings.json`, keeping your own hooks. It skips the file if it is a symlink (e.g. managed by Home Manager).
- **Anything else**: call the command above from whatever hooks the harness provides.

Run `uninstall` before removing the plugin: TPM gives plugins no removal hook.
Re-run `install` after updating the plugin to pick up hook changes, then restart running agents: hooks and
extensions are loaded when an agent starts.

### Status indicator

Add the placeholder `#{tmux_agents_status}` anywhere inside your `status-left` or `status-right`, for example:

```tmux
set -g status-right '#{tmux_agents_status}'
```

It renders three colored counters: **green** done, **yellow** working, **red** blocked.

### Notifications

When an agent becomes done or blocked while its pane is not on screen, the plugin sends a desktop notification
(`notify-send`) named after the agent's session, e.g. "Fix the login bug finished". Without `notify-send`, or
with `@tmux_agents_notify 'tmux'`, it shows the message in the status line of every attached client instead.

omp also sends its own notifications through tmux passthrough, but tmux drops them from panes that are not
visible unless `allow-passthrough` is `all`; the plugin's notification covers that case (and Claude Code).

### Keybindings

| Key          | Action                                                                                                                       |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| `prefix + O` | Agent dashboard ("overview") popup: agents on the left, the highlighted agent's chat on the right, both updating live. `Enter` switch to it, `Ctrl-r` reply, `PgUp`/`PgDn` scroll the chat (scrolling to the top loads older messages), `x` twice close the agent, `q`/`Esc` close |
| `prefix + W` | Pick a branch (fzf), create or reuse its worktree, and switch to a tmux session rooted there (reuses any session already there) |

Agents are listed by the session name their harness gave them. Agents that need input (**needs input**) come
first, then agents that finished while you were not looking at their pane (**finished**, cleared when you visit
the pane), then the rest. Closing an agent (`x`, then `x` again to confirm) stops the harness like a normal exit
(SIGTERM: omp and Claude Code run their shutdown hooks and save the session), then closes its pane.

The chat is rendered from the agent's session file, so it wraps to the popup width (prose, code and tables) and
stays visible while the agent's pane shows something else, e.g. `$EDITOR` on a long prompt. Only agents whose
hooks report a session (the bundled omp and Claude Code integrations) show a chat.

The dashboard uses your terminal's colorscheme. omp agents also stream their in-progress reply, current activity
(thinking, running a tool) and prompts queued while they work (shown as *queued* until delivered) into the
dashboard, and share their prompt draft: `Ctrl-r` opens the half-typed prompt for editing, `Enter` submits it,
`Ctrl-j` adds a newline, and `Esc` puts your edits back into the agent's prompt. For other agents, and for an
agent waiting on a dialog, a reply types your text followed by `Enter` into the pane; an empty reply sends just
`Enter`, which accepts the highlighted option in a permission prompt.

Worktrees go to `<project>/.worktrees/<repo>/<branch>`, where `<project>` is the folder that contains the repo
(e.g. `~/Repos/work/.worktrees/api/feat-x`). If the branch is already checked out somewhere, that checkout is
reused. A branch that doesn't exist yet is created from the current `HEAD`.

## Installation

Dependencies: `git`, `fzf`, `jq`, `notify-send` (optional, desktop notifications), [`uv`](https://docs.astral.sh/uv/) (the dashboard is a Python app; its first
launch sets up the plugin's `.venv`), tmux ≥ 3.2 (popups).

### Tmux Plugin Manager (recommended)

```tmux
set -g @plugin 'ardubev16/tmux-agents'
```

Then press `prefix + I` to install it.

### Manual

```sh
git clone https://github.com/ardubev16/tmux-agents.git ~/.tmux/plugins/tmux-agents
```

```tmux
run-shell ~/.tmux/plugins/tmux-agents/main.tmux
```

> [!IMPORTANT]
> If you want the [status indicator](#status-indicator), make sure `status-left`/`status-right` are set **before** the line that loads this plugin.

## Configuration Options

The following configuration options (with defaults) are available:

```tmux
# Status segment colors
set -g @tmux_agents_status_background 'colour238'
set -g @tmux_agents_status_done_foreground 'green'
set -g @tmux_agents_status_working_foreground 'yellow'
set -g @tmux_agents_status_blocked_foreground 'red'

# Notify when an agent becomes done/blocked off screen: 'system' (desktop, falls back to tmux), 'tmux', 'off'
set -g @tmux_agents_notify 'system'

# Worktree base folder ('' = <project>/.worktrees); worktrees go to <dir>/<repo>/<branch>
set -g @tmux_agents_worktree_dir ''

# Command typed into a newly created worktree session, e.g. 'nvim .'
set -g @tmux_agents_worktree_command ''

# Keys
set -g @tmux_agents_key_dashboard 'O'
set -g @tmux_agents_key_worktree 'W'

# Print verbose error details
set -g @tmux_agents_debug 'off'
```
