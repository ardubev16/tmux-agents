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
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Chunk is how many bytes are read per step when walking back through the file.
var Chunk int64 = 1 << 18

// Tool-call argument shown next to the tool name, first match wins (omp's "i" is its intent line).
var summaryKeys = []string{"i", "description", "command", "file_path", "path", "pattern", "query", "url"}

// Block is one block of the chat: Markdown, a prompt of the user's (plain text), or a step (thinking, a tool
// call) folded to an Icon, the tool's Name and a one-line Summary, with its full Detail (Markdown: the
// thoughts, or the arguments and result) shown when expanded.
type Block struct {
	Key                         string // stable across reads, so a step stays expanded
	Markdown                    string
	Prompt                      bool // Markdown is what the user typed, shown as typed
	Icon, Name, Summary, Detail string
	call, resultOf              string // tool call id: a result is a block of its own until attached to its call
}

// ThinkingStep folds thoughts to their first line.
func ThinkingStep(thoughts string) Block {
	thoughts = strings.TrimSpace(thoughts)
	return Block{Icon: "∴", Summary: firstLine(thoughts), Detail: thoughts}
}

type part struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments"`   // omp
	Input     map[string]any  `json:"input"`       // Claude Code
	ToolUseID string          `json:"tool_use_id"` // Claude Code tool_result
	Content   json.RawMessage `json:"content"`     // Claude Code tool_result: a string or a list of parts
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
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`    // a string or a list of parts
		ToolCallID string          `json:"toolCallId"` // omp toolResult
	} `json:"message"`
}

func (e *entry) key() string    { return e.ID + e.UUID }
func (e *entry) parent() string { return e.ParentID + e.ParentUUID }

func toolStep(p part) Block {
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
	name := p.Name
	if name == "" {
		name = "?"
	}
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(args)) {
		v, ok := args[k].(string)
		if k == "i" {
			continue // omp's intent line: the summary already
		}
		if !ok {
			b, _ := json.Marshal(args[k])
			v = string(b)
		}
		lines = append(lines, k+": "+strings.ReplaceAll(v, "\n", "\n  "))
	}
	return Block{Icon: "⚙", Name: name, Summary: strings.Join(strings.Fields(summary), " "), Detail: fence(strings.Join(lines, "\n")), call: p.ID}
}

// fence puts s in a code block whose fence is longer than any run of backticks in s; "" stays "". Tabs become
// spaces: the view measures a tab as one column, the terminal as up to eight.
func fence(s string) string {
	if s = strings.ReplaceAll(strings.Trim(s, "\n"), "\t", "    "); s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", max(3, longest+1))
	return f + "\n" + s + "\n" + f
}

// contentText is the text of message content: a string or a list of parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []part
	_ = json.Unmarshal(raw, &parts) // unreadable content has no text
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// firstLine is the first non-empty line of Markdown, without heading or bold markers.
func firstLine(md string) string {
	for _, line := range strings.Split(md, "\n") {
		if line = strings.Trim(line, "*# \t"); line != "" {
			return line
		}
	}
	return ""
}

// question renders what an ask tool asks the user: omp's HTML form or Claude Code's (and older omp's) list of
// questions. The answers the agent prefilled (checked options, input values, textarea text) are left out.
func question(p part) string {
	if p.Name != "ask" && p.Name != "AskUserQuestion" {
		return ""
	}
	args := p.Arguments
	if args == nil {
		args = p.Input
	}
	var blocks []string
	if form, ok := args["html"].(string); ok {
		blocks = formBlocks(form)
	}
	questions, _ := args["questions"].([]any)
	for _, q := range questions {
		q, _ := q.(map[string]any)
		if text, _ := q["question"].(string); text != "" {
			blocks = append(blocks, text)
		}
		options, _ := q["options"].([]any)
		for _, o := range options {
			o, _ := o.(map[string]any)
			item, _ := o["label"].(string)
			if desc, _ := o["description"].(string); desc != "" {
				item += " — " + desc
			}
			blocks = append(blocks, "- "+strings.Join(strings.Fields(item), " "))
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	// The heading sets the question apart from the agent's prose.
	md := "**Question**"
	for i, block := range blocks {
		if i > 0 && strings.HasPrefix(block, "- ") && strings.HasPrefix(blocks[i-1], "- ") {
			md += "\n" + block // one list
		} else {
			md += "\n\n" + block
		}
	}
	return md
}

// Form elements whose content is an answer (or nothing to read) rather than part of the question.
var notQuestion = map[atom.Atom]bool{atom.Textarea: true, atom.Script: true, atom.Style: true, atom.Svg: true, atom.Button: true}

// formBlocks turns a form into Markdown blocks: text, legends in bold, choices and fields as list items.
func formBlocks(form string) []string {
	doc, err := html.Parse(strings.NewReader(form))
	if err != nil {
		return nil
	}
	var blocks []string
	add := func(format, text string) {
		if text != "" {
			blocks = append(blocks, fmt.Sprintf(format, text))
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			add("%s", inlineText(n)) // loose text outside any block
			return
		case n.Type != html.ElementNode && n.Type != html.DocumentNode, notQuestion[n.DataAtom]:
			return
		}
		switch n.DataAtom {
		case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			add("%s", inlineText(n))
			return
		case atom.Legend:
			add("**%s**", inlineText(n))
			return
		case atom.Li, atom.Option, atom.Tr:
			add("- %s", inlineText(n))
			return
		case atom.Pre:
			add("```\n%s\n```", strings.Trim(rawText(n), "\n"))
			return
		case atom.Label:
			add("- %s", inlineText(n))
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.DataAtom == atom.Select {
					walk(c) // its options, below the label
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return blocks
}

// inlineText is a node's text on one line: code in backticks, table cells apart, selects and answers left out.
func inlineText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(n.Data)
			return
		case n.Type != html.ElementNode || notQuestion[n.DataAtom] || n.DataAtom == atom.Select:
			return
		case n.DataAtom == atom.Code:
			b.WriteString("`" + strings.Join(strings.Fields(rawText(n)), " ") + "`")
			return
		case n.DataAtom == atom.Td || n.DataAtom == atom.Th:
			b.WriteString(" · ")
		case n.DataAtom == atom.Br:
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	if n.Type == html.TextNode {
		walk(n)
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	return strings.TrimPrefix(strings.Join(strings.Fields(b.String()), " "), "· ")
}

func rawText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(rawText(c))
	}
	return b.String()
}

func (e *entry) blocks() []Block {
	if e.IsMeta || e.IsSidechain || e.Message == nil || (e.Type != "message" && e.Type != "user" && e.Type != "assistant") {
		return nil
	}
	role := e.Message.Role
	if role == "toolResult" { // omp
		if e.Message.ToolCallID == "" {
			return nil
		}
		return []Block{{resultOf: e.Message.ToolCallID, Detail: contentText(e.Message.Content)}}
	}
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
	var out []Block
	for i, p := range parts {
		key := fmt.Sprint(e.key(), "/", i)
		switch p.Type {
		case "text":
			t := strings.TrimSpace(p.Text)
			if t == "" {
				continue
			}
			// Claude Code logs slash commands and their output as user messages.
			if role == "user" && (strings.HasPrefix(t, "<command-") || strings.HasPrefix(t, "<local-command")) {
				continue
			}
			out = append(out, Block{Key: key, Markdown: t, Prompt: role == "user"})
		case "thinking":
			if strings.TrimSpace(p.Thinking) != "" {
				b := ThinkingStep(p.Thinking)
				b.Key = key
				out = append(out, b)
			}
		case "toolCall", "tool_use":
			b := toolStep(p)
			b.Key = key
			out = append(out, b)
			if q := question(p); q != "" {
				out = append(out, Block{Key: key + "?", Markdown: q})
			}
		case "tool_result": // Claude Code
			if p.ToolUseID != "" {
				out = append(out, Block{resultOf: p.ToolUseID, Detail: contentText(p.Content)})
			}
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

// Blocks returns at least `wanted` newest blocks of the active branch, oldest first, each tool result in its
// call's Detail, and whether older ones exist.
func (t *Transcript) Blocks(wanted int) ([]Block, bool) {
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
	var chain [][]Block
	count, key, seen := 0, t.leaf, map[string]bool{}
	for key != "" && !seen[key] && count < wanted {
		e, ok := t.entries[key]
		if !ok {
			if t.older(f) {
				continue
			}
			break // parent not in the file (e.g. compacted away): that is the root
		}
		if e.Type == "reset_boundary" { // omp /clear: what came before is out of the agent's context
			key = ""
			break
		}
		seen[key] = true
		chain = append(chain, e.blocks())
		count += len(chain[len(chain)-1])
		key = e.parent()
	}
	out := []Block{}          // not nil, even for a session without messages yet: nil keeps the chat shown before
	calls := map[string]int{} // tool call id -> its block in out
	for i := len(chain) - 1; i >= 0; i-- {
		for _, b := range chain[i] {
			switch {
			case b.resultOf != "":
				if at, ok := calls[b.resultOf]; ok {
					out[at].Detail = strings.Trim(out[at].Detail+"\n\n"+fence(b.Detail), "\n")
				}
			case b.call != "":
				calls[b.call] = len(out)
				fallthrough
			default:
				out = append(out, b)
			}
		}
	}
	_, loaded := t.entries[key]
	return out, key != "" && !seen[key] && (loaded || t.start > 0)
}

type titleEntry struct {
	mtime, size int64
	searched    int64 // Claude Code: bytes up to here (a line boundary) were searched for ai-title lines
	title       string
}

var titles sync.Map // path -> titleEntry

// Title is the session name the harness gave it: omp keeps it on the first line, Claude Code appends
// ai-title entries. Called every second per agent, so a changed file is only searched where it grew.
func Title(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	v, _ := titles.Load(path)
	e, _ := v.(titleEntry)
	mtime, size := info.ModTime().UnixNano(), info.Size()
	if e.mtime == mtime && e.size == size {
		return e.title
	}
	if size < e.searched { // rewritten from scratch
		e = titleEntry{}
	}
	e.title, e.searched = readTitle(path, size, e.searched, e.title)
	e.mtime, e.size = mtime, size
	titles.Store(path, e)
	return e.title
}

// readTitle returns the title and how far the file has now been searched for ai-title lines; Claude Code
// files are searched backwards from the end down to from, keeping title when no newer one turns up.
func readTitle(path string, size, from int64, title string) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return title, from
	}
	defer f.Close()
	head := make([]byte, min(size, 4096))
	n, _ := f.ReadAt(head, 0)
	first, _, _ := bytes.Cut(head[:n], []byte("\n"))
	if bytes.Contains(first, []byte(`"type":"title"`)) {
		var omp struct{ Title string }
		_ = json.Unmarshal(first, &omp) // omp: rewritten in place on renames; unreadable means no title
		return omp.Title, 0
	}
	searched := from
	pattern := []byte(`"type":"ai-title"`)
	for end := size; end > from; {
		low := max(from, end-Chunk)
		chunk := make([]byte, min(size, end+4096)-low) // overlap so a line across the boundary is found whole
		n, _ := f.ReadAt(chunk, low)
		chunk = chunk[:n]
		if end == size {
			// The last line may still be being written: search it again next time.
			if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
				searched = low + int64(i) + 1
			}
		}
		for at := bytes.LastIndex(chunk, pattern); at >= 0; at = bytes.LastIndex(chunk[:at], pattern) {
			line, _, complete := bytes.Cut(chunk[bytes.LastIndexByte(chunk[:at], '\n')+1:], []byte("\n"))
			var claude struct {
				AITitle string `json:"aiTitle"`
			}
			if complete && json.Unmarshal(line, &claude) == nil {
				return claude.AITitle, searched
			}
		}
		end = low
	}
	return title, searched
}
