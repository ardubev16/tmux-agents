// Package theme holds the dashboard's colors: the terminal's own (its 16-color palette and default colors) or
// one of omp's themes, chosen with @tmux_agents_theme, which main.sh passes as TMUX_AGENTS_THEME.
package theme

import (
	"os"

	"github.com/charmbracelet/glamour/ansi"
)

// Theme colors are lipgloss/glamour color strings: a palette index ("3"), "#rrggbb", or "" for the terminal's
// default.
type Theme struct {
	Name string
	Hex  bool // colors beyond the palette: render at the terminal's full color depth

	Done, Working, Blocked string // the agent list, as the status bar counters
	Border, Selected       string // rules and the highlighted agent's background
	Tool, Thought, Prompt  string // tool names, thoughts, the background of the user's prompts
	Muted                  string // what a tool call is about

	// Markdown: headings (H1, H2, H3-H6), bold and italics, inline code, links, quotes, rules, list bullets.
	H1, H2, H3, Strong, Code, Link, Quote, Rule, Bullet string
	Syntax                                              *ansi.Chroma // nil: glamour's, reduced to the palette
}

// Terminal uses the terminal's palette; Prompt is replaced by a shade of its background when it reports one.
var Terminal = Theme{
	Name: "terminal",
	Done: "2", Working: "3", Blocked: "1",
	Border: "8", Selected: "8",
	Tool: "3", Thought: "", Prompt: "8", Muted: "8",
	H1: "5", H2: "3", H3: "4", Strong: "5", Code: "2", Link: "6", Quote: "8", Rule: "8", Bullet: "6",
}

// GruvboxDark is omp's dark-gruvbox theme.
var GruvboxDark = func() Theme {
	const (
		fg1, fg2, gray, bg1, bg2           = "#ebdbb2", "#d5c4a1", "#928374", "#3c3836", "#504945"
		red, green, yellow, blue, purple   = "#fb4934", "#b8bb26", "#fabd2f", "#83a598", "#d3869b"
		aqua, orange, neutralOrange, bgDim = "#8ec07c", "#fe8019", "#d65d0e", "#1d2021"
	)
	c := func(color string) ansi.StylePrimitive { return ansi.StylePrimitive{Color: &color} }
	return Theme{
		Name: "gruvbox-dark", Hex: true,
		Done: green, Working: yellow, Blocked: red,
		Border: bg2, Selected: bg1,
		Tool: neutralOrange, Thought: gray, Prompt: bgDim, Muted: gray,
		H1: yellow, H2: yellow, H3: yellow, Strong: "", Code: purple, Link: aqua, Quote: gray, Rule: bg2, Bullet: orange,
		Syntax: &ansi.Chroma{
			Text: c(fg1), Comment: c(gray), CommentPreproc: c(aqua),
			Keyword: c(red), KeywordReserved: c(red), KeywordNamespace: c(red), KeywordType: c(aqua),
			Operator: c(orange), Punctuation: c(fg2),
			Name: c(blue), NameBuiltin: c(orange), NameTag: c(aqua), NameAttribute: c(yellow), NameClass: c(aqua),
			NameConstant: c(purple), NameDecorator: c(yellow), NameFunction: c(yellow),
			LiteralNumber: c(purple), LiteralString: c(green), LiteralStringEscape: c(orange),
			GenericDeleted: c(red), GenericInserted: c(green), GenericSubheading: c(gray),
		},
	}
}()

// Current is the theme TMUX_AGENTS_THEME names; the terminal's for an unknown or no name.
var Current = func() Theme {
	if os.Getenv("TMUX_AGENTS_THEME") == GruvboxDark.Name {
		return GruvboxDark
	}
	return Terminal
}()
