package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNothingIsCutOrWiderThanTheView(t *testing.T) {
	var c Chat
	c.SetSize(40, 10)
	long := strings.Repeat("x", 300)
	c.Update([]string{
		"> " + strings.Repeat("word ", 30),
		"```\n" + long + "\n```",
		"| a | b |\n|---|---|\n| " + strings.Repeat("y", 80) + " | z |",
	}, "", false)
	text := ""
	for _, line := range c.Lines() {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line wider than the view (%d): %q", w, ansi.Strip(line))
		}
		text += ansi.Strip(line)
	}
	if got := strings.Count(text, "x"); got != 300 {
		t.Errorf("code line lost characters: %d of 300 x", got)
	}
	if got := strings.Count(text, "word"); got != 30 {
		t.Errorf("prose lost words: %d of 30", got)
	}
	if got := strings.Count(text, "y"); got != 80 {
		t.Errorf("table cell cut: %d of 80 y", got)
	}
}

func TestOlderBlocksKeepTheViewInPlace(t *testing.T) {
	var c Chat
	c.SetSize(40, 5)
	var blocks []string
	for i := range 20 {
		blocks = append(blocks, fmt.Sprintf("answer %d", i))
	}
	c.Update(blocks[10:], "", true)
	c.ScrollBy(-1000)
	if !c.AtTop() {
		t.Fatal("expected to be at the top")
	}
	top := ansi.Strip(c.Lines()[0])
	c.Update(blocks, "", false)
	if got := ansi.Strip(c.Lines()[c.offset]); got != top {
		t.Errorf("view jumped: top line %q, want %q", got, top)
	}
}
