// tmux-agents integration for omp/pi. Install: symlink into ~/.omp/agent/extensions/.
// Reports state changes and the session file to tmux, and serves a socket the dashboard uses to
// stream the in-progress reply and to read/write the prompt draft (protocol: src/tmux_agents/agent_socket.py).
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import { createServer, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

type Block = { type?: string; text?: string };
type Event = { toolName?: string; message?: { role?: string; content?: Block[] } };
type Ctx = {
  sessionManager?: { getSessionFile?: () => string | undefined };
  ui?: { getEditorText?: () => string; setEditorText?: (text: string) => void };
};
type Pi = { on(event: string, handler: (event: Event, ctx: Ctx) => void): void };

type Live = { text: string; thinking: boolean; tool: string; streaming: boolean };

export default function (pi: Pi): void {
  if (!process.env.TMUX_PANE) return;
  const main = process.env.TMUX_AGENTS || fromTmux();
  if (!main) return;
  // Subagents load extensions in the same process and pane; only the first (main) session reports.
  const owner = globalThis as { tmuxAgentsLoaded?: boolean };
  if (owner.tmuxAgentsLoaded) return;
  owner.tmuxAgentsLoaded = true;

  // Sync keeps transitions ordered and survives the process exiting right after session_shutdown; tmux calls take ms.
  const run = (...args: string[]) => {
    try {
      execFileSync(main, args, { stdio: "ignore" });
    } catch {}
  };

  let ctx: Ctx | undefined;
  let live: Live = { text: "", thinking: false, tool: "", streaming: false };
  const subscribers = new Set<Socket>();
  const push = (patch: Partial<Live>) => {
    live = { ...live, ...patch };
    const line = `${JSON.stringify(live)}\n`;
    for (const conn of subscribers) conn.write(line);
  };

  const socketPath = join(tmpdir(), `tmux-agents-${process.pid}.sock`);
  rmSync(socketPath, { force: true });
  const server = createServer((conn) => {
    let buffer = "";
    conn.setEncoding("utf8");
    conn.on("data", (chunk: string) => {
      buffer += chunk;
      for (let end = buffer.indexOf("\n"); end >= 0; end = buffer.indexOf("\n")) {
        const line = buffer.slice(0, end);
        buffer = buffer.slice(end + 1);
        try {
          handle(conn, JSON.parse(line));
        } catch {
          conn.end(`${JSON.stringify({ error: "bad request" })}\n`);
        }
      }
    });
    conn.on("close", () => subscribers.delete(conn));
    conn.on("error", () => subscribers.delete(conn));
  });
  const handle = (conn: Socket, request: { op?: string; text?: string }) => {
    if (request.op === "get_draft") {
      conn.end(`${JSON.stringify({ draft: ctx?.ui?.getEditorText?.() ?? "" })}\n`);
    } else if (request.op === "set_draft") {
      ctx?.ui?.setEditorText?.(request.text ?? "");
      conn.end(`${JSON.stringify({ ok: Boolean(ctx?.ui?.setEditorText) })}\n`);
    } else if (request.op === "subscribe") {
      subscribers.add(conn);
      conn.write(`${JSON.stringify(live)}\n`);
    } else {
      conn.end(`${JSON.stringify({ error: "unknown op" })}\n`);
    }
  };
  server.listen(socketPath);
  server.unref();

  const report = (c: Ctx) => {
    ctx = c;
    const file = c.sessionManager?.getSessionFile?.();
    if (file) run("session", file, socketPath);
  };
  const set = (state: string) => () => run("state", state);

  pi.on("session_start", (_, c) => report(c));
  pi.on("session_switch", (_, c) => report(c));
  pi.on("agent_start", set("working"));
  pi.on("agent_end", set("done"));
  pi.on("tool_approval_requested", set("blocked"));
  pi.on("tool_approval_resolved", set("working"));
  pi.on("message_update", (event) => {
    if (event.message?.role !== "assistant") return;
    const blocks = event.message.content ?? [];
    const text = blocks.filter((b) => b.type === "text").map((b) => b.text ?? "").join("\n\n");
    push({ text, thinking: blocks.at(-1)?.type === "thinking", streaming: true });
  });
  pi.on("message_end", (event) => {
    if (event.message?.role === "assistant") push({ text: "", thinking: false, streaming: false });
  });
  pi.on("tool_execution_start", (event) => {
    push({ tool: event.toolName ?? "tool" });
    // The ask tool waits for the user's answer.
    if (event.toolName === "ask") run("state", "blocked");
  });
  pi.on("tool_execution_end", (event) => {
    push({ tool: "" });
    if (event.toolName === "ask") run("state", "working");
  });
  pi.on("session_shutdown", () => {
    server.close();
    rmSync(socketPath, { force: true });
    run("state", "clear");
  });
}

// Panes opened before the plugin loaded lack TMUX_AGENTS; the server's global environment has it.
function fromTmux(): string | undefined {
  try {
    const out = execFileSync("tmux", ["show-environment", "-g", "TMUX_AGENTS"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
    return /^TMUX_AGENTS=(.+)$/m.exec(out)?.[1];
  } catch {
    return undefined;
  }
}
