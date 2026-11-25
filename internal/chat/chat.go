// Package chat is a scrollable view of Markdown blocks rendered at the view's width, plus a live tail.
//
// Glamour renders the Markdown; lines it cannot wrap (code, tables) are hard-wrapped, so nothing is cut.
// Rendered blocks are cached per width, so appending a message or streaming the tail only renders what
// changed. The owner passes only the newest blocks and asks for more when AtTop.
package chat

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type Chat struct {
	Blocks []string
	Live   string
	More   bool // older blocks exist that the owner has not passed yet

	width, height, offset int
	lines                 []string
	cache                 map[string][]string
	renderer              *glamour.TermRenderer
}

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

func (c *Chat) render(markdown string) []string {
	if lines, ok := c.cache[markdown]; ok {
		return lines
	}
	out, err := c.renderer.Render(markdown)
	if err != nil {
		out = markdown
	}
	var lines []string
	for _, line := range strings.Split(strings.Trim(out, "\n"), "\n") {
		lines = append(lines, strings.Split(xansi.Hardwrap(line, c.width, true), "\n")...)
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[0])) == "" {
		lines = lines[1:] // glamour pads blocks with blank lines; the view adds its own separators
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	c.cache[markdown] = lines
	return lines
}

func (c *Chat) SetSize(width, height int) {
	if width != c.width {
		c.width, c.cache, c.renderer = width, map[string][]string{}, newRenderer(width)
	}
	c.height = height
	c.relayout(0)
}

// Update replaces the content; nil blocks keep the current ones.
func (c *Chat) Update(blocks []string, live string, more bool) {
	prepended := 0
	if blocks != nil {
		// Older blocks loaded in front: keep the view on the same text instead of jumping.
		if old := c.Blocks; len(old) > 0 && len(blocks) > len(old) && equal(blocks[len(blocks)-len(old):], old) {
			prepended = len(blocks) - len(old)
		}
		c.Blocks = blocks
	}
	c.Live, c.More = live, more
	c.relayout(prepended)
}

func equal(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}

func (c *Chat) maxOffset() int { return max(0, len(c.lines)-c.height) }

func (c *Chat) relayout(prepended int) {
	if c.width <= 0 {
		return
	}
	follow := c.offset >= c.maxOffset()
	blocks := c.Blocks
	if c.Live != "" {
		blocks = append(blocks[:len(blocks):len(blocks)], c.Live)
	}
	var lines []string
	shift := 0
	for i, block := range blocks {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		if i == prepended {
			shift = len(lines)
		}
		lines = append(lines, c.render(block)...)
	}
	c.lines = lines
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
