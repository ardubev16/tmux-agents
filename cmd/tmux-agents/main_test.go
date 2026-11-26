package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeAgent is the omp integration's socket: it serves a draft and pushes live snapshots.
type fakeAgent struct {
	draft string
	subs  chan net.Conn
}

func serveAgent(t *testing.T, draft string) (*fakeAgent, string) {
	dir, _ := os.MkdirTemp("/tmp", "ta") // unix socket paths are capped at ~100 bytes
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	agent := &fakeAgent{draft: draft, subs: make(chan net.Conn, 1)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req struct{ Op, Text string }
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			json.Unmarshal(line, &req)
			switch req.Op {
			case "get_draft":
				json.NewEncoder(conn).Encode(map[string]string{"draft": agent.draft})
			case "set_draft":
				agent.draft = req.Text
				conn.Write([]byte(`{"ok":true}` + "\n"))
			case "subscribe":
				agent.subs <- conn
				continue
			}
			conn.Close()
		}
	}()
	return agent, path
}

func (a *fakeAgent) push(t *testing.T, live map[string]any) {
	conn := <-a.subs
	defer func() { a.subs <- conn }()
	if err := json.NewEncoder(conn).Encode(live); err != nil {
		t.Fatal(err)
	}
}

// fakeMain stands in for main.sh: a blocked agent without a session, a working titled one with a long
// transcript, and a finished one nobody has looked at yet. Calls are logged.
func fakeMain(t *testing.T, socket string) (string, func() []string) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString(`{"type":"title","title":"Fix the bug"}` + "\n")
	for i := range 100 {
		parent := any(nil)
		if i > 0 {
			parent = fmt.Sprint(i - 1)
		}
		line, _ := json.Marshal(map[string]any{"type": "message", "id": fmt.Sprint(i), "parentId": parent,
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("answer %d", i)}}}})
		b.Write(append(line, '\n'))
	}
	session := filepath.Join(dir, "t.jsonl")
	os.WriteFile(session, []byte(b.String()), 0o644)
	log := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "main.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/usr/bin/env bash
printf '%%s\n' "$*" >>'%[1]s'
case "$1" in
list_agents)
    printf '%%%%1\tblocked\ts:1.0\tclaude\t~/a\t\t\t\t%%s\n' "$(( $(date +%%s) - 180 ))"
    printf '%%%%2\tworking\ts:2.0\tomp\t%[4]s\t%[2]s\t%[3]s\t\t%%s\n' "$(( $(date +%%s) - 180 ))"
    grep -q '^close_agent %%3' '%[1]s' || printf '%%%%3\tdone\ts:3.0\tomp\t~/c\t\t\t1\t%%s\n' "$(( $(date +%%s) - 7200 ))" ;;
esac
`, log, session, socket, dir)), 0o755)
	return script, func() []string {
		out, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}
}

func newModel(t *testing.T, socket string) (*model, chan tea.Msg, func() []string) {
	script, calls := fakeMain(t, socket)
	msgs := make(chan tea.Msg, 100)
	m := newDashboard(script)
	m.send = func(msg tea.Msg) { msgs <- msg }
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, msgs, calls
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "pgup":
			msg = tea.KeyMsg{Type: tea.KeyPgUp}
		case "ctrl+r":
			msg = tea.KeyMsg{Type: tea.KeyCtrlR}
		case "ctrl+j":
			msg = tea.KeyMsg{Type: tea.KeyCtrlJ}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestListIsGroupedAndShowsNamesAndWhatNeedsYou(t *testing.T) {
	m, _, _ := newModel(t, "")
	rows, owners := m.listRows()
	var lines []string
	for i, row := range rows {
		if i == 0 || owners[i] != owners[i-1] { // section headers and each agent's first line
			lines = append(lines, strings.TrimSpace(ansi.Strip(row)))
		}
	}
	want := []string{"Waiting", "● s:1.0 needs input", "Running", "Fix the bug", "Idle", "● s:3.0 finished"}
	for i := range want {
		if i >= len(lines) || !strings.Contains(lines[i], want[i]) {
			t.Fatalf("rows %q, want %q", lines, want)
		}
	}
	if !strings.HasSuffix(lines[3], " 3m") || !strings.HasSuffix(lines[5], " 2h") || strings.HasSuffix(lines[1], "m") {
		t.Errorf("running and idle agents show how long, blocked ones do not: %q", lines)
	}
	m.agents = m.agents[:1] // no running or idle agents: no empty sections
	if rows, _ := m.listRows(); len(rows) != 3 {
		t.Errorf("rows %q", rows)
	}
}

func TestChatShowsTheNewestAndLoadsOlderOnScroll(t *testing.T) {
	m, _, _ := newModel(t, "")
	press(m, "down")
	if got := len(m.chat.Blocks); got != chatBlocks || !m.chat.More {
		t.Fatalf("blocks=%d more=%v", got, m.chat.More)
	}
	for range 40 {
		press(m, "pgup")
	}
	if got := len(m.chat.Blocks); got <= chatBlocks {
		t.Errorf("scrolling to the top did not load older blocks: %d", got)
	}
}

func TestNewSessionShowsAnEmptyChatNotThePreviousOne(t *testing.T) {
	m, _, _ := newModel(t, "")
	press(m, "down") // the agent with a long transcript
	// Reported, but omp writes it with the first message.
	m.agents[2].transcript = filepath.Join(t.TempDir(), "new.jsonl")
	press(m, "down")
	if len(m.chat.Blocks) != 0 {
		t.Errorf("chat shows %d blocks of another session", len(m.chat.Blocks))
	}
}

func TestReplyContinuesTheAgentsDraft(t *testing.T) {
	agent, socket := serveAgent(t, "half typed")
	m, _, calls := newModel(t, socket)
	press(m, "down", "ctrl+r")
	if got := m.reply.Value(); got != "half typed" {
		t.Fatalf("reply starts with %q", got)
	}
	press(m, " ", "o", "k", "ctrl+j", "x", "esc")
	if agent.draft != "half typed ok\nx" {
		t.Errorf("cancelling should keep the edits in the agent: %q", agent.draft)
	}
	press(m, "ctrl+r", "!", "enter")
	if agent.draft != "half typed ok\nx!" || !contains(calls(), "reply_agent %2 ") {
		t.Errorf("draft %q, calls %q", agent.draft, calls())
	}
}

func TestReplyToAnUnreachableAgentIsKeptNotTypedIntoThePane(t *testing.T) {
	m, _, calls := newModel(t, filepath.Join(t.TempDir(), "gone.sock"))
	press(m, "down", "ctrl+r", "h", "i", "enter")
	if contains(calls(), "reply_agent %2 hi") || !m.replying || m.reply.Value() != "hi" || m.notice == "" {
		t.Errorf("replying=%v value=%q notice=%q calls %q", m.replying, m.reply.Value(), m.notice, calls())
	}
}

func TestPeriodicRefreshRunsOutsideUpdate(t *testing.T) {
	m, _, calls := newModel(t, "")
	before := len(calls())
	var cmd tea.Cmd
	for range fps {
		_, cmd = m.Update(tickMsg{})
	}
	if len(calls()) != before || !m.loading {
		t.Fatalf("list_agents ran inside Update: %q", calls()[before:])
	}
	// Run what Update handed back, as Bubble Tea would, and deliver the agent list.
	var pending []tea.Cmd
	for pending = append(pending, cmd); len(pending) > 0; pending = pending[1:] {
		if pending[0] == nil {
			continue
		}
		switch msg := pending[0]().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case agentsMsg:
			m.Update(msg)
		}
	}
	if m.loading || len(calls()) != before+1 || len(m.agents) != 3 {
		t.Errorf("loading=%v agents=%d calls %q", m.loading, len(m.agents), calls()[before:])
	}
}

func TestBlockedAgentReplyTypesIntoThePane(t *testing.T) {
	m, _, calls := newModel(t, "")
	press(m, "ctrl+r", "y", "e", "s", "enter")
	if !contains(calls(), "reply_agent %1 yes") {
		t.Errorf("calls %q", calls())
	}
}

func TestFilterNarrowsTheList(t *testing.T) {
	m, _, _ := newModel(t, "")
	press(m, "/", "o", "m", "p")
	if len(m.agents) != 2 || m.agents[0].command != "omp" {
		t.Fatalf("filtered to %+v", m.agents)
	}
	press(m, "enter", "esc") // keep it, then clear it outside the input
	if len(m.agents) != 3 || m.filtering {
		t.Errorf("after esc: %d agents, filtering=%v", len(m.agents), m.filtering)
	}
}

func TestChangesSummarizesTheDiff(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, "a"), []byte("1\n2\n"), 0o644)
	git("add", "a")
	git("commit", "-qm", "init")
	if got := changes(dir); got != "" {
		t.Errorf("clean tree: %q", got)
	}
	os.WriteFile(filepath.Join(dir, "a"), []byte("1\nchanged\nadded\n"), 0o644)
	if got := changes(dir); got != "1 file +2 −1" {
		t.Errorf("got %q", got)
	}
	git("branch", "base")
	git("branch", "--set-upstream-to=base")
	git("commit", "-qam", "ahead")
	if got := changes(dir); got != "⇡1" {
		t.Errorf("got %q", got)
	}
}

func TestReplyGoesToTheAgentItWasOpenedFor(t *testing.T) {
	m, _, calls := newModel(t, "")
	press(m, "down", "down", "ctrl+r", "h", "i")
	m.main("close_agent", "%3") // the agent exits while the reply is being typed
	for range fps {
		m.Update(tickMsg{})
	}
	m.Update(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	press(m, "enter")
	if got := calls(); !contains(got, "reply_agent %3 hi") || contains(got, "reply_agent %1 hi") {
		t.Errorf("calls %q", got)
	}
}

func TestTabRunsLazygitInPlaceOfTheChat(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "lazygit"), []byte("#!/bin/sh\necho stub-lazygit\nread answer\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	m, msgs, _ := newModel(t, "")
	waitFor := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case msg := <-msgs:
				m.Update(msg)
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s; view:\n%s", what, ansi.Strip(m.View()))
			}
		}
	}
	press(m, "down", "tab")
	waitFor("lazygit", func() bool { return strings.Contains(m.View(), "stub-lazygit") })
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Waiting") {
		t.Errorf("the agent list should stay next to lazygit:\n%s", view)
	}
	press(m, "enter") // goes to lazygit, which exits
	waitFor("the chat back", func() bool { return m.lazygit == nil })
}

func TestLiveReplyAndQueuedPromptsStream(t *testing.T) {
	agent, socket := serveAgent(t, "")
	m, msgs, _ := newModel(t, socket)
	press(m, "down")
	agent.push(t, map[string]any{"text": "streaming **now**", "streaming": true, "queued": []string{"also do this"}})
	select {
	case msg := <-msgs:
		m.Update(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("no live update")
	}
	var lines []string
	for _, l := range m.chat.Lines() {
		if s := strings.TrimSpace(ansi.Strip(l)); s != "" {
			lines = append(lines, s)
		}
	}
	tail := strings.Join(lines[len(lines)-5:], "\n") // reply, then the quoted prompt with its marker
	if !strings.Contains(tail, "streaming now") || !strings.Contains(tail, "also do this") || !strings.Contains(tail, "queued") {
		t.Errorf("tail:\n%s", tail)
	}
	if !strings.Contains(ansi.Strip(m.status()), "writing") {
		t.Errorf("status %q", ansi.Strip(m.status()))
	}
}

func TestEnterSwitchesAndCloses(t *testing.T) {
	m, _, calls := newModel(t, "")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != tea.Quit() || !contains(calls(), "switch_agent %1") {
		t.Errorf("calls %q", calls())
	}
}

func TestQClosesButTypesInAReply(t *testing.T) {
	m, _, calls := newModel(t, "")
	press(m, "ctrl+r", "q", "enter")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !contains(calls(), "reply_agent %1 q") || cmd == nil || cmd() != tea.Quit() {
		t.Errorf("calls %q", calls())
	}
}

func TestCloseNeedsConfirmation(t *testing.T) {
	m, _, calls := newModel(t, "")
	press(m, "down", "down", "x", "esc", "x")
	if contains(calls(), "close_agent %3") {
		t.Fatal("closed without confirmation")
	}
	press(m, "x")
	time.Sleep(300 * time.Millisecond) // close runs detached
	if !contains(calls(), "close_agent %3") {
		t.Errorf("calls %q", calls())
	}
}

func TestCtrlArrowsMoveByWordInTheReply(t *testing.T) {
	m, _, _ := newModel(t, "")
	press(m, "ctrl+r", "one two three")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	press(m, "X")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	press(m, "Y")
	if got := m.reply.Value(); got != "one Xtwo threeY" && got != "one XtwoY three" {
		t.Errorf("reply %q", got)
	}
}
