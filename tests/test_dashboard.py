import asyncio
import json
import tempfile
from pathlib import Path

from textual.containers import VerticalScroll
from textual.widgets import Static, TextArea

from tmux_agents.chat import ChatLog
from tmux_agents.dashboard import Dashboard
from tmux_agents.transcript import to_blocks


def write_jsonl(path: Path, entries: list[dict]) -> str:
    path.write_text("\n".join(json.dumps(e) for e in entries) + "\n")
    return str(path)


def omp_message(id_: str, parent: str | None, role: str, *content: dict) -> dict:
    return {"type": "message", "id": id_, "parentId": parent, "message": {"role": role, "content": list(content)}}


def text(t: str) -> dict:
    return {"type": "text", "text": t}


def test_omp_transcript_shows_only_the_active_branch(tmp_path):
    path = write_jsonl(
        tmp_path / "s.jsonl",
        [
            {"type": "session", "id": "s"},
            omp_message("1", None, "user", text("fix it")),
            omp_message("2", "1", "assistant", text("abandoned answer")),
            omp_message("3", "1", "assistant", {"type": "thinking", "thinking": "hmm"}, text("rewound answer")),
            omp_message(
                "4", "3", "assistant", {"type": "toolCall", "name": "bash", "arguments": {"command": "ls\n-la"}}
            ),
            omp_message("5", "4", "toolResult", text("file list")),
            omp_message("6", "5", "assistant", {"type": "toolCall", "name": "read", "arguments": {"i": "Reading `x`"}}),
        ],
    )
    assert to_blocks(path) == ["> fix it", "rewound answer", "- `bash` ls -la\n- `read` Reading 'x'"]


def test_claude_transcript(tmp_path):
    path = write_jsonl(
        tmp_path / "c.jsonl",
        [
            {"type": "user", "uuid": "1", "parentUuid": None, "message": {"role": "user", "content": "hi\nthere"}},
            {
                "type": "user",
                "uuid": "2",
                "parentUuid": "1",
                "isMeta": True,
                "message": {"role": "user", "content": "x"},
            },
            {
                "type": "assistant",
                "uuid": "3",
                "parentUuid": "2",
                "message": {
                    "role": "assistant",
                    "content": [text("**hello**"), {"type": "tool_use", "name": "Read", "input": {"file_path": "/a"}}],
                },
            },
            {
                "type": "user",
                "uuid": "4",
                "parentUuid": "3",
                "message": {"role": "user", "content": [{"type": "tool_result", "content": "x"}]},
            },
            {
                "type": "user",
                "uuid": "5",
                "parentUuid": "4",
                "message": {"role": "user", "content": "<command-name>/clear</command-name>"},
            },
        ],
    )
    assert to_blocks(path) == ["> hi\n> there", "**hello**", "- `Read` /a"]


class FakeAgent:
    """The omp integration's socket: serves a draft and records what the dashboard sends."""

    def __init__(self, draft: str) -> None:
        self.draft = draft
        self.requests: list[dict] = []
        self.subscribers: list[asyncio.StreamWriter] = []

    async def handle(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        request = json.loads(await reader.readline())
        self.requests.append(request)
        if request["op"] == "get_draft":
            writer.write(json.dumps({"draft": self.draft}).encode() + b"\n")
        elif request["op"] == "set_draft":
            self.draft = request["text"]
            writer.write(b'{"ok": true}\n')
        elif request["op"] == "subscribe":
            self.subscribers.append(writer)
            return
        await writer.drain()
        writer.close()

    async def push(self, live: dict) -> None:
        for writer in self.subscribers:
            writer.write(json.dumps(live).encode() + b"\n")
            await writer.drain()


def fake_main(tmp_path: Path, socket: str = "") -> tuple[str, Path]:
    """A main.sh stand-in: a blocked agent with only a pane, a working one with a long transcript."""
    entries = [omp_message(str(i), str(i - 1) if i else None, "assistant", text(f"answer {i}")) for i in range(100)]
    entries.append(omp_message("100", "99", "assistant", text("```\n" + "x" * 300 + "\n```")))
    transcript = write_jsonl(tmp_path / "t.jsonl", entries)
    log = tmp_path / "calls"
    script = tmp_path / "main.sh"
    script.write_text(f"""#!/usr/bin/env bash
printf '%s\\n' "$*" >>'{log}'
case "$1" in
list_agents) printf '%%1\\tblocked\\ts:1.0\\tclaude\\t~/a\\t\\t\\n%%2\\tworking\\ts:2.0\\tomp\\t~/b\\t{transcript}\\t{socket}\\n' ;;
preview_agent) echo "screen of $2" ;;
esac
""")
    script.chmod(0o755)
    return str(script), log


def chat_text(app: Dashboard) -> list[str]:
    chat = app.query_one(ChatLog)
    return [strip.text.rstrip() for strip in chat._lines]


async def test_chat_reflows_to_the_view_and_falls_back_to_the_pane(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test(size=(100, 30)) as pilot:
        snapshot = app.query_one("#snapshot", VerticalScroll)
        assert snapshot.display and "screen of %1" in str(app.query_one("#screen", Static).render())
        await pilot.press("down")
        await pilot.pause()
        chat = app.query_one(ChatLog)
        assert chat.display and not snapshot.display
        lines = chat_text(app)
        assert "answer 0" in lines[0] and any("answer 99" in line for line in lines)
        # The 300-column code line wraps instead of being cut.
        assert sum(line.count("x") for line in lines) == 300
        assert all(len(line) <= chat.scrollable_content_region.width for line in lines)


async def test_pageup_scrolls_chat_half_a_page(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test(size=(100, 30)) as pilot:
        await pilot.press("down")
        await pilot.pause()
        chat = app.query_one(ChatLog)
        bottom = chat.scroll_y
        assert bottom > 0 and bottom == chat.max_scroll_y
        await pilot.press("pageup")
        assert chat.scroll_y == bottom - chat.size.height // 2


async def test_reply_continues_the_agents_draft(tmp_path):
    with tempfile.TemporaryDirectory(dir="/tmp") as short:  # unix socket paths are capped at ~100 bytes
        socket, agent = f"{short}/a.sock", FakeAgent("half typed")
        server = await asyncio.start_unix_server(agent.handle, socket)
        main_sh, log = fake_main(tmp_path, socket)
        app = Dashboard(main_sh)
        async with app.run_test() as pilot:
            await pilot.press("down", "ctrl+r")
            await pilot.pause()
            reply = app.query_one(TextArea)
            assert reply.text == "half typed"
            await pilot.press("space", "o", "k", "ctrl+j", "x", "escape")
            assert agent.draft == "half typed ok\nx"  # cancelling keeps the edits in the agent
            await pilot.press("ctrl+r", "!", "enter")
            assert agent.draft == "half typed ok\nx!"
        server.close()
    calls = log.read_text().splitlines()
    assert calls.count("reply_agent %2 ") == 1  # the draft is submitted with a bare Enter


async def test_live_reply_streams_into_the_chat(tmp_path):
    with tempfile.TemporaryDirectory(dir="/tmp") as short:
        socket, agent = f"{short}/a.sock", FakeAgent("")
        server = await asyncio.start_unix_server(agent.handle, socket)
        main_sh, _ = fake_main(tmp_path, socket)
        app = Dashboard(main_sh)
        async with app.run_test(size=(100, 30)) as pilot:
            await pilot.press("down")
            await pilot.pause(0.2)
            await agent.push({"text": "streaming **now**", "thinking": False, "tool": "", "streaming": True})
            await pilot.pause(0.2)
            assert "streaming now" in chat_text(app)[-1]
            assert "writing" in str(app.query_one("#status", Static).render())
        server.close()


async def test_blocked_agent_reply_types_into_the_pane(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("ctrl+r", "y", "e", "s", "enter")
    assert "reply_agent %1 yes" in log.read_text().splitlines()


async def test_enter_switches_and_closes(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("enter")
    assert "switch_agent %1" in log.read_text().splitlines()
    assert app.return_code == 0


async def test_snapshot_view_scrolls_too(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("pageup")
        assert app.query_one("#snapshot", VerticalScroll).display
