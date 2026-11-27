// tmux-agents integration for omp/pi. Install: symlink into ~/.omp/agent/extensions/.
// Reports state changes and the session file to tmux, serves a socket the dashboard uses to stream the
// in-progress reply and to read/write the prompt draft (protocol: internal/agentsocket), and gives the agent a
// tool to keep its status (the dashboard's context card) current.
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import { createServer, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Type } from "@sinclair/typebox";

type Block = { type?: string; text?: string; thinking?: string };
type Event = {
  toolName?: string;
  text?: string;
  prompt?: string;
  systemPrompt?: string | string[];
  message?: { role?: string; content?: Block[] };
};
type Ctx = {
  hasUI?: boolean;
  sessionManager?: { getSessionFile?: () => string | undefined };
  ui?: { getEditorText?: () => string; setEditorText?: (text: string) => void };
};
type Tool = {
  name: string;
  label: string;
  description: string;
  parameters: unknown;
  execute(
    id: string,
    params: { doing: string; needs?: string },
    signal: unknown,
    onUpdate: unknown,
    ctx?: Ctx,
  ): Promise<unknown>;
};
type Pi = { on(event: string, handler: (event: Event, ctx: Ctx) => unknown): void; registerTool(tool: Tool): void };

// The tool's description alone does not get it called every turn, so this goes in the system prompt too.
const statusInstruction =
  "The user follows many agents in the tmux-agents dashboard. At the end of every turn, before your final reply, call " +
  "tmux_status: doing = what you are working on overall (the goal, not the last step), needs = what you need from " +
  "the user (omit it if nothing). At most 12 words each.";

// queued: prompts submitted while the agent runs; omp writes them to the session file only when it delivers them.
// thoughts: the streaming message's thinking so far.
type Live = { text: string; thinking: boolean; thoughts: string; tool: string; streaming: boolean; queued: string[] };
// One per process, kept on globalThis so an extension reload keeps the socket and its subscribers.
type Shared = {
  ctx?: Ctx;
  live: Live;
  running: boolean;
  subscribers: Set<Socket>;
  socketPath: string;
  file?: string;
  watch?: NodeJS.Timeout;
};

export default function (pi: Pi): void {
  if (!process.env.TMUX_PANE) return;
  const main = mainSh();
  if (!main) return;
  const global = globalThis as { tmuxAgents?: Shared };
  global.tmuxAgents ??= serve();
  const shared = global.tmuxAgents;

  // Sync keeps transitions ordered and survives the process exiting right after session_shutdown; tmux calls take ms.
  const run = (...args: string[]) => {
    try {
      execFileSync(main, args, { stdio: "ignore" });
    } catch {}
  };
  const push = (patch: Partial<Live>) => {
    shared.live = { ...shared.live, ...patch };
    const line = `${JSON.stringify(shared.live)}\n`;
    for (const conn of shared.subscribers) conn.write(line);
  };
  // Subagents run in this process and pane too, without a UI; only the interactive session reports.
  const on = (event: string, handler: (event: Event, ctx: Ctx) => unknown) =>
    pi.on(event, (e, c) => (c?.hasUI !== false ? handler(e, c) : undefined));

  const report = (c: Ctx) => {
    shared.ctx = c;
    shared.file = c.sessionManager?.getSessionFile?.();
    if (shared.file) run("session", shared.file, shared.socketPath);
  };

  // /move relocates the session file without any event to extensions: notice the path changing.
  clearInterval(shared.watch);
  shared.watch = setInterval(() => {
    try {
      if (shared.ctx && shared.ctx.sessionManager?.getSessionFile?.() !== shared.file) report(shared.ctx);
    } catch {}
  }, 1000);
  shared.watch.unref();

  on("session_start", (_, c) => report(c));
  on("session_switch", (_, c) => report(c));
  // /clear keeps the session file but not what the agent was doing.
  on("session_clear", () => run("status"));
  pi.registerTool({
    name: "tmux_status",
    label: "Status",
    description:
      "Shows the user what you are doing in their tmux-agents dashboard; call it at the end of every turn. doing: " +
      "what you are working on overall (the goal, not the last step); needs: what you need from the user, omitted " +
      "if nothing. At most 12 words each.",
    parameters: Type.Object({
      doing: Type.String({ description: "what you are working on overall, at most 12 words" }),
      needs: Type.Optional(
        Type.String({ description: "what you need from the user, at most 12 words; empty if nothing" }),
      ),
    }),
    async execute(_id, params, _signal, _onUpdate, ctx) {
      // Subagents run in this process and pane too: only the interactive session's status is the pane's.
      if (ctx?.hasUI !== false) run("status", params.doing, params.needs ?? "");
      return { content: [{ type: "text", text: "Status shown in the dashboard." }] };
    },
  });
  on("agent_start", (_, c) => {
    shared.ctx = c;
    shared.running = true;
    run("state", "working");
  });
  on("agent_end", () => {
    shared.running = false;
    run("state", "done");
  });
  on("input", (event) => {
    const text = event.text?.trim();
    // Slash commands and !bash run right away instead of queueing.
    if (shared.running && text && !/^[/!]/.test(text)) push({ queued: [...shared.live.queued, text] });
  });
  // A queued prompt reaches the agent with the next run (before_agent_start) or, steering, mid-run as a user message.
  const delivered = (prompt: string) => {
    const queued = shared.live.queued;
    if (queued.some((q) => prompt.includes(q))) push({ queued: queued.filter((q) => !prompt.includes(q)) });
  };
  on("before_agent_start", (event) => {
    delivered(event.prompt ?? "");
    const sp = event.systemPrompt;
    if (sp === undefined) return undefined;
    return { systemPrompt: Array.isArray(sp) ? [...sp, statusInstruction] : `${sp}\n\n${statusInstruction}` };
  });
  on("tool_approval_requested", () => run("state", "blocked"));
  on("tool_approval_resolved", () => run("state", "working"));
  on("message_update", (event) => {
    if (event.message?.role !== "assistant") return;
    const blocks = event.message.content ?? [];
    const texts: string[] = [];
    const thoughts: string[] = [];
    for (const b of blocks) {
      if (b.type === "text") texts.push(b.text ?? "");
      if (b.type === "thinking") thoughts.push(b.thinking ?? "");
    }
    push({
      text: texts.join("\n\n"),
      thoughts: thoughts.join("\n\n"),
      thinking: blocks.at(-1)?.type === "thinking",
      streaming: true,
    });
  });
  on("message_end", (event) => {
    if (event.message?.role === "assistant") push({ text: "", thoughts: "", thinking: false, streaming: false });
    if (event.message?.role === "user") delivered((event.message.content ?? []).map((b) => b.text ?? "").join("\n"));
  });
  on("tool_execution_start", (event) => {
    push({ tool: event.toolName ?? "tool" });
    // The ask tool waits for the user's answer.
    if (event.toolName === "ask") run("state", "blocked");
  });
  on("tool_execution_end", (event) => {
    push({ tool: "" });
    if (event.toolName === "ask") run("state", "working");
  });
  on("session_shutdown", () => run("state", "clear"));
}

function serve(): Shared {
  const shared: Shared = {
    live: { text: "", thinking: false, thoughts: "", tool: "", streaming: false, queued: [] },
    running: false,
    subscribers: new Set(),
    socketPath: join(tmpdir(), `tmux-agents-${process.pid}.sock`),
  };
  const handle = (conn: Socket, request: { op?: string; text?: string }) => {
    const ui = shared.ctx?.ui;
    if (request.op === "get_draft") {
      conn.end(`${JSON.stringify({ draft: ui?.getEditorText?.() ?? "" })}\n`);
    } else if (request.op === "set_draft") {
      ui?.setEditorText?.(request.text ?? "");
      conn.end(`${JSON.stringify({ ok: Boolean(ui?.setEditorText) })}\n`);
    } else if (request.op === "subscribe") {
      shared.subscribers.add(conn);
      conn.write(`${JSON.stringify(shared.live)}\n`);
    } else {
      conn.end(`${JSON.stringify({ error: "unknown op" })}\n`);
    }
  };
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
    conn.on("close", () => shared.subscribers.delete(conn));
    conn.on("error", () => shared.subscribers.delete(conn));
  });
  // Without the socket the dashboard still shows the session file; never let it take the agent down.
  server.on("error", () => {});
  const removeSocket = () => {
    try {
      rmSync(shared.socketPath, { force: true });
    } catch {}
  };
  removeSocket();
  server.listen(shared.socketPath);
  server.unref();
  // Normal exits only: a pane killed from the dashboard (SIGHUP) leaves a stale file, which the dashboard ignores.
  process.on("exit", removeSocket);
  return shared;
}

// The path to main.sh, from the tmux server's global environment: always current, also in panes opened before
// the plugin loaded or after it moved.
function mainSh(): string | undefined {
  try {
    const out = execFileSync("tmux", ["show-environment", "-g", "TMUX_AGENTS"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    return /^TMUX_AGENTS=(.+)$/m.exec(out)?.[1];
  } catch {
    return undefined;
  }
}
