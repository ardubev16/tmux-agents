// Package chat is a scrollable view of transcript blocks rendered at the view's width, plus a live tail.
//
// Glamour renders the Markdown; lines it cannot wrap (code, tables) are hard-wrapped, so nothing is cut.
// Rendered blocks are cached per width, so appending a message or streaming the tail only renders what
// changed. The owner passes only the newest blocks and asks for more when AtTop. The user's prompts show in a
// box; steps (thinking, tool calls) as one line each, expanded to their detail while the agent is on them or
// with Toggle.
package chat

import (
	"slices"
	"strings"

	"github.com/ardubev16/tmux-agents/internal/theme"
	"github.com/ardubev16/tmux-agents/internal/transcript"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Tool names and the detail's bar in the theme's colors, thoughts in italics.
var (
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Tool))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Muted))
	thoughtStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Thought)).Italic(true)
	barStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Border))
)

type cacheKey struct {
	width  int
	prompt bool
}

type Chat struct {
	Blocks []transcript.Block
	Live   []transcript.Block // what the agent is writing, not in its session file yet
	More   bool               // older blocks exist that the owner has not passed yet

	width, height, offset int
	writing               bool // the agent is still on the last block: a step shows expanded
	lines                 []string
	owners                []int                            // the block each line shows, -1 for the blank lines between blocks
	toggled               map[string]bool                  // steps clicked open or, while being written, shut; by Key
	cache                 map[cacheKey]map[string][]string // by width and kind, then text
	renderer              *glamour.TermRenderer
}

// ompStyle mirrors omp's colorful Markdown in the theme's colors (for the terminal's palette: H1 magenta, H2
// yellow, H3-H6 blue, strong/emphasis magenta, inline code green, links cyan, muted rules and quote bars), prose
// in the terminal's default foreground, headings without their `#` markers.
func ompStyle(t theme.Theme) ansi.StyleConfig {
	s := styles.DarkStyleConfig
	color := func(c string) *string {
		if c == "" {
			return nil // the terminal's default
		}
		return &c
	}
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.Color = nil
	s.Heading.Color = nil
	for i, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix, h.Suffix, h.BackgroundColor = "", "", nil
		h.Color = color([]string{t.H1, t.H2, t.H3, t.H3, t.H3, t.H3}[i])
	}
	s.Strong.Color, s.Emph.Color = color(t.Strong), color(t.Strong)
	s.Code.Prefix, s.Code.Suffix, s.Code.BackgroundColor, s.Code.Color = "", "", nil, color(t.Code)
	s.Link.Color, s.LinkText.Color = color(t.Link), color(t.Link)
	s.BlockQuote.Color = color(t.Quote)
	s.HorizontalRule.Color, s.HorizontalRule.Format = color(t.Rule), "\n────────\n"
	s.Item.Color, s.Enumeration.Color = color(t.Bullet), color(t.Bullet)
	s.CodeBlock.Color = nil
	if t.Syntax != nil {
		s.CodeBlock.Chroma = t.Syntax
		return s
	}
	// Plain code and punctuation in light grays would come out as the palette's white, lighter than the text.
	chroma := *s.CodeBlock.Chroma
	chroma.Text.Color, chroma.Name.Color, chroma.Punctuation.Color, chroma.NameClass.Color = nil, nil, nil, nil
	s.CodeBlock.Chroma = &chroma
	return s
}

func newRenderer(width int) *glamour.TermRenderer {
	// The terminal's theme: ANSI colors at most, so its colorscheme applies (NO_COLOR still turns them off).
	profile := lipgloss.ColorProfile()
	if !theme.Current.Hex {
		profile = max(profile, termenv.ANSI)
	}
	options := []glamour.TermRendererOption{
		glamour.WithStyles(ompStyle(theme.Current)),
		glamour.WithColorProfile(profile),
		// Glamour does not count blockquote and list prefixes against the wrap width; leave room for them
		// so prose wraps at word boundaries instead of being hard-wrapped below.
		glamour.WithWordWrap(max(1, width-4)),
	}
	// Code highlighting at the same depth.
	switch profile {
	case termenv.ANSI:
		options = append(options, glamour.WithChromaFormatter("terminal16"))
	case termenv.TrueColor:
		options = append(options, glamour.WithChromaFormatter("terminal16m"))
	}
	r, err := glamour.NewTermRenderer(options...)
	if err != nil {
		panic(err) // only fails on invalid options
	}
	return r
}

// render is renderLines (or promptLines) cached per text; the live tail changes on every update, so it is not
// cached.
func (c *Chat) render(text string, width int, prompt bool) []string {
	key := cacheKey{width, prompt}
	if lines, ok := c.cache[key][text]; ok {
		return lines
	}
	var lines []string
	if prompt {
		lines = c.promptLines(text)
	} else {
		lines = c.renderLines(text, width)
	}
	if c.cache[key] == nil {
		c.cache[key] = map[string][]string{}
	}
	c.cache[key][text] = lines
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

// promptLines boxes a prompt as typed (no Markdown) on the prompt background, across the whole width.
func (c *Chat) promptLines(text string) []string {
	box := lipgloss.NewStyle().Background(lipgloss.Color(theme.Current.Prompt)).Padding(0, 1).Width(c.width)
	return strings.Split(box.Render(text), "\n")
}

func (c *Chat) stepLine(b transcript.Block) string {
	line := b.Icon + " " + thoughtStyle.Render(b.Summary)
	if b.Name != "" {
		line = toolStyle.Render(b.Icon+" "+b.Name) + " " + mutedStyle.Render(b.Summary)
	}
	return xansi.Truncate(line, c.width, "…")
}

func (c *Chat) SetSize(width, height int) {
	if width != c.width {
		c.width, c.cache, c.renderer = width, map[cacheKey]map[string][]string{}, newRenderer(width)
	}
	c.height = height
	c.relayout(0)
}

// Update replaces the content; nil blocks keep the current ones. writing: the agent is still on the last block.
func (c *Chat) Update(blocks, live []transcript.Block, writing, more bool) {
	prepended := 0
	if blocks != nil {
		// Older blocks loaded in front: keep the view on the same text instead of jumping.
		if old := c.Blocks; len(old) > 0 && len(blocks) > len(old) && slices.Equal(blocks[len(blocks)-len(old):], old) {
			prepended = len(blocks) - len(old)
		}
		c.Blocks = blocks
	}
	c.Live, c.writing, c.More = live, writing, more
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

func (c *Chat) maxOffset() int { return max(0, len(c.lines)-c.height) }

// renderBlock renders a prompt or Markdown, cached unless live.
func (c *Chat) renderBlock(text string, width int, prompt, live bool) []string {
	switch {
	case !live:
		return c.render(text, width, prompt)
	case prompt:
		return c.promptLines(text)
	}
	return c.renderLines(text, width)
}

func (c *Chat) relayout(prepended int) {
	if c.width <= 0 {
		return
	}
	follow := c.offset >= c.maxOffset()
	blocks := c.Blocks
	if len(c.Live) > 0 {
		blocks = append(blocks[:len(blocks):len(blocks)], c.Live...)
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
		live := i >= len(c.Blocks)
		var add []string
		if b.Icon == "" {
			add = c.renderBlock(b.Markdown, c.width, b.Prompt, live)
		} else {
			add = []string{c.stepLine(b)}
			if c.toggled[b.Key] != (c.writing && i == len(blocks)-1) && b.Detail != "" {
				for _, line := range c.renderBlock(b.Detail, c.width-2, false, live) {
					add = append(add, barStyle.Render("│ ")+line)
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
