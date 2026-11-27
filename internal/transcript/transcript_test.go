package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type m = map[string]any

func writeJSONL(t *testing.T, name string, entries ...m) string {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func omp(id string, parent any, role string, content ...m) m {
	return m{"type": "message", "id": id, "parentId": parent, "message": m{"role": role, "content": content}}
}

func text(s string) m { return m{"type": "text", "text": s} }

// texts is each block as its Markdown, a prompt quoted, or a step as its icon, name and summary.
func texts(blocks []Block, more bool) ([]string, bool) {
	var out []string
	for _, b := range blocks {
		switch {
		case b.Icon != "":
			out = append(out, strings.Join(strings.Fields(b.Icon+" "+b.Name+" "+b.Summary), " "))
		case b.Prompt:
			out = append(out, "> "+b.Markdown)
		default:
			out = append(out, b.Markdown)
		}
	}
	return out, more
}

func all(t *testing.T, path string) []string {
	t.Helper()
	out, _ := texts(New(path).Blocks(1 << 30))
	return out
}

func TestOmpShowsOnlyTheActiveBranch(t *testing.T) {
	path := writeJSONL(t, "s.jsonl",
		m{"type": "session", "id": "s"},
		omp("1", nil, "user", text("fix it")),
		omp("2", "1", "assistant", text("abandoned answer")),
		omp("3", "1", "assistant", m{"type": "thinking", "thinking": "**Looking**\n\nhmm"}, text("rewound answer")),
		omp("4", "3", "assistant", m{"type": "toolCall", "id": "c1", "name": "bash", "arguments": m{"command": "ls\n-la"}}),
		m{"type": "message", "id": "5", "parentId": "4", "message": m{"role": "toolResult", "toolCallId": "c1", "content": []m{text("file list")}}},
		omp("6", "5", "assistant", m{"type": "toolCall", "name": "read", "arguments": m{"i": "Reading `x`"}}),
	)
	want := []string{"> fix it", "∴ Looking", "rewound answer", "⚙ bash ls -la", "⚙ read Reading `x`"}
	if got := all(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	blocks, _ := New(path).Blocks(100)
	if got, want := blocks[1].Detail, "**Looking**\n\nhmm"; got != want {
		t.Errorf("thinking detail %q, want %q", got, want)
	}
	if got, want := blocks[3].Detail, "```\ncommand: ls\n  -la\n```\n\n```\nfile list\n```"; got != want {
		t.Errorf("tool detail %q, want %q", got, want)
	}
}

func TestOmpClearStartsOver(t *testing.T) {
	path := writeJSONL(t, "s.jsonl",
		omp("1", nil, "user", text("old task")),
		m{"type": "reset_boundary", "id": "2", "parentId": "1"},
		omp("3", "2", "user", text("new task")),
	)
	got, more := texts(New(path).Blocks(100))
	if want := []string{"> new task"}; !reflect.DeepEqual(got, want) || more {
		t.Errorf("got %q (more %v), want %q", got, more, want)
	}
}

func TestOmpNewSessionWithoutMessagesIsEmptyNotNil(t *testing.T) {
	path := writeJSONL(t, "s.jsonl",
		m{"type": "session", "id": "s"},
		m{"type": "thinking_level_change", "id": "1", "parentId": nil},
	)
	if blocks, _ := New(path).Blocks(100); blocks == nil || len(blocks) != 0 {
		t.Errorf("got %#v, want an empty non-nil slice", blocks)
	}
}

func TestContextOmp(t *testing.T) {
	todo := func(id, parent string, tasks ...m) []m {
		return []m{
			omp(id, parent, "assistant", m{"type": "toolCall", "id": "c" + id, "name": "todo", "arguments": m{"op": "init"}}),
			{"type": "message", "id": id + "r", "parentId": id, "message": m{"role": "toolResult", "toolCallId": "c" + id,
				"toolName": "todo", "content": []m{text("Remaining items")}, "details": m{"phases": []m{{"name": "A", "tasks": tasks}}}}},
		}
	}
	entries := append([]m{omp("1", nil, "user", text("build it"))},
		todo("2", "1", m{"content": "one", "status": "completed"}, m{"content": "two", "status": "in_progress"})...)
	entries = append(entries, omp("3", "2r", "assistant", text("It works now."), text("Tests pass.")))
	want := Context{Plan: []Todo{{"one", "completed"}, {"two", "in_progress"}}, Last: "Tests pass."}
	if c := New(writeJSONL(t, "s.jsonl", entries...)).Context(); !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}

	// After a /clear, the plan from before it is gone.
	entries = append(entries, m{"type": "reset_boundary", "id": "4", "parentId": "3"}, omp("5", "4", "assistant", text("Fresh start.")))
	want = Context{Last: "Fresh start."}
	if c := New(writeJSONL(t, "s.jsonl", entries...)).Context(); !reflect.DeepEqual(c, want) {
		t.Errorf("after /clear: got %+v\nwant %+v", c, want)
	}
}

func TestContextClaude(t *testing.T) {
	path := writeJSONL(t, "c.jsonl",
		m{"type": "user", "uuid": "1", "message": m{"role": "user", "content": "build it"}},
		m{"type": "assistant", "uuid": "2", "parentUuid": "1", "message": m{"role": "assistant", "content": []m{
			{"type": "tool_use", "id": "t1", "name": "TodoWrite", "input": m{"todos": []m{{"content": "one", "status": "pending", "activeForm": "Doing one"}}}},
		}}},
		m{"type": "assistant", "uuid": "3", "parentUuid": "2", "message": m{"role": "assistant", "content": "Done."}},
	)
	want := Context{Plan: []Todo{{"one", "pending"}}, Last: "Done."}
	if c := New(path).Context(); !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
}

func TestClaude(t *testing.T) {
	path := writeJSONL(t, "c.jsonl",
		m{"type": "user", "uuid": "1", "parentUuid": nil, "message": m{"role": "user", "content": "hi\nthere"}},
		m{"type": "user", "uuid": "2", "parentUuid": "1", "isMeta": true, "message": m{"role": "user", "content": "x"}},
		m{"type": "assistant", "uuid": "3", "parentUuid": "2", "message": m{"role": "assistant", "content": []m{
			text("**hello**"), {"type": "tool_use", "id": "t1", "name": "Read", "input": m{"file_path": "/a"}},
		}}},
		m{"type": "user", "uuid": "4", "parentUuid": "3", "message": m{"role": "user", "content": []m{{"type": "tool_result", "tool_use_id": "t1", "content": "x ``` y"}}}},
		m{"type": "user", "uuid": "5", "parentUuid": "4", "message": m{"role": "user", "content": "<command-name>/clear</command-name>"}},
	)
	want := []string{"> hi\nthere", "**hello**", "⚙ Read /a"}
	if got := all(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	blocks, _ := New(path).Blocks(100)
	if got, want := blocks[2].Detail, "```\nfile_path: /a\n```\n\n````\nx ``` y\n````"; got != want {
		t.Errorf("tool detail %q, want %q", got, want)
	}
}

func TestReadsFromTheEndAndFollowsAppends(t *testing.T) {
	defer func(c int64) { Chunk = c }(Chunk)
	Chunk = 64 // many backward steps, and lines longer than a chunk
	entries := []m{omp("0", nil, "user", text("root"))}
	for i := 1; i < 30; i++ {
		entries = append(entries, omp(itoa(i), itoa(i-1), "assistant", text("answer "+itoa(i)+" "+strings.Repeat("x", i%3*50))))
	}
	entries = append(entries, omp("rewound", "2", "assistant", text("after rewind"))) // branches off far back
	path := writeJSONL(t, "s.jsonl", entries...)

	reader := New(path)
	blocks, more := texts(reader.Blocks(2))
	if len(blocks) != 2 || !strings.HasPrefix(blocks[0], "answer 2") || blocks[1] != "after rewind" || !more {
		t.Fatalf("tail: %q more=%v", blocks, more)
	}
	if reader.start == 0 {
		t.Error("the head of the file should not have been parsed")
	}
	want := []string{"> root", "answer 1 " + strings.Repeat("x", 50), "answer 2 " + strings.Repeat("x", 100), "after rewind"}
	if blocks, more := texts(reader.Blocks(100)); !reflect.DeepEqual(blocks, want) || more {
		t.Errorf("full: %q more=%v", blocks, more)
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	line, _ := json.Marshal(omp("new", "rewound", "assistant", text("appended")))
	f.Write(append(line, '\n'))
	f.WriteString(`{"type":"message","id":"half`) // still being written
	f.Close()
	if blocks, more := texts(reader.Blocks(2)); !reflect.DeepEqual(blocks, []string{"after rewind", "appended"}) || !more {
		t.Errorf("after append: %q more=%v", blocks, more)
	}
}

func TestTitles(t *testing.T) {
	ompFile := writeJSONL(t, "o.jsonl", m{"type": "title", "title": "Omp name"}, m{"type": "session"})
	claude := writeJSONL(t, "c.jsonl", m{"type": "ai-title", "aiTitle": "first"}, m{"type": "user"}, m{"type": "ai-title", "aiTitle": "renamed"})
	got := []string{Title(ompFile), Title(claude), Title(filepath.Join(t.TempDir(), "missing"))}
	if want := []string{"Omp name", "renamed", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	// Later calls search only what was appended, including a line that was still being written.
	appendTo := func(s string) {
		f, err := os.OpenFile(claude, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	for _, step := range []struct{ appended, want string }{
		{`{"type":"user"}` + "\n", "renamed"},
		{`{"type":"ai-title","aiTi`, "renamed"},
		{`tle":"third"}` + "\n", "third"},
	} {
		appendTo(step.appended)
		if got := Title(claude); got != step.want {
			t.Errorf("after appending %q: got %q, want %q", step.appended, got, step.want)
		}
	}
}

func TestQuestionsShowWithoutPrefilledAnswers(t *testing.T) {
	form := `<form><p>Sessions live in <code>memory</code>.</p><fieldset><legend>Session store</legend>
<label><input type="radio" name="store" value="sqlite" checked> SQLite</label>
<label><input type="radio" name="store" value="redis"> Redis</label></fieldset>
<label>Lifetime <input type="number" name="ttl" value="30"></label><textarea name="notes">my guess</textarea></form>`
	claude := m{"questions": []any{m{"question": "Fix it too?", "options": []any{m{"label": "Yes", "description": "same pass"}, m{"label": "No"}}}}}
	path := writeJSONL(t, "q.jsonl",
		omp("1", nil, "assistant", m{"type": "toolCall", "name": "ask", "arguments": m{"i": "Choosing", "html": form}}),
		m{"type": "assistant", "uuid": "2", "parentUuid": "1", "message": m{"role": "assistant", "content": []any{m{"type": "tool_use", "name": "AskUserQuestion", "input": claude}}}})
	want := []string{
		"⚙ ask Choosing",
		"**Question**\n\nSessions live in `memory`.\n\n**Session store**\n\n- SQLite\n- Redis\n- Lifetime",
		"⚙ AskUserQuestion",
		"**Question**\n\nFix it too?\n\n- Yes — same pass\n- No",
	}
	if got := all(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
