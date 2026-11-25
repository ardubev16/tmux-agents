"""Agent dashboard: agents on the left, the highlighted agent's chat on the right.

The chat is rendered from the agent's session file, so it reflows to any width and stays
visible while the agent's pane shows something else (e.g. $EDITOR on a prompt). Agents that
serve a socket (omp) also stream their in-progress reply and share their prompt draft.
Agents without a session file fall back to a snapshot of their pane.
Everything touching tmux goes through main.sh callbacks, so this module stays tmux-free.
"""

import os
import subprocess
from typing import ClassVar, NamedTuple

from rich.text import Text
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.widgets import Footer, OptionList, Static, TextArea
from textual.widgets.option_list import Option

from tmux_agents import agent_socket
from tmux_agents.chat import ChatLog
from tmux_agents.transcript import to_blocks

# Same glyphs and default colors as the status bar counters; working agents spin instead.
STATE_STYLES = {"done": ("○", "green"), "working": ("⦿", "yellow"), "blocked": ("●", "red")}
SPINNER = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
SOCKET_ERRORS = (OSError, TimeoutError, ValueError)


class Agent(NamedTuple):
    pane: str
    state: str
    target: str
    command: str
    path: str
    transcript: str
    socket: str

    @property
    def shares_draft(self) -> bool:
        # A blocked agent shows a dialog, so a reply answers it instead of touching the draft.
        return bool(self.socket) and self.state != "blocked"


class Dashboard(App):
    CSS = """
    #agents { width: 35%; height: 1fr; border: none; border-right: solid $panel; padding: 0; }
    #right { width: 1fr; }
    #chat, #snapshot { height: 1fr; padding: 0 1; scrollbar-size-vertical: 1; }
    #screen { width: auto; }
    #status { height: 1; padding: 0 1; }
    #reply { display: none; height: auto; max-height: 12; border: tall $accent; }
    """

    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar = [
        # Priority so the footer shows it over OptionList's hidden enter binding, and Enter sends replies.
        Binding("enter", "enter", "switch / send", priority=True),
        Binding("ctrl+j,shift+enter", "newline", "newline", show=False, priority=True),
        Binding("ctrl+r", "reply", "reply"),
        Binding("ctrl+l", "reload", "refresh"),
        Binding("pageup", "scroll_chat(-1)", "scroll", key_display="pgup/pgdn", priority=True),
        Binding("pagedown", "scroll_chat(1)", show=False, priority=True),
        Binding("escape", "close", "close"),
    ]

    def __init__(self, main_sh: str) -> None:
        super().__init__(ansi_color=True)
        self.main_sh = main_sh
        self.agents: dict[str, Agent] = {}
        self.frame = 0
        self.shown: tuple | None = None  # what the chat currently shows, to skip identical re-renders
        self.live: dict = {}  # latest live-state snapshot from the highlighted agent's socket

    def compose(self) -> ComposeResult:
        with Horizontal():
            yield OptionList(id="agents")
            with Vertical(id="right"):
                yield ChatLog(id="chat")
                with VerticalScroll(id="snapshot"):
                    yield Static(id="screen")
                yield Static(id="status")
                yield TextArea(id="reply", soft_wrap=True, show_line_numbers=False)
        yield Footer()

    async def on_mount(self) -> None:
        await self.action_reload()
        # Owned by the list so they stop when the widgets are torn down on exit.
        agents = self.query_one(OptionList)
        agents.set_interval(1, self.action_reload)
        agents.set_interval(0.25, self._poll_chat)
        agents.set_interval(1 / 12, self._tick)

    def _main(self, *args: str) -> str:
        return subprocess.run([self.main_sh, *args], check=True, capture_output=True, text=True).stdout

    @property
    def _agent(self) -> Agent | None:
        agents = self.query_one(OptionList)
        if agents.highlighted is None:
            return None
        return self.agents.get(agents.get_option_at_index(agents.highlighted).id or "")

    def _label(self, agent: Agent) -> Text:
        icon, color = STATE_STYLES.get(agent.state, (" ", "default"))
        if agent.state == "working":
            icon = SPINNER[self.frame % len(SPINNER)]
        return Text.assemble((icon, color), f" {agent.target} ", (agent.command, "bold"), "\n  ", (agent.path, "dim"))

    async def action_reload(self) -> None:
        agents: dict[str, Agent] = {}
        for line in self._main("list_agents").splitlines():
            fields = (line.split("\t") + [""] * 7)[:7]
            agents[fields[0]] = Agent(*fields)
        if agents == self.agents:
            return
        listing = self.query_one(OptionList)
        previous = self._agent
        self.agents = agents
        listing.clear_options()
        listing.add_options(Option(self._label(agent), id=pane) for pane, agent in agents.items())
        ids = list(agents)
        listing.highlighted = ids.index(previous.pane) if previous and previous.pane in ids else (0 if ids else None)
        self._on_agent_changed(previous)

    def on_option_list_option_highlighted(self) -> None:
        self._on_agent_changed(None)

    def _on_agent_changed(self, previous: Agent | None) -> None:
        agent = self._agent
        if previous and agent and (previous.pane, previous.socket) == (agent.pane, agent.socket):
            return
        self.workers.cancel_group(self, "live")
        self.live = {}
        if agent and agent.socket:
            self._follow(agent.socket)
        self._poll_chat(force=True)

    @work(exclusive=True, group="live")
    async def _follow(self, socket: str) -> None:
        try:
            async for live in agent_socket.subscribe(socket):
                finished = self.live.get("streaming") and not live.get("streaming")
                self.live = live
                if finished:
                    self._poll_chat(force=True)  # the finished reply is in the session file now
                else:
                    self.query_one(ChatLog).update(live=live.get("text", ""))
        except SOCKET_ERRORS:
            pass  # agent gone or socket stale: the session file still shows the chat
        self.live = {}

    def _poll_chat(self, force: bool = False) -> None:
        agent = self._agent
        chat, snapshot = self.query_one(ChatLog), self.query_one("#snapshot")
        # omp creates the session file with the first message; until then the chat is just the live reply.
        native = bool(agent and agent.transcript)
        screen = ""
        if agent and native:
            stat = os.stat(agent.transcript) if os.path.isfile(agent.transcript) else None
            key: tuple | None = (agent.pane, stat and (stat.st_mtime_ns, stat.st_size))
        elif agent:
            screen = self._main("preview_agent", agent.pane).rstrip("\n")
            key = (agent.pane, screen)
        else:
            key = None
        if key == self.shown and not force:
            return
        pane_changed = not (self.shown and key and self.shown[0] == key[0])
        self.shown = key
        chat.display, snapshot.display = native or agent is None, not native and agent is not None
        if agent and native:
            live = self.live.get("text", "") if self.live.get("streaming") else ""
            chat.update(blocks=to_blocks(agent.transcript), live=live)
        elif agent:
            self.query_one("#screen", Static).update(Text.from_ansi(screen, no_wrap=True))
        else:
            chat.update(blocks=[], live="")
        if pane_changed:
            chat.scroll_end(animate=False)
            snapshot.scroll_end(animate=False)

    def _tick(self) -> None:
        """Animates working agents: their list icon and the status line under the chat."""
        self.frame += 1
        listing = self.query_one(OptionList)
        for index, agent in enumerate(self.agents.values()):
            if agent.state == "working":
                listing.replace_option_prompt_at_index(index, self._label(agent))
        self.query_one("#status", Static).update(self._status())

    def _status(self) -> Text | str:
        agent = self._agent
        if agent is None:
            return ""
        if not self.query_one(ChatLog).display:
            return Text("no session file reported: showing the pane", "dim")
        if agent.state == "blocked":
            return Text("● waiting for you", "red")
        if agent.state != "working":
            return ""
        spinner = SPINNER[self.frame % len(SPINNER)]
        if self.live.get("tool"):
            doing = f"running {self.live['tool']}…"
        elif self.live.get("thinking"):
            doing = "thinking…"
        elif self.live.get("streaming"):
            doing = "writing…"
        else:
            doing = "working…"
        return Text(f"{spinner} {doing}", "yellow")

    async def _set_draft(self, agent: Agent, text: str) -> bool:
        try:
            return bool((await agent_socket.request(agent.socket, op="set_draft", text=text)).get("ok"))
        except SOCKET_ERRORS:
            return False

    async def action_reply(self) -> None:
        agent = self._agent
        if agent is None:
            return
        draft = ""
        if agent.shares_draft:
            try:
                draft = (await agent_socket.request(agent.socket, op="get_draft")).get("draft", "")
            except SOCKET_ERRORS:
                pass
        reply = self.query_one(TextArea)
        reply.text = draft
        reply.display = True
        reply.focus()
        reply.move_cursor(reply.document.end)

    def action_newline(self) -> None:
        reply = self.query_one(TextArea)
        if reply.display:
            reply.insert("\n")

    async def action_enter(self) -> None:
        reply, agent = self.query_one(TextArea), self._agent
        if reply.display:
            if agent and agent.shares_draft and await self._set_draft(agent, reply.text):
                self._main("reply_agent", agent.pane, "")  # Enter submits the draft we just wrote
            elif agent:
                self._main("reply_agent", agent.pane, reply.text)
            self._close_reply()
            await self.action_reload()
        elif agent is not None:
            self._main("switch_agent", agent.pane)
            self.exit()

    def action_scroll_chat(self, direction: int) -> None:
        view = self.query_one(ChatLog) if self.query_one(ChatLog).display else self.query_one("#snapshot")
        view.scroll_relative(y=direction * max(1, view.size.height // 2), animate=False)

    def _close_reply(self) -> None:
        reply = self.query_one(TextArea)
        reply.text = ""
        reply.display = False
        self.query_one(OptionList).focus()

    async def action_close(self) -> None:
        reply, agent = self.query_one(TextArea), self._agent
        if reply.display:
            if agent and agent.shares_draft:
                await self._set_draft(agent, reply.text)  # keep the edits as the agent's draft
            self._close_reply()
        else:
            self.exit()
