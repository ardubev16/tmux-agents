// tmux-agents state reporting for omp/pi. Install: symlink into ~/.omp/agent/extensions/.
import { execFileSync } from "node:child_process";

type ToolEvent = { toolName?: string };
type Pi = { on(event: string, handler: (event: ToolEvent) => void): void };

export default function (pi: Pi): void {
  const main = process.env.TMUX_AGENTS;
  if (!main || !process.env.TMUX_PANE) return;

  // Sync keeps transitions ordered and survives the process exiting right after session_shutdown; tmux calls take ms.
  const set = (state: string) => {
    try {
      execFileSync(main, ["state", state], { stdio: "ignore" });
    } catch {}
  };

  pi.on("agent_start", () => set("working"));
  pi.on("agent_end", () => set("done"));
  pi.on("tool_approval_requested", () => set("blocked"));
  pi.on("tool_approval_resolved", () => set("working"));
  // The ask tool waits for the user's answer.
  pi.on("tool_execution_start", (event) => event.toolName === "ask" && set("blocked"));
  pi.on("tool_execution_end", (event) => event.toolName === "ask" && set("working"));
  pi.on("session_shutdown", () => set("clear"));
}
