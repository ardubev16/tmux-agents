"""Turn an agent's JSONL session file (omp/pi or Claude Code) into Markdown blocks for the dashboard.

Session files reach tens of MB, so `Transcript` reads from the end: only the newest part of the active
branch is parsed, older parts on demand, and each poll parses just the appended lines.
"""

import functools
import json
import os

CHUNK = 1 << 18  # bytes read per step when walking back through the file

# Tool-call argument shown next to the tool name, first match wins (omp's "i" is its intent line).
SUMMARY_KEYS = ("i", "description", "command", "file_path", "path", "pattern", "query", "url")
TOOL_PREFIX = "- `"


def _tool_line(name: str, args: object) -> str:
    summary = ""
    if isinstance(args, dict):
        summary = next((str(args[k]) for k in SUMMARY_KEYS if isinstance(args.get(k), str)), "")
    summary = " ".join(summary.split())[:100].replace("`", "'")
    return f"{TOOL_PREFIX}{name}` {summary}".rstrip()


def _blocks(entry: dict) -> list[str]:
    if entry.get("isMeta") or entry.get("isSidechain") or entry.get("type") not in ("message", "user", "assistant"):
        return []
    message = entry.get("message") or {}
    role, content = message.get("role"), message.get("content")
    if isinstance(content, str):
        content = [{"type": "text", "text": content}]
    if role not in ("user", "assistant") or not isinstance(content, list):
        return []

    blocks = []
    for part in content:
        kind = part.get("type")
        if kind == "text" and part.get("text", "").strip():
            text = part["text"].strip()
            if role == "user":
                # Claude Code logs slash commands and their output as user messages.
                if text.startswith(("<command-", "<local-command")):
                    continue
                text = "\n".join(f"> {line}" for line in text.splitlines())
            blocks.append(text)
        elif kind in ("toolCall", "tool_use"):
            blocks.append(_tool_line(part.get("name", "?"), part.get("arguments", part.get("input"))))
    return blocks


def _parse(data: bytes) -> list[tuple[str, dict]]:
    """(id, entry) per line; omp uses `id`, Claude Code `uuid`. Lines without one are not part of the tree."""
    entries = []
    for line in data.split(b"\n"):
        try:
            entry = json.loads(line)
        except ValueError:
            continue  # blank
        key = entry.get("id") or entry.get("uuid") if isinstance(entry, dict) else None
        if key:
            entries.append((key, entry))
    return entries


class Transcript:
    def __init__(self, path: str) -> None:
        self.path = path
        self.entries: dict[str, dict] = {}
        self.leaf: str | None = None  # last entry in the file: the tip of the active branch
        self.start = self.end = -1  # parsed byte range; both on line boundaries, -1 = nothing read yet

    def _older(self, file) -> bool:
        """Parses the lines before self.start; False when the whole file is already parsed."""
        if self.start <= 0:
            return False
        low = self.start
        while True:
            low = max(0, low - CHUNK)
            file.seek(low)
            chunk = file.read(self.start - low)
            cut = 0 if low == 0 else chunk.find(b"\n") + 1  # first full line in the chunk
            if low == 0 or 0 < cut < len(chunk):
                break  # otherwise one line is longer than the chunk: read further back
        entries = _parse(chunk[cut:])
        for key, entry in entries:
            self.entries.setdefault(key, entry)
        if self.leaf is None and entries:
            self.leaf = entries[-1][0]
        self.start = low + cut
        return True

    def _refresh(self, file) -> None:
        size = os.fstat(file.fileno()).st_size
        if size < self.end:  # rewritten from scratch
            self.__init__(self.path)
        if self.end < 0:
            # Start at the end of the last complete line; the one after it may still be being written.
            end = size
            while end > 0:
                low = max(0, end - CHUNK)
                file.seek(low)
                newline = file.read(end - low).rfind(b"\n")
                if newline >= 0:
                    end = low + newline + 1
                    break
                end = low
            self.start = self.end = end
            return
        file.seek(self.end)
        data = file.read(size - self.end)
        data = data[: data.rfind(b"\n") + 1]  # the last line may still be being written
        for key, entry in _parse(data):
            self.entries[key] = entry
            self.leaf = key
        self.end += len(data)

    def blocks(self, wanted: int) -> tuple[list[str], bool]:
        """At least `wanted` newest Markdown blocks of the active branch, oldest first (consecutive tool
        calls share one), and whether older ones exist."""
        chain: list[list[str]] = []
        count, key, seen = 0, None, set()
        try:
            with open(self.path, "rb") as file:
                self._refresh(file)
                while self.leaf is None and self._older(file):
                    pass
                # Both formats are trees (rewinds/forks branch off): walk back from the leaf.
                key = self.leaf
                while key and key not in seen and count < wanted:
                    entry = self.entries.get(key)
                    if entry is None:
                        if self._older(file):
                            continue
                        break  # parent not in the file (e.g. compacted away): that is the root
                    seen.add(key)
                    chain.append(_blocks(entry))
                    count += len(chain[-1])
                    key = entry.get("parentId") or entry.get("parentUuid")
        except OSError:
            return [], False
        out: list[str] = []
        for block in (b for blocks in reversed(chain) for b in blocks):
            if out and block.startswith(TOOL_PREFIX) and out[-1].startswith(TOOL_PREFIX):
                out[-1] += "\n" + block
            else:
                out.append(block)
        return out, bool(key and key not in seen and (key in self.entries or self.start > 0))


def title(path: str) -> str:
    """The session name the harness gave it: omp keeps it on the first line, Claude Code appends ai-title entries."""
    try:
        stat = os.stat(path)
    except OSError:
        return ""
    return _title(path, stat.st_mtime_ns, stat.st_size)


@functools.lru_cache(maxsize=64)
def _title(path: str, _mtime_ns: int, size: int) -> str:
    with open(path, "rb") as file:
        first = file.readline()
        if first.startswith(b'{"type":"title"'):
            return json.loads(first).get("title", "")  # omp: rewritten in place on renames
        # Claude Code: the newest ai-title line, searched from the end.
        end = size
        while end > 0:
            low = max(0, end - CHUNK)
            file.seek(low)
            chunk = file.read(end - low + 4096)  # overlap so a line across the boundary is found whole
            at = chunk.rfind(b'"type":"ai-title"')
            if at >= 0:
                line = chunk[chunk.rfind(b"\n", 0, at) + 1 :].split(b"\n", 1)[0]
                try:
                    return json.loads(line).get("aiTitle", "")
                except ValueError:
                    return ""
            end = low
    return ""
