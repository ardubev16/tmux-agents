"""Agent dashboard: agent list on top, full-width preview of the highlighted pane below.

Everything touching tmux goes through main.sh callbacks, so this module stays tmux-free.
"""

import subprocess
from typing import ClassVar

from rich.text import Text
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.widgets import Footer, Input, OptionList, RichLog
from textual.widgets.option_list import Option

# Same glyphs as the status bar counters.
STATE_ICONS = {"done": "○", "working": "⦿", "blocked": "●"}


class Dashboard(App):
    CSS = """
    OptionList { height: auto; max-height: 8; border: none; padding: 0; }
    /* Panes are at most as wide as the client: no padding/scrollbar, so a full-width pane fits exactly. */
    RichLog { height: 1fr; padding: 0; scrollbar-size: 0 0; }
    #reply { dock: bottom; display: none; }
    """

    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar = [
        # Priority so the footer shows it over OptionList's hidden enter binding.
        Binding("enter", "enter", "switch", priority=True),
        Binding("ctrl+r", "reply", "reply"),
        Binding("ctrl+l", "reload", "refresh"),
        Binding(
            "pageup",
            "scroll_preview(-1)",
            "scroll",
            key_display="pgup/pgdn",
            priority=True,
        ),
        Binding("pagedown", "scroll_preview(1)", show=False, priority=True),
        Binding("escape", "close", "close"),
    ]

    def __init__(self, main_sh: str) -> None:
        super().__init__(ansi_color=True)
        self.main_sh = main_sh

    def compose(self) -> ComposeResult:
        yield OptionList(id="agents")
        yield RichLog(id="preview", wrap=False, highlight=False, markup=False)
        yield Input(id="reply", placeholder="reply (empty = just Enter, esc = cancel)")
        yield Footer()

    def on_mount(self) -> None:
        self.action_reload()

    def _main(self, *args: str) -> str:
        return subprocess.run(
            [self.main_sh, *args], check=True, capture_output=True, text=True
        ).stdout

    @property
    def _pane(self) -> str | None:
        agents = self.query_one(OptionList)
        if agents.highlighted is None:
            return None
        return agents.get_option_at_index(agents.highlighted).id

    def action_enter(self) -> None:
        reply = self.query_one(Input)
        if reply.display:
            self._main("reply_agent", self._pane or "", reply.value)
            self._close_reply()
            self.action_reload()
        elif self._pane is not None:
            self._main("switch_agent", self._pane)
            self.exit()

    def action_reload(self) -> None:
        agents = self.query_one(OptionList)
        previous = self._pane
        agents.clear_options()
        # list_agents: pane_id \t state \t target \t command \t path
        for line in self._main("list_agents").splitlines():
            pane, state, target, command, path = line.split("\t")
            label = f"{STATE_ICONS.get(state, ' ')} {state:<8} {target:<16} {command:<12} {path}"
            agents.add_option(Option(label, id=pane))
        ids = [agents.get_option_at_index(i).id for i in range(agents.option_count)]
        agents.highlighted = (
            ids.index(previous) if previous in ids else (0 if ids else None)
        )
        self._show_preview()

    def _show_preview(self) -> None:
        preview = self.query_one(RichLog)
        preview.clear()
        if self._pane is not None:
            # shrink=False: lines wider than the popup scroll horizontally instead of being cut.
            preview.write(
                Text.from_ansi(self._main("preview_agent", self._pane).rstrip("\n")),
                shrink=False,
            )
            preview.scroll_end(animate=False)

    def on_option_list_option_highlighted(self) -> None:
        self._show_preview()

    def action_scroll_preview(self, direction: int) -> None:
        preview = self.query_one(RichLog)
        preview.scroll_relative(
            y=direction * max(1, preview.size.height // 2), animate=False
        )

    def action_reply(self) -> None:
        if self._pane is None:
            return
        reply = self.query_one(Input)
        reply.display = True
        reply.focus()

    def _close_reply(self) -> None:
        reply = self.query_one(Input)
        reply.value = ""
        reply.display = False
        self.query_one(OptionList).focus()

    def action_close(self) -> None:
        if self.query_one(Input).display:
            self._close_reply()
        else:
            self.exit()
