import json
from pathlib import Path

from textual.containers import VerticalScroll
from textual.widgets import Markdown, Static

from tmux_agents.dashboard import Dashboard
from tmux_agents.transcript import to_markdown


def write_jsonl(path: Path, entries: list[dict]) -> str:
    path.write_text("\n".join(json.dumps(e) for e in entries) + "\n")
    return str(path)


def omp_message(id_: str, parent: str | None, role: str, *content: dict) -> dict:
    return {
        "type": "message",
        "id": id_,
        "parentId": parent,
        "message": {"role": role, "content": list(content)},
    }


def text(t: str) -> dict:
    return {"type": "text", "text": t}


def test_omp_transcript_shows_only_the_active_branch(tmp_path):
    path = write_jsonl(
        tmp_path / "s.jsonl",
        [
            {"type": "session", "id": "s"},
            omp_message("1", None, "user", text("fix it")),
            omp_message("2", "1", "assistant", text("abandoned answer")),
            omp_message(
                "3",
                "1",
                "assistant",
                {"type": "thinking", "thinking": "hmm"},
                text("rewound answer"),
            ),
            omp_message(
                "4",
                "3",
                "assistant",
                {
                    "type": "toolCall",
                    "name": "bash",
                    "arguments": {"command": "ls\n-la"},
                },
            ),
            omp_message("5", "4", "toolResult", text("file list")),
            omp_message(
                "6",
                "5",
                "assistant",
                {"type": "toolCall", "name": "read", "arguments": {"i": "Reading `x`"}},
            ),
        ],
    )
    assert (
        to_markdown(path)
        == "> fix it\n\nrewound answer\n\n- `bash` ls -la\n- `read` Reading 'x'"
    )


def test_claude_transcript(tmp_path):
    path = write_jsonl(
        tmp_path / "c.jsonl",
        [
            {
                "type": "user",
                "uuid": "1",
                "parentUuid": None,
                "message": {"role": "user", "content": "hi\nthere"},
            },
            {
                "type": "user",
                "uuid": "2",
                "parentUuid": "1",
                "isMeta": True,
                "message": {"role": "user", "content": "meta"},
            },
            {
                "type": "assistant",
                "uuid": "3",
                "parentUuid": "2",
                "message": {
                    "role": "assistant",
                    "content": [
                        text("**hello**"),
                        {
                            "type": "tool_use",
                            "name": "Read",
                            "input": {"file_path": "/a"},
                        },
                    ],
                },
            },
            {
                "type": "user",
                "uuid": "4",
                "parentUuid": "3",
                "message": {
                    "role": "user",
                    "content": [{"type": "tool_result", "content": "x"}],
                },
            },
            {
                "type": "user",
                "uuid": "5",
                "parentUuid": "4",
                "message": {
                    "role": "user",
                    "content": "<command-name>/clear</command-name>",
                },
            },
        ],
    )
    assert to_markdown(path) == "> hi\n> there\n\n**hello**\n\n- `Read` /a"


def fake_main(tmp_path: Path) -> tuple[str, Path]:
    """A main.sh stand-in: a blocked agent with only a pane, a working one with a long transcript."""
    transcript = write_jsonl(
        tmp_path / "t.jsonl",
        [
            omp_message(
                str(i), str(i - 1) if i else None, "assistant", text(f"answer {i}")
            )
            for i in range(100)
        ],
    )
    log = tmp_path / "calls"
    script = tmp_path / "main.sh"
    script.write_text(f"""#!/usr/bin/env bash
echo "$*" >>'{log}'
case "$1" in
list_agents) printf '%%1\\tblocked\\ts:1.0\\tclaude\\t~/a\\t\\n%%2\\tworking\\ts:2.0\\tomp\\t~/b\\t{transcript}\\n' ;;
preview_agent) echo "screen of $2" ;;
esac
""")
    script.chmod(0o755)
    return str(script), log


async def test_chat_comes_from_transcript_or_falls_back_to_the_pane(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        screen = app.query_one("#screen", Static)
        assert screen.display and "screen of %1" in str(screen.render())
        await pilot.press("down")
        await pilot.pause()
        md = app.query_one(Markdown)
        assert md.display and not screen.display
        assert md.source.endswith("answer 99")


async def test_reply_goes_to_highlighted_agent(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("down", "ctrl+r", "y", "e", "s", "enter")
        await pilot.press("ctrl+r", "enter")
        calls = log.read_text().splitlines()
        assert "reply_agent %2 yes" in calls
        assert calls.count("reply_agent %2 ") == 1


async def test_pageup_scrolls_chat_half_a_page(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test(size=(100, 30)) as pilot:
        await pilot.press("down")
        await pilot.pause(0.3)
        chat = app.query_one("#chat", VerticalScroll)
        bottom = chat.scroll_y
        assert bottom > 0
        await pilot.press("pageup")
        assert chat.scroll_y == bottom - chat.size.height // 2


async def test_enter_switches_and_closes(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("enter")
    assert "switch_agent %1" in log.read_text().splitlines()
    assert app.return_code == 0
