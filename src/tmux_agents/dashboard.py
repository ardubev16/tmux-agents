"""Agent dashboard: agents on the left, the highlighted agent's chat on the right.

The chat is rendered from the agent's session file, so it reflows to any width and stays
visible while the agent's pane shows something else (e.g. $EDITOR on a prompt). Agents
without a session file fall back to a snapshot of their pane.
Everything touching tmux goes through main.sh callbacks, so this module stays tmux-free.
"""

import os
import subprocess
from typing import ClassVar

from rich.text import Text
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.widgets import Footer, Input, Markdown, OptionList, Static
from textual.widgets.option_list import Option

from tmux_agents.transcript import to_markdown

# Same glyphs and default colors as the status bar counters; working agents spin instead.
STATE_STYLES = {
    "done": ("○", "green"),
    "working": ("⦿", "yellow"),
    "blocked": ("●", "red"),
}
SPINNER = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"


class Dashboard(App):
    CSS = """
    #agents { width: 35%; height: 1fr; max-height: 100%; border: none; border-right: solid $panel; padding: 0; }
    #right { width: 1fr; }
    #chat { height: 1fr; padding: 0 1; scrollbar-size-vertical: 1; }
    #chat Markdown { margin: 0; padding: 0; }
    #screen { width: auto; }
    #status { height: 1; padding: 0 1; }
    #reply { display: none; }
    """

    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar = [
        # Priority so the footer shows it over OptionList's hidden enter binding.
        Binding("enter", "enter", "switch", priority=True),
        Binding("ctrl+r", "reply", "reply"),
        Binding("ctrl+l", "reload", "refresh"),
        Binding(
            "pageup",
            "scroll_chat(-1)",
            "scroll",
            key_display="pgup/pgdn",
            priority=True,
        ),
        Binding("pagedown", "scroll_chat(1)", show=False, priority=True),
        Binding("escape", "close", "close"),
    ]

    def __init__(self, main_sh: str) -> None:
        super().__init__(ansi_color=True)
        self.main_sh = main_sh
        self.agents: dict[
            str, list[str]
        ] = {}  # pane -> [state, target, command, path, transcript]
        self.frame = 0
        self.shown: tuple | None = (
            None  # what the chat currently shows, to skip identical re-renders
        )

    def compose(self) -> ComposeResult:
        with Horizontal():
            yield OptionList(id="agents")
            with Vertical(id="right"):
                with VerticalScroll(id="chat"):
                    yield Markdown(id="md")
                    yield Static(id="screen")
                yield Static(id="status")
                yield Input(
                    id="reply", placeholder="reply (empty = just Enter, esc = cancel)"
                )
        yield Footer()

    async def on_mount(self) -> None:
        await self.action_reload()
        # Owned by the list so they stop when the widgets are torn down on exit.
        agents = self.query_one(OptionList)
        agents.set_interval(1, self.action_reload)
        agents.set_interval(0.1, self._tick)

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

    def _label(self, pane: str) -> Text:
        state, target, command, path, _ = self.agents[pane]
        icon, color = STATE_STYLES.get(state, (" ", "default"))
        if state == "working":
            icon = SPINNER[self.frame % len(SPINNER)]
        return Text.assemble(
            (icon, color), f" {target} ", (command, "bold"), "\n  ", (path, "dim")
        )

    async def action_reload(self) -> None:
        agents = self.query_one(OptionList)
        previous = self._pane
        self.agents = {}
        for line in self._main("list_agents").splitlines():
            pane, *fields = line.split("\t")
            self.agents[pane] = (fields + [""])[:5]
        agents.clear_options()
        agents.add_options(Option(self._label(pane), id=pane) for pane in self.agents)
        ids = list(self.agents)
        agents.highlighted = (
            ids.index(previous) if previous in ids else (0 if ids else None)
        )
        await self._show_chat()

    async def on_option_list_option_highlighted(self) -> None:
        await self._show_chat()

    async def _show_chat(self) -> None:
        chat, md, screen = (
            self.query_one("#chat", VerticalScroll),
            self.query_one(Markdown),
            self.query_one("#screen", Static),
        )
        pane = self._pane
        transcript = self.agents[pane][4] if pane else ""
        if transcript and os.path.isfile(transcript):
            key = (pane, transcript, os.path.getmtime(transcript))
            content = None
        elif pane:
            content = Text.from_ansi(
                self._main("preview_agent", pane).rstrip("\n"), no_wrap=True
            )
            key = (pane, content.plain)
        else:
            key, content = None, None
        if key == self.shown:
            return

        follow = (
            self.shown is None
            or self.shown[0] != pane
            or chat.scroll_y >= chat.max_scroll_y - 1
        )
        self.shown = key
        md.display, screen.display = (
            content is None and pane is not None,
            content is not None,
        )
        if content is None:
            await md.update(to_markdown(transcript) if transcript else "")
        else:
            screen.update(content)
        if follow:
            self.call_after_refresh(chat.scroll_end, animate=False)

    def _tick(self) -> None:
        """Animates working agents: their list icon and the status line under the chat."""
        self.frame += 1
        agents = self.query_one(OptionList)
        for index, pane in enumerate(self.agents):
            if self.agents[pane][0] == "working":
                agents.replace_option_prompt_at_index(index, self._label(pane))
        state = self.agents[self._pane][0] if self._pane else ""
        status = {
            "working": Text(f"{SPINNER[self.frame % len(SPINNER)]} working…", "yellow"),
            "blocked": Text("● waiting for you", "red"),
        }.get(state, "")
        self.query_one("#status", Static).update(status)

    async def action_enter(self) -> None:
        reply = self.query_one(Input)
        if reply.display:
            self._main("reply_agent", self._pane or "", reply.value)
            self._close_reply()
            await self.action_reload()
        elif self._pane is not None:
            self._main("switch_agent", self._pane)
            self.exit()

    def action_scroll_chat(self, direction: int) -> None:
        chat = self.query_one("#chat", VerticalScroll)
        chat.scroll_relative(y=direction * max(1, chat.size.height // 2), animate=False)

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
