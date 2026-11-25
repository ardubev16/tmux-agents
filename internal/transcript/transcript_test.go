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

func all(t *testing.T, path string) []string {
	t.Helper()
	blocks, _ := New(path).Blocks(1 << 30)
	return blocks
}

func TestOmpShowsOnlyTheActiveBranch(t *testing.T) {
	path := writeJSONL(t, "s.jsonl",
		m{"type": "session", "id": "s"},
		omp("1", nil, "user", text("fix it")),
		omp("2", "1", "assistant", text("abandoned answer")),
		omp("3", "1", "assistant", m{"type": "thinking", "thinking": "hmm"}, text("rewound answer")),
		omp("4", "3", "assistant", m{"type": "toolCall", "name": "bash", "arguments": m{"command": "ls\n-la"}}),
		omp("5", "4", "toolResult", text("file list")),
		omp("6", "5", "assistant", m{"type": "toolCall", "name": "read", "arguments": m{"i": "Reading `x`"}}),
	)
	want := []string{"> fix it", "rewound answer", "- `bash` ls -la\n- `read` Reading 'x'"}
	if got := all(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestClaude(t *testing.T) {
	path := writeJSONL(t, "c.jsonl",
		m{"type": "user", "uuid": "1", "parentUuid": nil, "message": m{"role": "user", "content": "hi\nthere"}},
		m{"type": "user", "uuid": "2", "parentUuid": "1", "isMeta": true, "message": m{"role": "user", "content": "x"}},
		m{"type": "assistant", "uuid": "3", "parentUuid": "2", "message": m{"role": "assistant", "content": []m{
			text("**hello**"), {"type": "tool_use", "name": "Read", "input": m{"file_path": "/a"}},
		}}},
		m{"type": "user", "uuid": "4", "parentUuid": "3", "message": m{"role": "user", "content": []m{{"type": "tool_result", "content": "x"}}}},
		m{"type": "user", "uuid": "5", "parentUuid": "4", "message": m{"role": "user", "content": "<command-name>/clear</command-name>"}},
	)
	want := []string{"> hi\n> there", "**hello**", "- `Read` /a"}
	if got := all(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
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
	blocks, more := reader.Blocks(2)
	if len(blocks) != 2 || !strings.HasPrefix(blocks[0], "answer 2") || blocks[1] != "after rewind" || !more {
		t.Fatalf("tail: %q more=%v", blocks, more)
	}
	if reader.start == 0 {
		t.Error("the head of the file should not have been parsed")
	}
	want := []string{"> root", "answer 1 " + strings.Repeat("x", 50), "answer 2 " + strings.Repeat("x", 100), "after rewind"}
	if blocks, more := reader.Blocks(100); !reflect.DeepEqual(blocks, want) || more {
		t.Errorf("full: %q more=%v", blocks, more)
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	line, _ := json.Marshal(omp("new", "rewound", "assistant", text("appended")))
	f.Write(append(line, '\n'))
	f.WriteString(`{"type":"message","id":"half`) // still being written
	f.Close()
	if blocks, more := reader.Blocks(2); !reflect.DeepEqual(blocks, []string{"after rewind", "appended"}) || !more {
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
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
