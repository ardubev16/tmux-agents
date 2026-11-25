"""Scrollable chat view: Markdown blocks rendered at the widget's width, plus a live tail.

Rich's Markdown wraps prose, code and tables to the given width, so nothing is cut; blocks
are cached per width, so appending a message or streaming the tail only renders what changed.
The owner passes only the newest blocks; scrolling to the top posts `NeedMore` for older ones.
"""

from rich.markdown import Markdown
from textual.geometry import Size
from textual.message import Message
from textual.scroll_view import ScrollView
from textual.strip import Strip


class ChatLog(ScrollView, can_focus=False):
    class NeedMore(Message):
        pass

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self.blocks: list[str] = []
        self.live = ""
        self.more = False  # older blocks exist that the owner has not passed yet
        self._cache: dict[str, list[Strip]] = {}
        self._cache_width = 0
        self._lines: list[Strip] = []

    def update(self, blocks: list[str] | None = None, live: str | None = None, more: bool | None = None) -> None:
        prepended = 0
        if blocks is not None:
            old = self.blocks
            # Older blocks loaded in front: keep the view on the same text instead of jumping.
            if old and len(blocks) > len(old) and blocks[-len(old) :] == old:
                prepended = len(blocks) - len(old)
            self.blocks = blocks
        if live is not None:
            self.live = live
        if more is not None:
            self.more = more
        self._relayout(prepended)

    def _render_block(self, markdown: str, width: int) -> list[Strip]:
        if markdown not in self._cache:
            console = self.app.console
            # ansi_dark highlights code with the terminal's palette instead of fixed RGB (monokai).
            md = Markdown(markdown, code_theme="ansi_dark")
            lines = console.render_lines(md, console.options.update_width(width), pad=False)
            self._cache[markdown] = [Strip(line) for line in lines]
        return self._cache[markdown]

    def _relayout(self, prepended: int = 0) -> None:
        width = self.scrollable_content_region.width
        if width <= 0:
            return  # not laid out yet; on_resize renders
        if width != self._cache_width:
            self._cache, self._cache_width = {}, width
        follow = self.scroll_y >= self.max_scroll_y - 1
        lines: list[Strip] = []
        shift = 0
        for index, block in enumerate([*self.blocks, self.live] if self.live else self.blocks):
            if lines:
                lines.append(Strip.blank(width))
            if index == prepended:
                shift = len(lines)
            lines.extend(self._render_block(block, width))
        self._lines = lines
        self.virtual_size = Size(width, len(lines))
        self.refresh()
        if follow:
            # After the refresh, so max_scroll_y already accounts for the new lines.
            self.call_after_refresh(self.scroll_end, animate=False)
        elif prepended:
            self.scroll_to(y=self.scroll_y + shift, animate=False, immediate=True)

    def watch_scroll_y(self, old_value: float, new_value: float) -> None:
        super().watch_scroll_y(old_value, new_value)
        if new_value == 0 and self.more and self.max_scroll_y > 0:
            self.more = False  # one request until the owner answers with update(more=...)
            self.post_message(self.NeedMore())

    def on_resize(self) -> None:
        self._relayout()

    def render_line(self, y: int) -> Strip:
        scroll_x, scroll_y = self.scroll_offset
        width = self.scrollable_content_region.width
        index = scroll_y + y
        if index >= len(self._lines):
            return Strip.blank(width, self.rich_style)
        return self._lines[index].crop_extend(scroll_x, scroll_x + width, self.rich_style)
