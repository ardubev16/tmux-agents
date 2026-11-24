# tmux-agents

A tmux plugin to keep track of coding agents (omp, Claude Code, …) running in your tmux panes:

- a status bar counter of agents that are done / working / blocked,
- a bell on the agent's window when it finishes or needs you,
- a popup dashboard to read every agent's output, jump to it, or send it a quick reply,
- a keybinding to open a git worktree for a branch in a fixed folder, as its own tmux session.

Agents keep running wherever you start them (e.g. a split next to nvim). They report their state
through their own hooks, so nothing has to run through a separate multiplexer.

## Usage

### Agent hooks

Agents call `"$TMUX_AGENTS" state <working|blocked|done|clear>` from inside their pane. The plugin
exports `TMUX_AGENTS` to every pane created after it loads. You only need to set this up once per harness:

- **omp / pi**: `ln -s "$PWD/integrations/omp.ts" ~/.omp/agent/extensions/tmux-agents.ts`
- **Claude Code**: merge [`integrations/claude-hooks.json`](integrations/claude-hooks.json) into `~/.claude/settings.json`.
- **Anything else**: call the command above from whatever hooks the harness provides.

### Status indicator

Add the placeholder `#{tmux_agents_status}` anywhere inside your `status-left` or `status-right`, for example:

```tmux
set -g status-right '#{tmux_agents_status}'
```

It renders three colored counters: **green** done, **yellow** working, **red** blocked.

### Bell

When an agent becomes done or blocked, the plugin rings its pane's bell. tmux then marks the window with `!`
(also in `prefix + s`/`w`) and forwards the bell to your terminal. This needs `monitor-bell on`, which is the
tmux default.

### Keybindings

| Key          | Action                                                                                                                       |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| `prefix + a` | Agent dashboard popup: blocked agents first, live output preview. `Enter` switch to it, `Ctrl-r` reply, `Ctrl-l` refresh     |
| `prefix + W` | Pick a branch (fzf), create or reuse its worktree, and switch to a tmux session rooted there (reuses any session already there) |

A reply in the dashboard sends your text followed by `Enter`. An empty reply sends just `Enter`, which accepts
the highlighted option in a permission prompt. `Ctrl-c` cancels the reply.

Worktrees go to `<project>/.worktrees/<repo>/<branch>`, where `<project>` is the folder that contains the repo
(e.g. `~/Repos/work/.worktrees/api/feat-x`). If the branch is already checked out somewhere, that checkout is
reused. A branch that doesn't exist yet is created from the current `HEAD`.

## Installation

Dependencies: `git`, `fzf`, tmux ≥ 3.2 (popups).

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

# Ring the agent pane's bell when it becomes done/blocked
set -g @tmux_agents_bell 'on'

# Worktree base folder ('' = <project>/.worktrees); worktrees go to <dir>/<repo>/<branch>
set -g @tmux_agents_worktree_dir ''

# Command typed into a newly created worktree session, e.g. 'nvim .'
set -g @tmux_agents_worktree_command ''

# Keys
set -g @tmux_agents_key_dashboard 'a'
set -g @tmux_agents_key_worktree 'W'

# Print verbose error details
set -g @tmux_agents_debug 'off'
```
