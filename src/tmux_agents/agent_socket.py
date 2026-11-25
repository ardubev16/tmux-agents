"""Client for the socket an agent integration serves (omp: integrations/omp.ts).

Newline-delimited JSON. Requests: {"op": "get_draft"} -> {"draft": str},
{"op": "set_draft", "text": str} -> {"ok": true}, {"op": "subscribe"} -> a stream of
{"text": str, "thinking": bool, "tool": str, "streaming": bool} live-state snapshots.
"""

import asyncio
import json
from collections.abc import AsyncIterator

# Live snapshots carry the whole streaming message, which can outgrow asyncio's 64 KiB line default.
LIMIT = 16 * 1024 * 1024


async def request(path: str, **message: object) -> dict:
    reader, writer = await asyncio.open_unix_connection(path, limit=LIMIT)
    try:
        writer.write(json.dumps(message).encode() + b"\n")
        await writer.drain()
        return json.loads(await asyncio.wait_for(reader.readline(), 2))
    finally:
        writer.close()


async def subscribe(path: str) -> AsyncIterator[dict]:
    reader, writer = await asyncio.open_unix_connection(path, limit=LIMIT)
    try:
        writer.write(b'{"op": "subscribe"}\n')
        await writer.drain()
        while line := await reader.readline():
            yield json.loads(line)
    finally:
        writer.close()
