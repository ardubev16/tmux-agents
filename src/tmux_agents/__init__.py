import sys


def main() -> None:
    from tmux_agents.dashboard import Dashboard

    if len(sys.argv) != 2:
        sys.exit("Usage: tmux-agents <path to main.sh>")
    Dashboard(sys.argv[1]).run()
