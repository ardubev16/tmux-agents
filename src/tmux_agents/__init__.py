import os
import sys


def main() -> None:
    # A lone Esc is told apart from escape sequences by waiting (Textual default 100 ms); tmux already
    # waits escape-time and hands the popup whole sequences, so a short wait is enough.
    os.environ.setdefault("ESCDELAY", "10")
    from tmux_agents.dashboard import Dashboard

    if len(sys.argv) != 2:
        sys.exit("Usage: tmux-agents <path to main.sh>")
    Dashboard(sys.argv[1]).run()
