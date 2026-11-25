from pathlib import Path

from textual.widgets import RichLog

from tmux_agents.dashboard import Dashboard

WIDE = 300


def fake_main(tmp_path: Path) -> tuple[str, Path]:
    """A main.sh stand-in: two agents, a 300-column pane, and a log of every call."""
    log = tmp_path / "calls"
    script = tmp_path / "main.sh"
    script.write_text(f"""#!/usr/bin/env bash
echo "$*" >>'{log}'
case "$1" in
list_agents) printf '%%1\\tblocked\\ts:1.0\\tclaude\\t~/a\\n%%2\\tdone\\ts:2.0\\tomp\\t~/b\\n' ;;
preview_agent) for i in $(seq 50); do printf "$1-%0{WIDE}d\\n" "$i"; done ;;
esac
""")
    script.chmod(0o755)
    return str(script), log


async def test_wide_pane_is_neither_wrapped_nor_cut(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test(size=(80, 24)):
        preview = app.query_one(RichLog)
        assert len(preview.lines) == 50
        assert preview.virtual_size.width >= WIDE


async def test_reply_goes_to_highlighted_agent(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("down", "ctrl+r", "y", "e", "s", "enter")
        await pilot.press("ctrl+r", "enter")
        calls = log.read_text().splitlines()
        assert "reply_agent %2 yes" in calls
        assert calls.count("reply_agent %2 ") == 1


async def test_pageup_scrolls_preview_half_a_page(tmp_path):
    main_sh, _ = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test(size=(80, 24)) as pilot:
        preview = app.query_one(RichLog)
        await pilot.pause()
        bottom = preview.scroll_y
        await pilot.press("pageup")
        assert preview.scroll_y == bottom - preview.size.height // 2


async def test_enter_switches_and_closes(tmp_path):
    main_sh, log = fake_main(tmp_path)
    app = Dashboard(main_sh)
    async with app.run_test() as pilot:
        await pilot.press("enter")
    assert "switch_agent %1" in log.read_text().splitlines()
    assert app.return_code == 0
