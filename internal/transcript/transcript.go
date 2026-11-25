// Package transcript turns an agent's JSONL session file (omp/pi or Claude Code) into Markdown blocks.
//
// Session files reach tens of MB, so Transcript reads from the end: only the newest part of the active
// branch is parsed, older parts on demand, and each poll parses just the appended lines.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Chunk is how many bytes are read per step when walking back through the file.
var Chunk int64 = 1 << 18

const toolPrefix = "- `"

// Tool-call argument shown next to the tool name, first match wins (omp's "i" is its intent line).
var summaryKeys = []string{"i", "description", "command", "file_path", "path", "pattern", "query", "url"}

type part struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"` // omp
	Input     map[string]any `json:"input"`     // Claude Code
}

type entry struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	UUID        string `json:"uuid"`
	ParentID    string `json:"parentId"`
	ParentUUID  string `json:"parentUuid"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // a string or a list of parts
	} `json:"message"`
}

func (e *entry) key() string    { return e.ID + e.UUID }
func (e *entry) parent() string { return e.ParentID + e.ParentUUID }

func toolLine(p part) string {
	args := p.Arguments
	if args == nil {
		args = p.Input
	}
	summary := ""
	for _, k := range summaryKeys {
		if s, ok := args[k].(string); ok {
			summary = s
			break
		}
	}
	summary = strings.Join(strings.Fields(summary), " ")
	if r := []rune(summary); len(r) > 100 {
		summary = string(r[:100])
	}
	name := p.Name
	if name == "" {
		name = "?"
	}
	return strings.TrimRight(fmt.Sprintf("%s%s` %s", toolPrefix, name, strings.ReplaceAll(summary, "`", "'")), " ")
}

func (e *entry) blocks() []string {
	if e.IsMeta || e.IsSidechain || e.Message == nil || (e.Type != "message" && e.Type != "user" && e.Type != "assistant") {
		return nil
	}
	role := e.Message.Role
	if role != "user" && role != "assistant" {
		return nil
	}
	var parts []part
	var text string
	if json.Unmarshal(e.Message.Content, &text) == nil {
		parts = []part{{Type: "text", Text: text}}
	} else if json.Unmarshal(e.Message.Content, &parts) != nil {
		return nil
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			t := strings.TrimSpace(p.Text)
			if t == "" {
				continue
			}
			if role == "user" {
				// Claude Code logs slash commands and their output as user messages.
				if strings.HasPrefix(t, "<command-") || strings.HasPrefix(t, "<local-command") {
					continue
				}
				t = "> " + strings.ReplaceAll(t, "\n", "\n> ")
			}
			out = append(out, t)
		case "toolCall", "tool_use":
			out = append(out, toolLine(p))
		}
	}
	return out
}

// parse returns the entries of complete lines that belong to the tree (omp: id, Claude Code: uuid).
func parse(data []byte) []*entry {
	var out []*entry
	for _, line := range bytes.Split(data, []byte("\n")) {
		var e entry
		if json.Unmarshal(line, &e) == nil && e.key() != "" {
			out = append(out, &e)
		}
	}
	return out
}

type Transcript struct {
	path       string
	entries    map[string]*entry
	leaf       string // last entry in the file: the tip of the active branch
	start, end int64  // parsed byte range; both on line boundaries, -1 = nothing read yet
}

func New(path string) *Transcript {
	return &Transcript{path: path, entries: map[string]*entry{}, start: -1, end: -1}
}

// older parses the lines before t.start; false when the whole file is already parsed.
func (t *Transcript) older(f *os.File) bool {
	if t.start <= 0 {
		return false
	}
	low := t.start
	var chunk []byte
	cut := 0
	for {
		low = max(0, low-Chunk)
		chunk = make([]byte, t.start-low)
		if _, err := f.ReadAt(chunk, low); err != nil && err != io.EOF {
			return false
		}
		if low == 0 {
			cut = 0
			break
		}
		cut = bytes.IndexByte(chunk, '\n') + 1 // first full line in the chunk
		if cut > 0 && cut < len(chunk) {
			break // otherwise one line is longer than the chunk: read further back
		}
	}
	entries := parse(chunk[cut:])
	for _, e := range entries {
		if _, seen := t.entries[e.key()]; !seen {
			t.entries[e.key()] = e
		}
	}
	if t.leaf == "" && len(entries) > 0 {
		t.leaf = entries[len(entries)-1].key()
	}
	t.start = low + int64(cut)
	return true
}

func (t *Transcript) refresh(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size < t.end { // rewritten from scratch
		*t = *New(t.path)
	}
	if t.end < 0 {
		// Start at the end of the last complete line; the one after it may still be being written.
		end := size
		for end > 0 {
			low := max(0, end-Chunk)
			buf := make([]byte, end-low)
			if _, err := f.ReadAt(buf, low); err != nil && err != io.EOF {
				return err
			}
			if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
				end = low + int64(i) + 1
				break
			}
			end = low
		}
		t.start, t.end = end, end
		return nil
	}
	data := make([]byte, size-t.end)
	if _, err := f.ReadAt(data, t.end); err != nil && err != io.EOF {
		return err
	}
	data = data[:bytes.LastIndexByte(data, '\n')+1] // the last line may still be being written
	for _, e := range parse(data) {
		t.entries[e.key()] = e
		t.leaf = e.key()
	}
	t.end += int64(len(data))
	return nil
}

// Blocks returns at least `wanted` newest Markdown blocks of the active branch, oldest first (consecutive
// tool calls share one), and whether older ones exist.
func (t *Transcript) Blocks(wanted int) ([]string, bool) {
	f, err := os.Open(t.path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if t.refresh(f) != nil {
		return nil, false
	}
	for t.leaf == "" && t.older(f) {
	}
	// Both formats are trees (rewinds/forks branch off): walk back from the leaf.
	var chain [][]string
	count, key, seen := 0, t.leaf, map[string]bool{}
	for key != "" && !seen[key] && count < wanted {
		e, ok := t.entries[key]
		if !ok {
			if t.older(f) {
				continue
			}
			break // parent not in the file (e.g. compacted away): that is the root
		}
		seen[key] = true
		chain = append(chain, e.blocks())
		count += len(chain[len(chain)-1])
		key = e.parent()
	}
	var out []string
	for i := len(chain) - 1; i >= 0; i-- {
		for _, b := range chain[i] {
			if n := len(out); n > 0 && strings.HasPrefix(b, toolPrefix) && strings.HasPrefix(out[n-1], toolPrefix) {
				out[n-1] += "\n" + b
			} else {
				out = append(out, b)
			}
		}
	}
	_, loaded := t.entries[key]
	return out, key != "" && !seen[key] && (loaded || t.start > 0)
}

type titleKey struct {
	path        string
	mtime, size int64
}

var titles sync.Map // titleKey -> string

// Title is the session name the harness gave it: omp keeps it on the first line, Claude Code appends
// ai-title entries.
func Title(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	key := titleKey{path, info.ModTime().UnixNano(), info.Size()}
	if v, ok := titles.Load(key); ok {
		return v.(string)
	}
	title := readTitle(path, info.Size())
	titles.Store(key, title)
	return title
}

func readTitle(path string, size int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, min(size, 4096))
	n, _ := f.ReadAt(head, 0)
	first, _, _ := bytes.Cut(head[:n], []byte("\n"))
	if bytes.Contains(first, []byte(`"type":"title"`)) {
		var omp struct{ Title string }
		json.Unmarshal(first, &omp) // omp: rewritten in place on renames
		return omp.Title
	}
	// Claude Code: the newest ai-title line, searched from the end.
	for end := size; end > 0; {
		low := max(0, end-Chunk)
		chunk := make([]byte, min(size, end+4096)-low) // overlap so a line across the boundary is found whole
		n, _ := f.ReadAt(chunk, low)
		chunk = chunk[:n]
		if at := bytes.LastIndex(chunk, []byte(`"type":"ai-title"`)); at >= 0 {
			line := chunk[bytes.LastIndexByte(chunk[:at], '\n')+1:]
			line, _, _ = bytes.Cut(line, []byte("\n"))
			var claude struct {
				AITitle string `json:"aiTitle"`
			}
			json.Unmarshal(line, &claude)
			return claude.AITitle
		}
		end = low
	}
	return ""
}
