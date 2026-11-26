// Agent dashboard: agents on the left, the highlighted agent's chat on the right.
//
// The chat is rendered from the agent's session file, so it reflows to any width and stays visible while
// the agent's pane shows something else (e.g. $EDITOR on a prompt). Agents that serve a socket (omp) also
// stream their in-progress reply and share their prompt draft. Everything touching tmux goes through
// main.sh callbacks, so this program stays tmux-free.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ardubev16/tmux-agents/internal/agentsocket"
	"github.com/ardubev16/tmux-agents/internal/chat"
	"github.com/ardubev16/tmux-agents/internal/term"
	"github.com/ardubev16/tmux-agents/internal/theme"
	"github.com/ardubev16/tmux-agents/internal/transcript"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	chatBlocks = 40 // newest blocks rendered when opening an agent's chat, and added per scroll to the top
	fps        = 12
)

var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// Same glyphs and colors as the status bar counters, in the theme's colors.
var stateStyles = map[string]struct {
	icon  string
	color lipgloss.Color
}{
	"done":    {"○", lipgloss.Color(theme.Current.Done)},
	"working": {"⦿", lipgloss.Color(theme.Current.Working)},
	"blocked": {"●", lipgloss.Color(theme.Current.Blocked)},
}

var (
	dim     = lipgloss.NewStyle().Faint(true)
	bold    = lipgloss.NewStyle().Bold(true)
	border  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Border))
	warning = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Blocked))
	busy    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Current.Working))
)

// Agent list sections, in list_agents' order; empty ones are not shown. No state: a session opened but not prompted.
var groups = map[string]string{"blocked": "Waiting", "done": "Idle", "": "Idle", "working": "Running"}

// Distinct 256-color hues for project names.
var palette = []lipgloss.Color{"39", "208", "170", "43", "148", "141"}

// Each project (a repo and its worktrees) keeps the color it got first, saved in a file so it stays the same in
// every dashboard and across restarts.
var projectColors struct {
	sync.Mutex
	byProject map[string]int // index into palette; nil until read from the file
}

var projectColorsFile = func() string {
	dir, _ := os.UserCacheDir()
	return filepath.Join(dir, "tmux-agents", "project-colors")
}()

// assignColors sets each agent's color: its project's, or for a new project the one fewest listed projects have.
func assignColors(agents []agent) {
	pc := &projectColors
	pc.Lock()
	defer pc.Unlock()
	if pc.byProject == nil {
		pc.byProject = map[string]int{}
		data, _ := os.ReadFile(projectColorsFile) // missing: no project has a color yet
		for _, line := range strings.Split(string(data), "\n") {
			if p, i, ok := strings.Cut(line, "\t"); ok {
				if n, err := strconv.Atoi(i); err == nil && n >= 0 && n < len(palette) {
					pc.byProject[p] = n
				}
			}
		}
	}
	used, counted := make([]int, len(palette)), map[string]bool{}
	for _, a := range agents {
		if c, ok := pc.byProject[a.project]; ok && !counted[a.project] {
			used[c]++
			counted[a.project] = true
		}
	}
	var added strings.Builder
	for i := range agents {
		p := agents[i].project
		c, ok := pc.byProject[p]
		if !ok {
			c = slices.Index(used, slices.Min(used))
			used[c]++
			pc.byProject[p] = c
			fmt.Fprintf(&added, "%s\t%d\n", p, c)
		}
		agents[i].color = palette[c]
	}
	if added.Len() > 0 {
		// Unsaved, a new project just gets a color again next time.
		path := projectColorsFile
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.WriteString(added.String())
				_ = f.Close()
			}
		}
	}
}

var projects sync.Map // folder -> its project, so git runs once per folder

// project is the main checkout of the repo dir is in, the same for all its worktrees (as main.sh's repo_root),
// else dir itself.
func project(dir string) string {
	if p, ok := projects.Load(dir); ok {
		return p.(string)
	}
	p := dir
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		p = filepath.Dir(strings.TrimSpace(string(out)))
	}
	projects.Store(dir, p)
	return p
}

// idleColor goes from the theme's done color through its working color to its blocked one as an agent sits idle.
func idleColor(idle time.Duration) lipgloss.Color {
	switch {
	case idle < 15*time.Minute:
		return lipgloss.Color(theme.Current.Done)
	case idle < time.Hour:
		return lipgloss.Color(theme.Current.Working)
	}
	return lipgloss.Color(theme.Current.Blocked)
}

type agent struct {
	pane, state, target, command, path, transcript, socket, unread, title, project string
	since                                                                          int64          // epoch seconds the state began, 0 = unknown
	color                                                                          lipgloss.Color // its project's
}

// A blocked agent shows a dialog, so a reply answers it instead of touching the draft.
func (a agent) sharesDraft() bool { return a.socket != "" && a.state != "blocked" }

type (
	tickMsg    struct{}
	termMsg    struct{} // lazygit's screen changed or it exited
	agentsMsg  []agent  // list_agents, read in the background
	changesMsg struct{ dir, text string }
	liveMsg    struct {
		gen  int
		live *agentsocket.Live // nil: the subscription ended
	}
)

type model struct {
	mainSh string
	send   func(tea.Msg)
	focus  string // the pane the dashboard was opened over: highlighted on open when it runs an agent

	all      []agent // as list_agents reports them; agents is what the filter lets through
	agents   []agent
	selected int
	listTop  int // first visible row of the agent list
	frame    int
	closing  string // pane waiting for a second x to confirm closing it
	changes  string // the highlighted agent's uncommitted changes, e.g. "3 files +40 −12"
	notice   string // an error to show until the next key

	// The per-second agent list and the git summary run in the background, so keys never wait for them.
	loading    bool   // an agentsMsg is on its way
	changesDir string // folder whose git summary to fetch once the current update is done

	filtering bool
	filter    textinput.Model

	lazygit *term.Term // shown in place of the chat while it runs; gets every key

	chat        chat.Chat
	transcripts map[string]*transcript.Transcript // by path, so polls only parse appended lines
	wanted      int                               // newest blocks the chat shows; grows on scrolling up
	shown       string                            // what the chat shows, to skip identical re-renders

	live    agentsocket.Live // latest snapshot from the highlighted agent's socket
	liveGen int
	stop    chan struct{}

	replying bool
	reply    textarea.Model

	keysOpen         bool // the keybindings panel (?) shows in place of the chat
	keysSel, keysTop int  // its highlighted key and first visible one

	width, height int
}

func run(mainSh string, args ...string) string {
	out, _ := exec.Command(mainSh, args...).Output()
	return string(out)
}

func (m *model) main(args ...string) string { return run(m.mainSh, args...) }

func (m *model) agent() *agent {
	if m.selected < len(m.agents) {
		return &m.agents[m.selected]
	}
	return nil
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/fps, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Init() tea.Cmd {
	m.reload()
	if i := slices.IndexFunc(m.agents, func(a agent) bool { return a.pane == m.focus }); i > 0 {
		m.selected = i
		m.agentChanged()
	}
	return tea.Batch(tick(), m.fetchChanges())
}

// loadAgents reads list_agents and each agent's session name; safe to run in the background.
func loadAgents(mainSh string) []agent {
	var agents []agent
	for _, line := range strings.Split(strings.TrimRight(run(mainSh, "list_agents"), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := append(strings.Split(line, "\t"), make([]string, 9)...)
		a := agent{pane: f[0], state: f[1], target: f[2], command: f[3], path: f[4], transcript: f[5], socket: f[6], unread: f[7]}
		a.since, _ = strconv.ParseInt(f[8], 10, 64)
		a.project = project(a.path)
		if a.transcript != "" {
			a.title = transcript.Title(a.transcript)
		}
		agents = append(agents, a)
	}
	assignColors(agents)
	return agents
}

// reload reads the agent list right away, after something the user did changed it.
func (m *model) reload() {
	m.all = loadAgents(m.mainSh)
	m.applyFilter()
	m.wantChanges()
}

// wantChanges asks for the highlighted agent's git summary, fetched in the background after this update.
func (m *model) wantChanges() {
	if a := m.agent(); a != nil {
		m.changesDir = a.path
	}
}

func (m *model) fetchChanges() tea.Cmd {
	dir := m.changesDir
	m.changesDir = ""
	if dir == "" {
		return nil
	}
	return func() tea.Msg { return changesMsg{dir, changes(dir)} }
}

// applyFilter lists the agents matching the filter, keeping the highlighted one when it still matches.
func (m *model) applyFilter() {
	var prevPane, prevSocket string
	if previous := m.agent(); previous != nil {
		prevPane, prevSocket = previous.pane, previous.socket
	}
	m.agents, m.selected = nil, 0
	for _, a := range m.all {
		if fuzzy(name(a)+" "+a.target+" "+a.command, m.filter.Value()) {
			m.agents = append(m.agents, a)
		}
	}
	for i, a := range m.agents {
		if a.pane == prevPane {
			m.selected = i
		}
	}
	if a := m.agent(); a == nil || a.pane != prevPane || a.socket != prevSocket {
		m.agentChanged()
	}
}

// fuzzy reports whether the query's characters appear in s in order, ignoring case and spaces, like fzf.
func fuzzy(s, query string) bool {
	s = strings.ToLower(s)
	for _, r := range strings.ToLower(query) {
		if r == ' ' {
			continue
		}
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+utf8.RuneLen(r):]
	}
	return true
}

// changes sums up dir's uncommitted changes to tracked files and the commits its upstream lacks, e.g.
// "3 files +40 −12 · ⇡2"; "" when there are neither or dir is not in a git repo.
func changes(dir string) string {
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "LC_ALL=C") // the output is parsed, so not translated
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	var parts []string
	if stat, err := git("diff", "--shortstat", "HEAD"); err == nil && stat != "" {
		var files, added, deleted int
		for _, part := range strings.Split(stat, ", ") {
			var n int
			var what string
			_, _ = fmt.Sscanf(part, "%d %s", &n, &what) // unknown parts leave n at 0
			switch {
			case strings.HasPrefix(what, "file"):
				files = n
			case strings.HasPrefix(what, "insertion"):
				added = n
			case strings.HasPrefix(what, "deletion"):
				deleted = n
			}
		}
		noun := "files"
		if files == 1 {
			noun = "file"
		}
		parts = append(parts, fmt.Sprintf("%d %s +%d −%d", files, noun, added, deleted))
	}
	// A branch without an upstream has nothing to compare with.
	if n, err := git("rev-list", "--count", "@{upstream}..HEAD"); err == nil && n != "0" {
		parts = append(parts, "⇡"+n)
	}
	return strings.Join(parts, " · ")
}

func (m *model) agentChanged() {
	a := m.agent()
	if a == nil || a.pane != m.closing {
		m.closing = ""
	}
	m.changes = ""
	m.wantChanges()
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
	m.live, m.wanted = agentsocket.Live{}, chatBlocks
	m.liveGen++
	if a != nil && a.socket != "" {
		m.stop = make(chan struct{})
		gen, stop, socket, send := m.liveGen, m.stop, a.socket, m.send
		go func() {
			// Errors mean the agent is gone or the socket is stale: the session file still shows the chat.
			_ = agentsocket.Subscribe(socket, stop, func(live agentsocket.Live) { send(liveMsg{gen, &live}) })
			send(liveMsg{gen, nil})
		}()
	}
	m.pollChat(true)
	m.chat.ScrollEnd()
}

func (m *model) pollChat(force bool) {
	a := m.agent()
	key, exists := "", false
	if a != nil {
		key = a.pane
		// omp creates the session file with the first message; until then the chat is just the live reply.
		if info, err := os.Stat(a.transcript); err == nil && a.transcript != "" {
			key, exists = fmt.Sprint(a.pane, info.ModTime().UnixNano(), info.Size(), m.wanted), true
		}
	}
	if key == m.shown && !force {
		return
	}
	m.shown = key
	// Not nil: nil would keep the blocks shown before, i.e. the previously selected agent's chat.
	blocks, more := []transcript.Block{}, false
	if exists {
		t, ok := m.transcripts[a.transcript]
		if !ok {
			t = transcript.New(a.transcript)
			m.transcripts[a.transcript] = t
		}
		blocks, more = t.Blocks(m.wanted)
	}
	m.chat.Update(blocks, m.liveBlocks(), m.writing(), more)
}

// liveBlocks is the in-progress reply (its thoughts as a step, then its text), then prompts the agent has queued
// but not received yet.
func (m *model) liveBlocks() []transcript.Block {
	var out []transcript.Block
	if m.live.Streaming && m.live.Thoughts != "" {
		out = append(out, transcript.ThinkingStep(m.live.Thoughts))
	}
	if m.live.Streaming && m.live.Text != "" {
		out = append(out, transcript.Block{Markdown: m.live.Text})
	}
	for _, prompt := range m.live.Queued {
		out = append(out, transcript.Block{Markdown: prompt + "\n\n(queued)", Prompt: true})
	}
	return out
}

// writing reports whether the agent is still on the chat's last step (writing its thoughts, running its tool),
// which then shows expanded. omp says so on its socket; for Claude Code, whatever it does while working.
func (m *model) writing() bool {
	a := m.agent()
	return a != nil && a.state == "working" && (a.socket == "" || m.live.Streaming || m.live.Tool != "")
}

func (m *model) scrollChat(delta int) {
	m.chat.ScrollBy(delta)
	if m.chat.AtTop() && m.chat.More {
		m.wanted += chatBlocks
		m.pollChat(false)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.update(msg), m.fetchChanges())
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case tickMsg:
		m.frame++
		if m.frame%3 == 0 {
			m.pollChat(false)
		}
		// Not while replying or in lazygit: a reload could move the selection to another agent, which the reply
		// would then go to.
		if m.frame%fps == 0 && !m.replying && m.lazygit == nil && !m.loading {
			m.loading = true
			mainSh := m.mainSh
			return tea.Batch(tick(), func() tea.Msg { return agentsMsg(loadAgents(mainSh)) })
		}
		return tick()
	case agentsMsg:
		m.loading = false
		if !m.replying && m.lazygit == nil { // either may have started while the list was loading
			m.all = msg
			m.applyFilter()
			m.wantChanges()
		}
	case changesMsg:
		if a := m.agent(); a != nil && a.path == msg.dir {
			m.changes = msg.text
		}
	case liveMsg:
		if msg.gen != m.liveGen {
			return nil // from an agent no longer highlighted
		}
		if msg.live == nil {
			m.live = agentsocket.Live{}
			return nil
		}
		finished := m.live.Streaming && !msg.live.Streaming
		m.live = *msg.live
		if finished {
			m.pollChat(true) // the finished reply is in the session file now
		} else {
			m.chat.Update(nil, m.liveBlocks(), m.writing(), m.chat.More)
		}
	case termMsg:
		if m.lazygit != nil && m.lazygit.Exited() {
			m.lazygit.Close()
			m.lazygit = nil
			m.reload()
		}
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.KeyMsg:
		m.notice = ""
		switch {
		case m.lazygit != nil:
			m.lazygit.Key(msg)
			return nil
		case m.replying:
			return m.replyKey(msg)
		case m.filtering:
			return m.filterKey(msg)
		}
		return m.key(msg)
	}
	return nil
}

func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.lazygit != nil {
		// lazygit sits right of the list, separator and a space, below the header and its rule.
		x, y := msg.X-m.listWidth()-2, msg.Y-2
		if x >= 0 && y >= 0 && x < m.rightWidth() && y < m.chatHeight() {
			m.lazygit.Mouse(msg, x, y)
		}
		return nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollChat(-3)
	case tea.MouseButtonWheelDown:
		m.scrollChat(3)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress || m.replying {
			return nil
		}
		if msg.X < m.listWidth() {
			if _, owners := m.listRows(); m.listTop+msg.Y < len(owners) && owners[m.listTop+msg.Y] >= 0 {
				m.selected = owners[m.listTop+msg.Y]
				m.agentChanged()
			}
		} else if msg.X >= m.listWidth()+2 {
			m.chat.Toggle(msg.Y - 2) // the chat sits below the header and its rule
		}
	}
	return nil
}

type keybinding struct{ key, short, detail string }

// keybindings is what ? lists, with a tooltip for the keys that need more than their short description; the bar at
// the bottom shows only the keys a vim user would not guess.
var keybindings = []keybinding{
	{"↑/k ↓/j", "previous/next agent", ""},
	{"1-9", "agent by number", "Numbers follow the visible list: with a filter, they count the matching agents."},
	{"n", "next needing you", "Highlights the next agent waiting for your input or finished while you were away, wrapping around. Each section lists the one waiting longest first."},
	{"click", "highlight/expand", "In the list, highlights the agent. In the chat, expands or folds a thought or tool call to show all of it."},
	{"enter", "switch to agent", ""},
	{"ctrl+r", "reply", "Opens a reply box below the chat: enter sends it, ctrl+j adds a newline. For omp agents it starts from your half-typed prompt, and esc keeps what you wrote as that draft."},
	{"/", "filter", "Fuzzy-filters the agents by name, pane or command. enter keeps the filter, esc clears it."},
	{"tab", "lazygit", "Runs lazygit in the agent's folder in place of the chat. Keys and mouse go to it until you quit it."},
	{"pgup/pgdn", "scroll chat", "The mouse wheel scrolls too. Scrolling to the top loads older messages."},
	{"x x", "close agent", "Stops the agent like a normal exit (SIGTERM, so it saves its session) and closes its pane. The first x asks to confirm."},
	{"?", "keybindings", ""},
	{"q/esc", "close", ""},
}

// keysView is the ? panel: the keys with a short description, scrolled to keep the highlighted one in view, and
// the highlighted key's tooltip below them. Its size stays the same whichever key is highlighted.
func (m *model) keysView() string {
	keyWidth, inner := 0, 0
	for _, k := range keybindings {
		keyWidth = max(keyWidth, ansi.StringWidth(k.key))
	}
	for _, k := range keybindings {
		inner = max(inner, keyWidth+2+ansi.StringWidth(k.short))
	}
	inner = max(1, min(m.rightWidth()-4, max(inner, 50))) // inside the border and padding
	tooltip := lipgloss.NewStyle().Width(inner)
	tooltipHeight := 0
	for _, k := range keybindings {
		tooltipHeight = max(tooltipHeight, lipgloss.Height(tooltip.Render(k.detail)))
	}
	// Border, title and separator take 4 rows; the tooltip as many as the longest one.
	visible := max(1, min(len(keybindings), m.chatHeight()-4-tooltipHeight))
	m.keysTop = max(min(m.keysTop, m.keysSel), m.keysSel-visible+1)
	lines := []string{bold.Render("Keybindings") + dim.Render(fmt.Sprintf("  %d/%d", m.keysSel+1, len(keybindings)))}
	for i := m.keysTop; i < min(len(keybindings), m.keysTop+visible); i++ {
		k, style := keybindings[i], lipgloss.NewStyle()
		if i == m.keysSel {
			style = style.Background(lipgloss.Color(theme.Current.Selected))
		}
		line := style.Foreground(lipgloss.Color(theme.Current.Working)).Render(strings.Repeat(" ", keyWidth-ansi.StringWidth(k.key))+k.key) +
			style.Render("  "+k.short)
		lines = append(lines, ansi.Truncate(line, inner, "…")+style.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))))
	}
	if d := keybindings[m.keysSel].detail; d != "" {
		lines = append(lines, border.Render(strings.Repeat("─", inner)), tooltip.Render(d))
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.Current.Border)).
		Padding(0, 1).Render(strings.Join(lines, "\n"))
	// Placed as if every key had a tooltip, so the box stays put when one shows or hides.
	top := max(0, (m.chatHeight()-(visible+4+tooltipHeight))/2)
	return lipgloss.Place(m.rightWidth(), m.chatHeight(), lipgloss.Center, lipgloss.Top, lipgloss.NewStyle().MarginTop(top).Render(box))
}

func (m *model) key(msg tea.KeyMsg) tea.Cmd {
	if m.keysOpen {
		switch msg.String() {
		case "up", "k":
			m.keysSel = max(0, m.keysSel-1)
		case "down", "j":
			m.keysSel = min(len(keybindings)-1, m.keysSel+1)
		case "?", "esc", "q":
			m.keysOpen = false
		}
		return nil
	}
	switch msg.String() {
	case "?":
		m.keysOpen = true
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.agentChanged()
		}
	case "down", "j":
		if m.selected < len(m.agents)-1 {
			m.selected++
			m.agentChanged()
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(msg.String()[0] - '1'); i < len(m.agents) {
			m.selected = i
			m.agentChanged()
		}
	case "pgup":
		m.scrollChat(-max(1, m.chatHeight()/2))
	case "pgdown":
		m.scrollChat(max(1, m.chatHeight()/2))
	case "n":
		for step := 1; step <= len(m.agents); step++ {
			i := (m.selected + step) % len(m.agents)
			if a := m.agents[i]; a.state == "blocked" || (a.state == "done" && a.unread != "") {
				m.selected = i
				m.agentChanged()
				return nil
			}
		}
		m.notice = "no agent needs you"
	case "enter":
		if a := m.agent(); a != nil {
			m.main("switch_agent", a.pane)
			return tea.Quit
		}
	case "ctrl+r":
		if a := m.agent(); a != nil {
			draft := ""
			if a.sharesDraft() {
				var reply struct{ Draft string }
				if agentsocket.Request(a.socket, map[string]string{"op": "get_draft"}, &reply) == nil {
					draft = reply.Draft
				}
			}
			m.replying = true
			m.reply.SetValue(draft)
			m.reply.CursorEnd()
			m.layout()
			return m.reply.Focus()
		}
	case "x":
		if a := m.agent(); a != nil {
			if m.closing != a.pane {
				m.closing = a.pane // the status line asks for confirmation
				return nil
			}
			m.closing = ""
			// Detached: stopping the agent takes up to 5 s and must finish even if the popup closes meanwhile.
			cmd := exec.Command(m.mainSh, "close_agent", a.pane)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
			}
		}
	case "/":
		m.filtering = true
		return m.filter.Focus()
	case "tab":
		if a := m.agent(); a != nil {
			cmd := exec.Command("lazygit")
			cmd.Dir = a.path
			send := m.send
			t, err := term.Start(cmd, m.rightWidth(), m.chatHeight(), func() { send(termMsg{}) })
			if err != nil {
				m.notice = "lazygit: " + err.Error()
				return nil
			}
			m.lazygit = t
		}
	case "esc":
		switch {
		case m.closing != "":
			m.closing = ""
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.applyFilter()
		default:
			return tea.Quit
		}
		return nil
	case "q", "ctrl+c":
		return tea.Quit
	}
	return nil
}

// filterKey edits the filter while it has focus: enter keeps it, esc clears it, up/down still move.
func (m *model) filterKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.applyFilter()
		return nil
	case "up", "down":
		return m.key(msg)
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter()
	return cmd
}

func (m *model) setDraft(a *agent, text string) bool {
	var reply struct{ OK bool }
	return agentsocket.Request(a.socket, map[string]string{"op": "set_draft", "text": text}, &reply) == nil && reply.OK
}

func (m *model) replyKey(msg tea.KeyMsg) tea.Cmd {
	a := m.agent()
	switch msg.String() {
	case "enter":
		if a != nil {
			if a.sharesDraft() {
				if !m.setDraft(a, m.reply.Value()) {
					m.notice = "could not reach the agent's prompt: nothing was sent"
					return nil // keep the reply, so it is not lost
				}
				m.main("reply_agent", a.pane, "") // Enter submits the draft we just wrote
			} else {
				m.main("reply_agent", a.pane, m.reply.Value())
			}
		}
		m.closeReply()
		m.reload()
		return nil
	case "ctrl+j":
		m.reply.InsertString("\n")
		return nil
	case "esc":
		if a != nil && a.sharesDraft() && !m.setDraft(a, m.reply.Value()) {
			m.notice = "could not reach the agent's prompt: your edits were not kept"
		}
		m.closeReply()
		return nil
	}
	var cmd tea.Cmd
	m.reply, cmd = m.reply.Update(msg)
	return cmd
}

func (m *model) closeReply() {
	m.replying = false
	m.reply.Reset()
	m.reply.Blur()
	m.layout()
}

func (m *model) listWidth() int  { return m.width * 35 / 100 }
func (m *model) rightWidth() int { return max(3, m.width-m.listWidth()-2) } // separator and a space
func (m *model) chatWidth() int  { return m.rightWidth() - 2 }              // a space and the scrollbar

func (m *model) chatHeight() int {
	h := m.height - 5 // header and its rule, a blank line, status and help lines
	if m.replying {
		h -= m.reply.Height() + 1
	}
	return max(1, h)
}

func (m *model) layout() {
	m.reply.SetWidth(m.rightWidth())
	m.chat.SetSize(m.chatWidth(), m.chatHeight())
	if m.lazygit != nil {
		m.lazygit.Resize(m.rightWidth(), m.chatHeight())
	}
}

// name is the session name the harness gave the agent, else its pane; main.sh's agent_name follows the same rule.
func name(a agent) string {
	if a.title != "" {
		return a.title
	}
	return a.target
}

func details(a agent) string { return a.target + " · " + a.command }

// ago is how long ago since (epoch seconds) was, in its largest unit: 45s, 12m, 3h, 2d.
func ago(since int64, now time.Time) string {
	d := max(0, now.Sub(time.Unix(since, 0)))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", d/time.Second)
	case d < time.Hour:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dd", d/(24*time.Hour))
}

// listRows renders the agent list: a header per non-empty section, two lines per agent, the selected one
// highlighted. owners maps each row to its agent, -1 for headers.
func (m *model) listRows() (rows []string, owners []int) {
	width, group := m.listWidth(), ""
	if m.filtering || m.filter.Value() != "" {
		rows, owners = append(rows, ansi.Truncate(" "+m.filter.View(), width, "…")), append(owners, -1)
	}
	now := time.Now()
	for i, a := range m.agents {
		if g := groups[a.state]; g != group {
			if group != "" { // a blank row between sections
				rows, owners = append(rows, ""), append(owners, -1)
			}
			group = g
			rows, owners = append(rows, bold.Render(ansi.Truncate(" "+g, width, "…"))), append(owners, -1)
		}
		base := lipgloss.NewStyle()
		if i == m.selected {
			base = base.Background(lipgloss.Color(theme.Current.Selected))
		}
		style, ok := stateStyles[a.state]
		if !ok {
			style.icon = " "
		}
		icon, note := style.icon, ""
		switch {
		case a.state == "working":
			icon = string(spinner[m.frame%len(spinner)])
		case a.state == "blocked":
			note = " needs input"
		case a.unread != "":
			icon, note = "●", " finished"
		}
		color := base.Foreground(style.color)
		// How long the agent has been running, or (colored by how long) waiting for you or idle, at the right edge.
		since := ""
		switch {
		case a.since == 0:
		case a.state == "working":
			since = base.Faint(true).Render(ago(a.since, now) + " ")
		default:
			since = base.Foreground(idleColor(now.Sub(time.Unix(a.since, 0)))).Render(ago(a.since, now) + " ")
		}
		number := " " // keys 1-9 highlight the first nine
		if i < 9 {
			number = fmt.Sprint(i + 1)
		}
		first := base.Faint(true).Render(number) + base.Render(" ") + color.Render(icon) + base.Render(" ") + base.Bold(true).Render(name(a)) + color.Render(note)
		first = ansi.Truncate(first, max(0, width-ansi.StringWidth(since)), base.Render("…"))
		first += base.Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(first)-ansi.StringWidth(since)))) + since
		// The project name in its color picks out the agents working on the same one.
		second := base.Render("    ") + base.Foreground(a.color).Render(filepath.Base(a.project)) + base.Faint(true).Render(" · "+details(a))
		lines := []string{first, second}
		for _, line := range lines {
			line = ansi.Truncate(line, width, base.Render("…"))
			rows = append(rows, line+base.Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))))
			owners = append(owners, i)
		}
	}
	return rows, owners
}

func (m *model) scrollbar() string {
	height, lines := m.chatHeight(), len(m.chat.Lines())
	bar := make([]string, height)
	thumb, top := height, 0
	if lines > height {
		thumb = max(1, height*height/lines)
		top = (height - thumb) * m.chat.Offset() / max(1, lines-height)
	}
	for i := range bar {
		if lines > height && i >= top && i < top+thumb {
			bar[i] = border.Render("┃")
		} else {
			bar[i] = border.Faint(true).Render("│")
		}
	}
	return strings.Join(bar, "\n")
}

func (m *model) header() string {
	a := m.agent()
	if a == nil {
		return ""
	}
	// The git summary at the right edge, a column short of it; the name and details give way to it.
	changes := ""
	if m.changes != "" {
		changes = "  " + busy.Render(m.changes)
	}
	width := m.rightWidth() - 1 - ansi.StringWidth(changes)
	head := ansi.Truncate(bold.Render(name(*a))+"  "+dim.Render(details(*a)), width, "…")
	return head + strings.Repeat(" ", max(0, width-ansi.StringWidth(head))) + changes
}

func (m *model) status() string {
	a := m.agent()
	switch {
	case m.notice != "":
		return warning.Render(m.notice)
	case a == nil:
		return ""
	case m.closing == a.pane:
		return warning.Render("press x again to stop this agent and close its pane, esc to cancel")
	case a.transcript == "":
		return dim.Render("no session reported: restart the agent after installing the hooks")
	case a.state == "blocked":
		return warning.Render("● waiting for you")
	case a.state != "working":
		return ""
	}
	doing := "working…"
	switch {
	case m.live.Tool != "":
		doing = "running " + m.live.Tool + "…"
	case m.live.Thinking:
		doing = "thinking…"
	case m.live.Streaming:
		doing = "writing…"
	}
	return busy.Render(string(spinner[m.frame%len(spinner)]) + " " + doing)
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	body := max(1, m.height-1)
	rows, owners := m.listRows()
	// Keep the selected agent (and its section header) in view when the list is taller than the popup.
	for i, owner := range owners {
		if owner != m.selected {
			continue
		}
		first := i
		if i > 0 && owners[i-1] < 0 {
			first = i - 1
		}
		m.listTop = min(m.listTop, first)
		m.listTop = max(m.listTop, i+2-body)
		break
	}
	m.listTop = max(0, min(m.listTop, len(rows)-body))
	rows = rows[min(m.listTop, len(rows)):]
	list := lipgloss.NewStyle().Width(m.listWidth()).Height(body).MaxHeight(body).Render(strings.Join(rows, "\n"))
	chatView := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.chatWidth()).Render(m.chat.View()), " ", m.scrollbar())
	switch {
	case m.lazygit != nil:
		chatView = lipgloss.NewStyle().Width(m.rightWidth()).MaxWidth(m.rightWidth()).
			Height(m.chatHeight()).MaxHeight(m.chatHeight()).Render(m.lazygit.View())
	case m.keysOpen:
		chatView = m.keysView()
	}
	right := []string{m.header(), border.Render(strings.Repeat("─", m.rightWidth())), chatView, "", m.status()}
	if m.replying {
		right = append(right, border.Render(strings.Repeat("─", m.rightWidth())), m.reply.View())
	}
	sep := border.Render(strings.Repeat("│\n", body-1) + "│")
	main := lipgloss.JoinHorizontal(lipgloss.Top, list, sep, " ", strings.Join(right, "\n"))
	help := "⏎ switch · n next needing you · ^r reply · tab lazygit · x close agent · ? keybindings"
	switch {
	case m.lazygit != nil:
		help = "keys go to lazygit · quit it (q) to return to the chat"
	case m.replying:
		help = "⏎ send · ^j newline · esc keep as draft"
	case m.filtering:
		help = "type to fuzzy filter by name, pane or command · ⏎ keep · esc clear"
	case m.keysOpen:
		help = "j/k move · esc close"
	}
	return main + "\n" + dim.Render(ansi.Truncate(help, m.width, "…"))
}

func newDashboard(mainSh string) *model {
	reply := textarea.New()
	reply.ShowLineNumbers = false
	reply.Prompt = ""
	reply.SetHeight(4)
	reply.Placeholder = "reply (empty = just Enter)"
	// Ctrl+arrows jump by word too, like in shells and editors (the defaults only bind Alt).
	reply.KeyMap.WordBackward.SetKeys("alt+left", "alt+b", "ctrl+left")
	reply.KeyMap.WordForward.SetKeys("alt+right", "alt+f", "ctrl+right")
	filter := textinput.New()
	filter.Prompt = "/ "
	return &model{mainSh: mainSh, transcripts: map[string]*transcript.Transcript{}, wanted: chatBlocks, reply: reply, filter: filter}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: tmux-agents <path to main.sh>")
		os.Exit(1)
	}
	if theme.Current.Name == theme.Terminal.Name {
		if bg, ok := promptBackground(queryColors()); ok {
			theme.Current.Prompt = bg
		}
	}
	m := newDashboard(os.Args[1])
	m.focus = os.Getenv("TMUX_AGENTS_FOCUS")
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.send = p.Send
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
