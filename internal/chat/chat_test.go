package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ardubev16/tmux-agents/internal/transcript"
	"github.com/charmbracelet/x/ansi"
)

func markdown(texts ...string) []transcript.Block {
	var blocks []transcript.Block
	for _, t := range texts {
		blocks = append(blocks, transcript.Block{Markdown: t})
	}
	return blocks
}

func TestNothingIsCutOrWiderThanTheView(t *testing.T) {
	var c Chat
	c.SetSize(40, 10)
	long := strings.Repeat("x", 300)
	c.Update(markdown(
		"> "+strings.Repeat("word ", 30),
		"```\n"+long+"\n```",
		"| a | b |\n|---|---|\n| "+strings.Repeat("y", 80)+" | z |",
	), nil, false, false)
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
	var texts []string
	for i := range 20 {
		texts = append(texts, fmt.Sprintf("answer %d", i))
	}
	c.Update(markdown(texts[10:]...), nil, false, true)
	c.ScrollBy(-1000)
	if !c.AtTop() {
		t.Fatal("expected to be at the top")
	}
	top := ansi.Strip(c.Lines()[0])
	c.Update(markdown(texts...), nil, false, false)
	if got := ansi.Strip(c.Lines()[c.offset]); got != top {
		t.Errorf("view jumped: top line %q, want %q", got, top)
	}
}

func plain(c *Chat) string {
	var lines []string
	for _, line := range c.Lines() {
		lines = append(lines, strings.TrimRight(ansi.Strip(line), " ")) // glamour pads to the width
	}
	return strings.Join(lines, "\n")
}

func TestStepsAreOneLineUntilClicked(t *testing.T) {
	var c Chat
	c.SetSize(40, 20)
	c.Update([]transcript.Block{
		{Key: "1", Icon: "∴", Summary: "Planning", Detail: "deep thoughts"},
		{Key: "2", Icon: "⚙", Name: "bash", Summary: "ls", Detail: "```\nfile list\n```"},
		{Markdown: "done"},
	}, nil, false, false)
	if got, want := plain(&c), "∴ Planning\n⚙ bash ls\n\ndone"; got != want {
		t.Fatalf("folded:\n%s\nwant:\n%s", got, want)
	}
	if !c.Toggle(1) || !strings.Contains(plain(&c), "⚙ bash ls\n│ ") || !strings.Contains(plain(&c), "file list") ||
		strings.Contains(plain(&c), "deep thoughts") {
		t.Errorf("clicking the tool call should expand only it:\n%s", plain(&c))
	}
	if c.Toggle(len(c.Lines()) - 1) {
		t.Error("prose is not a step")
	}
	c.Toggle(1)
	if got := len(c.Lines()); got != 4 {
		t.Errorf("clicking again folds it: %d lines\n%s", got, plain(&c))
	}
}

func TestTheStepBeingWrittenIsExpandedThenFolds(t *testing.T) {
	var c Chat
	c.SetSize(40, 20)
	tool := transcript.Block{Key: "1", Icon: "⚙", Name: "bash", Summary: "ls", Detail: "```\nfile list\n```"}
	thinking := transcript.ThinkingStep("**Next**\n\nmore thoughts")
	c.Update([]transcript.Block{tool}, nil, true, false)
	if !strings.Contains(plain(&c), "file list") {
		t.Errorf("running tool folded:\n%s", plain(&c))
	}
	c.Update(nil, []transcript.Block{thinking}, true, false)
	if got := plain(&c); strings.Contains(got, "file list") || !strings.Contains(got, "more thoughts") {
		t.Errorf("only the thoughts being written should be expanded:\n%s", got)
	}
	c.Update(nil, []transcript.Block{thinking, {Markdown: "answer"}}, true, false)
	if got := plain(&c); strings.Contains(got, "more thoughts") || !strings.Contains(got, "∴ Next") {
		t.Errorf("thoughts should fold once the answer starts:\n%s", got)
	}
}

func TestPromptsAreBoxedAsTyped(t *testing.T) {
	var c Chat
	c.SetSize(20, 20)
	c.Update([]transcript.Block{{Prompt: true, Markdown: "fix **it** " + strings.Repeat("word ", 6)}}, nil, false, false)
	for _, line := range c.Lines() {
		if w := ansi.StringWidth(line); w != 20 {
			t.Errorf("box line %d wide, want the view's 20: %q", w, ansi.Strip(line))
		}
	}
	if got := plain(&c); !strings.Contains(got, "fix **it**") || strings.Count(got, "word") != 6 {
		t.Errorf("prompt not shown as typed:\n%s", got)
	}
}
