// Package chat is a scrollable view of transcript blocks rendered at the view's width, plus a live tail.
//
// Glamour renders the Markdown; lines it cannot wrap (code, tables) are hard-wrapped, so nothing is cut.
// Rendered blocks are cached per width, so appending a message or streaming the tail only renders what
// changed. The owner passes only the newest blocks and asks for more when AtTop. Steps (thinking, tool calls)
// show as one gray line each, expanded to their detail with Toggle or ToggleAll.
package chat

import (
	"slices"
	"strings"

	"github.com/ardubev16/tmux-agents/internal/transcript"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type Chat struct {
	Blocks []transcript.Block
	Live   string
	More   bool // older blocks exist that the owner has not passed yet

	width, height, offset int
	lines                 []string
	owners                []int // the block each line shows, -1 for the blank lines between blocks
	expandAll             bool
	toggled               map[string]bool             // steps expanded or folded against expandAll, by Key
	cache                 map[int]map[string][]string // by width, then Markdown
	renderer              *glamour.TermRenderer
}

var step = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

// ompStyle mirrors omp's colorful Markdown, from the terminal's ANSI palette: H1 magenta, H2 yellow,
// H3-H6 blue, strong/emphasis magenta, inline code green, links cyan, muted rules and quote bars, prose
// in the terminal's default foreground, headings without their `#` markers.
func ompStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	color := func(c string) *string { return &c }
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.Color = nil
	s.Heading.Color = nil
	for i, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix, h.Suffix, h.BackgroundColor = "", "", nil
		h.Color = color([]string{"5", "3", "4", "4", "4", "4"}[i])
	}
	s.Strong.Color, s.Emph.Color = color("5"), color("5")
	s.Code.Prefix, s.Code.Suffix, s.Code.BackgroundColor, s.Code.Color = "", "", nil, color("2")
	s.Link.Color, s.LinkText.Color = color("6"), color("6")
	s.BlockQuote.Color = color("8")
	s.HorizontalRule.Color, s.HorizontalRule.Format = color("8"), "\n────────\n"
	s.Item.Color, s.Enumeration.Color = color("6"), color("6")
	s.CodeBlock.Color = nil
	return s
}

func newRenderer(width int) *glamour.TermRenderer {
	style := ompStyle()
	// ANSI colors at most, so the terminal's colorscheme applies (NO_COLOR still turns them off).
	profile := max(lipgloss.ColorProfile(), termenv.ANSI)
	options := []glamour.TermRendererOption{
		glamour.WithStyles(style),
		glamour.WithColorProfile(profile),
		// Glamour does not count blockquote and list prefixes against the wrap width; leave room for them
		// so prose wraps at word boundaries instead of being hard-wrapped below.
		glamour.WithWordWrap(max(1, width-4)),
	}
	if profile == termenv.ANSI {
		options = append(options, glamour.WithChromaFormatter("terminal16")) // code highlighting too
	}
	r, err := glamour.NewTermRenderer(options...)
	if err != nil {
		panic(err) // only fails on invalid options
	}
	return r
}

// render is renderLines cached per block; the live tail changes on every update, so it is not cached.
func (c *Chat) render(markdown string, width int) []string {
	if lines, ok := c.cache[width][markdown]; ok {
		return lines
	}
	lines := c.renderLines(markdown, width)
	if c.cache[width] == nil {
		c.cache[width] = map[string][]string{}
	}
	c.cache[width][markdown] = lines
	return lines
}

func (c *Chat) renderLines(markdown string, width int) []string {
	out, err := c.renderer.Render(markdown)
	if err != nil {
		out = markdown
	}
	var lines []string
	for _, line := range strings.Split(strings.Trim(out, "\n"), "\n") {
		lines = append(lines, strings.Split(xansi.Hardwrap(line, width, true), "\n")...)
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[0])) == "" {
		lines = lines[1:] // glamour pads blocks with blank lines; the view adds its own separators
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (c *Chat) SetSize(width, height int) {
	if width != c.width {
		c.width, c.cache, c.renderer = width, map[int]map[string][]string{}, newRenderer(width)
	}
	c.height = height
	c.relayout(0)
}

// Update replaces the content; nil blocks keep the current ones.
func (c *Chat) Update(blocks []transcript.Block, live string, more bool) {
	prepended := 0
	if blocks != nil {
		// Older blocks loaded in front: keep the view on the same text instead of jumping.
		if old := c.Blocks; len(old) > 0 && len(blocks) > len(old) && slices.Equal(blocks[len(blocks)-len(old):], old) {
			prepended = len(blocks) - len(old)
		}
		c.Blocks = blocks
	}
	c.Live, c.More = live, more
	c.relayout(prepended)
}

// Toggle expands or folds the step shown on line y of the view, keeping the view in place; false if there is none.
func (c *Chat) Toggle(y int) bool {
	i := c.offset + y
	if y < 0 || i >= len(c.owners) || c.owners[i] < 0 || c.owners[i] >= len(c.Blocks) || c.Blocks[c.owners[i]].Icon == "" {
		return false
	}
	key := c.Blocks[c.owners[i]].Key
	if c.toggled[key] {
		delete(c.toggled, key)
	} else {
		if c.toggled == nil {
			c.toggled = map[string]bool{}
		}
		c.toggled[key] = true
	}
	offset := c.offset
	c.relayout(0)
	c.offset = min(offset, c.maxOffset())
	return true
}

// ToggleAll expands every step, or folds them all back.
func (c *Chat) ToggleAll() {
	c.expandAll, c.toggled = !c.expandAll, nil
	c.relayout(0)
}

func (c *Chat) maxOffset() int { return max(0, len(c.lines)-c.height) }

func (c *Chat) relayout(prepended int) {
	if c.width <= 0 {
		return
	}
	follow := c.offset >= c.maxOffset()
	blocks := c.Blocks
	if c.Live != "" {
		blocks = append(blocks[:len(blocks):len(blocks)], transcript.Block{Markdown: c.Live})
	}
	var lines []string
	var owners []int
	shift := 0
	for i, b := range blocks {
		// Steps in a row stay together, one line each; other blocks get a blank line around them.
		if len(lines) > 0 && (b.Icon == "" || blocks[i-1].Icon == "") {
			lines, owners = append(lines, ""), append(owners, -1)
		}
		if i == prepended {
			shift = len(lines)
		}
		var add []string
		switch {
		case i == len(c.Blocks): // the live tail
			add = c.renderLines(b.Markdown, c.width)
		case b.Icon == "":
			add = c.render(b.Markdown, c.width)
		default:
			expanded := c.expandAll != c.toggled[b.Key]
			arrow := "▸ "
			if expanded {
				arrow = "▾ "
			}
			add = []string{step.Render(xansi.Truncate(arrow+b.Icon+" "+b.Summary, c.width, "…"))}
			if expanded && b.Detail != "" {
				for _, line := range c.render(b.Detail, c.width-2) {
					add = append(add, step.Render("│ ")+line)
				}
			}
		}
		lines = append(lines, add...)
		for range add {
			owners = append(owners, i)
		}
	}
	c.lines, c.owners = lines, owners
	switch {
	case follow:
		c.offset = c.maxOffset()
	case prepended > 0:
		c.offset += shift
	}
	c.offset = min(c.offset, c.maxOffset())
}

func (c *Chat) ScrollBy(delta int) { c.offset = max(0, min(c.offset+delta, c.maxOffset())) }
func (c *Chat) ScrollEnd()         { c.offset = c.maxOffset() }
func (c *Chat) AtTop() bool        { return c.offset == 0 && len(c.lines) > c.height }
func (c *Chat) Lines() []string    { return c.lines }
func (c *Chat) Offset() int        { return c.offset }

func (c *Chat) View() string {
	end := min(c.offset+c.height, len(c.lines))
	visible := c.lines[c.offset:end]
	pad := make([]string, c.height-len(visible))
	return strings.Join(append(append([]string{}, visible...), pad...), "\n")
}
