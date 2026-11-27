package transcript

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Todo is one item of the agent's todo list; Status as the harness writes it: completed, in_progress, pending, ….
type Todo struct{ Text, Status string }

// Commit is one the agent made, as git (or omp's commit helper) reported it.
type Commit struct{ Hash, Subject string }

// Context is what the agent did in the session (since its last /clear), to recall it at a glance: its latest todo
// list, the commits it made (oldest first), the files it edited (most recent first), and its latest reply.
type Context struct {
	Plan    []Todo
	Commits []Commit
	Files   []string
	Last    string
}

var (
	// git commit's first line: [branch hash] subject.
	commitLine = regexp.MustCompile(`(?m)^\[[^\]\s]+ (?:\(root-commit\) )?([0-9a-f]{7,40})\] (.+)$`)
	// omp's edit tool names each file as [path#TAG].
	editHeader = regexp.MustCompile(`(?m)^\[([^\]#\n]+)#[0-9A-Za-z]{2,8}\]`)
	// Tools that write files, omp's and Claude Code's.
	editTools = map[string]bool{"edit": true, "write": true, "ast_edit": true, "Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}
)

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
	var files []string // in the order edited
	for _, e := range slices.Backward(chain) {
		msg := e.Message
		if msg == nil || e.IsMeta || e.IsSidechain {
			continue
		}
		if msg.Role == "toolResult" { // omp
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
			c.addCommits(contentText(msg.Content))
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
			switch p.Type {
			case "text":
				if text := strings.TrimSpace(p.Text); msg.Role == "assistant" && text != "" {
					c.Last = text
				}
			case "toolCall", "tool_use":
				args := p.Arguments
				if args == nil {
					args = p.Input
				}
				if p.Name == "TodoWrite" { // Claude Code: the whole list every time
					var todos struct {
						Todos []struct{ Content, Status string }
					}
					if raw, err := json.Marshal(args); err == nil && json.Unmarshal(raw, &todos) == nil {
						c.Plan = nil
						for _, todo := range todos.Todos {
							c.Plan = append(c.Plan, Todo{todo.Content, todo.Status})
						}
					}
				}
				if editTools[p.Name] {
					for _, k := range []string{"path", "file_path", "notebook_path"} {
						if path, ok := args[k].(string); ok && path != "" {
							files = append(files, path)
						}
					}
					if input, ok := args["input"].(string); ok {
						for _, m := range editHeader.FindAllStringSubmatch(input, -1) {
							files = append(files, m[1])
						}
					}
				}
			case "tool_result": // Claude Code
				c.addCommits(contentText(p.Content))
			}
		}
	}
	seen := map[string]bool{}
	for _, path := range slices.Backward(files) {
		if !seen[path] {
			seen[path] = true
			c.Files = append(c.Files, path)
		}
	}
	return c
}

func (c *Context) addCommits(output string) {
	for _, m := range commitLine.FindAllStringSubmatch(output, -1) {
		c.Commits = append(c.Commits, Commit{m[1], strings.TrimSpace(m[2])})
	}
}
