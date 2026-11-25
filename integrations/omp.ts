// tmux-agents state reporting for omp/pi. Install: symlink into ~/.omp/agent/extensions/.
import { execFileSync } from "node:child_process";

type ToolEvent = { toolName?: string };
type Ctx = { sessionManager?: { getSessionFile?: () => string | undefined } };
type Pi = { on(event: string, handler: (event: ToolEvent, ctx: Ctx) => void): void };

export default function (pi: Pi): void {
  if (!process.env.TMUX_PANE) return;
  const main = process.env.TMUX_AGENTS || fromTmux();
  if (!main) return;

  // Sync keeps transitions ordered and survives the process exiting right after session_shutdown; tmux calls take ms.
  // The session file lets the dashboard render the chat itself; it follows /new and /resume on the next state change.
  const set = (state: string, ctx?: Ctx) => {
    try {
      execFileSync(main, ["state", state, ctx?.sessionManager?.getSessionFile?.() ?? ""], { stdio: "ignore" });
    } catch {}
  };

  pi.on("agent_start", (_, ctx) => set("working", ctx));
  pi.on("agent_end", (_, ctx) => set("done", ctx));
  pi.on("tool_approval_requested", (_, ctx) => set("blocked", ctx));
  pi.on("tool_approval_resolved", (_, ctx) => set("working", ctx));
  // The ask tool waits for the user's answer.
  pi.on("tool_execution_start", (event, ctx) => event.toolName === "ask" && set("blocked", ctx));
  pi.on("tool_execution_end", (event, ctx) => event.toolName === "ask" && set("working", ctx));
  pi.on("session_shutdown", () => set("clear"));
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
