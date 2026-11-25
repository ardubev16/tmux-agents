"""Turn an agent's JSONL session file (omp/pi or Claude Code) into Markdown blocks for the dashboard."""

import json
from pathlib import Path

# Tool-call argument shown next to the tool name, first match wins (omp's "i" is its intent line).
SUMMARY_KEYS = ("i", "description", "command", "file_path", "path", "pattern", "query", "url")
TOOL_PREFIX = "- `"


def _active_branch(entries: list[dict]) -> list[dict]:
    """Both formats are trees (rewinds/forks branch off): walk back from the last entry to the root."""
    by_id = {e.get("id") or e.get("uuid"): e for e in entries}
    by_id.pop(None, None)
    leaf = next((e for e in reversed(entries) if (e.get("id") or e.get("uuid")) in by_id), None)
    branch, seen = [], set()
    while leaf is not None and id(leaf) not in seen:
        seen.add(id(leaf))
        branch.append(leaf)
        leaf = by_id.get(leaf.get("parentId") or leaf.get("parentUuid"))
    return branch[::-1]


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


def to_blocks(path: str) -> list[str]:
    """Markdown blocks of the conversation, oldest first; consecutive tool calls share one block."""
    entries = []
    if not Path(path).is_file():
        return []
    for line in Path(path).read_text(errors="replace").splitlines():
        try:
            entries.append(json.loads(line))
        except json.JSONDecodeError:
            continue  # a line still being written
    out: list[str] = []
    for block in (b for e in _active_branch(entries) for b in _blocks(e)):
        if out and block.startswith(TOOL_PREFIX) and out[-1].startswith(TOOL_PREFIX):
            out[-1] += "\n" + block
        else:
            out.append(block)
    return out
