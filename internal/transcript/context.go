package transcript

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

// Todo is one item of the agent's todo list; Status as the harness writes it: completed, in_progress, pending, ….
type Todo struct{ Text, Status string }

// Context is what the agent is doing in the session (since its last /clear), to recall it at a glance: its latest
// todo list and its latest reply.
type Context struct {
	Plan []Todo
	Last string
}

// Context reads the active branch back to its root or last /clear; the first call reads the whole file.
func (t *Transcript) Context() Context {
	f, err := os.Open(t.path)
	if err != nil {
		return Context{}
	}
	defer f.Close()
	if t.refresh(f) != nil {
		return Context{}
	}
	for t.older(f) {
	}
	var chain []*entry // newest first
	for key, seen := t.leaf, map[string]bool{}; key != "" && !seen[key]; {
		e, ok := t.entries[key]
		if !ok || e.Type == "reset_boundary" {
			break
		}
		seen[key] = true
		chain = append(chain, e)
		key = e.parent()
	}
	var c Context
	for _, e := range slices.Backward(chain) {
		msg := e.Message
		if msg == nil || e.IsMeta || e.IsSidechain {
			continue
		}
		if msg.Role == "toolResult" { // omp: the todo tool reports the whole list in its details
			if msg.ToolName == "todo" {
				var d struct {
					Phases []struct {
						Tasks []struct{ Content, Status string }
					}
				}
				if json.Unmarshal(msg.Details, &d) == nil && len(d.Phases) > 0 {
					c.Plan = nil
					for _, phase := range d.Phases {
						for _, task := range phase.Tasks {
							c.Plan = append(c.Plan, Todo{task.Content, task.Status})
						}
					}
				}
			}
			continue
		}
		var parts []part
		if json.Unmarshal(msg.Content, &parts) != nil {
			if text := strings.TrimSpace(contentText(msg.Content)); msg.Role == "assistant" && text != "" {
				c.Last = text
			}
			continue
		}
		for _, p := range parts {
			switch {
			case p.Type == "text" && msg.Role == "assistant" && strings.TrimSpace(p.Text) != "":
				c.Last = strings.TrimSpace(p.Text)
			case p.Type == "tool_use" && p.Name == "TodoWrite": // Claude Code: the whole list every time
				var todos struct {
					Todos []struct{ Content, Status string }
				}
				if raw, err := json.Marshal(p.Input); err == nil && json.Unmarshal(raw, &todos) == nil {
					c.Plan = nil
					for _, todo := range todos.Todos {
						c.Plan = append(c.Plan, Todo{todo.Content, todo.Status})
					}
				}
			}
		}
	}
	return c
}
