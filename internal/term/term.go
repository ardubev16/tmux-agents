// Package term runs a program in a pseudo-terminal sized to part of the dashboard (lazygit in place of the
// chat), keeps its screen in a terminal emulator, and forwards Bubble Tea keys to it.
package term

import (
	"io"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

type Term struct {
	emu  *vt.SafeEmulator
	pty  *os.File
	done chan struct{}
}

// Start runs cmd at width×height. changed is called, from another goroutine, whenever the screen changes and
// once cmd has exited.
func Start(cmd *exec.Cmd, width, height int, changed func()) (*Term, error) {
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color") // what the emulator speaks, whatever the dashboard runs in
	// The pty becomes the program's controlling terminal, so it can open /dev/tty (lazygit does).
	p, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return nil, err
	}
	t := &Term{emu: vt.NewSafeEmulator(width, height), pty: p, done: make(chan struct{})}
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				_, _ = t.emu.Write(buf[:n]) // fails only once closed
				changed()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { _, _ = io.Copy(p, t.emu) }() // keys, and the emulator's answers to the program's queries
	go func() {
		_ = cmd.Wait() // how it exited does not matter: the chat comes back either way
		close(t.done)
		changed()
	}()
	return t, nil
}

// Exited reports whether the program has ended; Close then releases the terminal.
func (t *Term) Exited() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Term) Close() {
	t.pty.Close()
	t.emu.Close()
}

func (t *Term) Resize(width, height int) {
	_ = pty.Setsize(t.pty, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)}) // fails only once it exited
	t.emu.Resize(width, height)
}

// View is the screen, with the cursor in reverse video where the program shows it.
func (t *Term) View() string {
	lines := strings.Split(t.emu.Render(), "\n")
	if pos := t.emu.CursorPosition(); !t.emu.CursorHidden() && pos.Y >= 0 && pos.Y < len(lines) {
		line := lines[pos.Y]
		line += strings.Repeat(" ", max(0, pos.X+1-ansi.StringWidth(line)))
		under := ansi.Strip(ansi.Cut(line, pos.X, pos.X+1))
		lines[pos.Y] = ansi.Cut(line, 0, pos.X) + "\x1b[7m" + under + "\x1b[m" + ansi.Cut(line, pos.X+1, ansi.StringWidth(line))
	}
	return strings.Join(lines, "\n")
}

var keys = map[tea.KeyType]uv.Key{
	tea.KeyEnter:     {Code: uv.KeyEnter},
	tea.KeyTab:       {Code: uv.KeyTab},
	tea.KeyShiftTab:  {Code: uv.KeyTab, Mod: uv.ModShift},
	tea.KeyEsc:       {Code: uv.KeyEscape},
	tea.KeyBackspace: {Code: uv.KeyBackspace},
	tea.KeySpace:     {Code: uv.KeySpace, Text: " "},
	tea.KeyUp:        {Code: uv.KeyUp},
	tea.KeyDown:      {Code: uv.KeyDown},
	tea.KeyRight:     {Code: uv.KeyRight},
	tea.KeyLeft:      {Code: uv.KeyLeft},
	tea.KeyShiftUp:   {Code: uv.KeyUp, Mod: uv.ModShift},
	tea.KeyShiftDown: {Code: uv.KeyDown, Mod: uv.ModShift},
	tea.KeyCtrlUp:    {Code: uv.KeyUp, Mod: uv.ModCtrl},
	tea.KeyCtrlDown:  {Code: uv.KeyDown, Mod: uv.ModCtrl},
	tea.KeyCtrlRight: {Code: uv.KeyRight, Mod: uv.ModCtrl},
	tea.KeyCtrlLeft:  {Code: uv.KeyLeft, Mod: uv.ModCtrl},
	tea.KeyHome:      {Code: uv.KeyHome},
	tea.KeyEnd:       {Code: uv.KeyEnd},
	tea.KeyPgUp:      {Code: uv.KeyPgUp},
	tea.KeyPgDown:    {Code: uv.KeyPgDown},
	tea.KeyDelete:    {Code: uv.KeyDelete},
	tea.KeyInsert:    {Code: uv.KeyInsert},
	tea.KeyF1:        {Code: uv.KeyF1},
	tea.KeyF2:        {Code: uv.KeyF2},
	tea.KeyF3:        {Code: uv.KeyF3},
	tea.KeyF4:        {Code: uv.KeyF4},
	tea.KeyF5:        {Code: uv.KeyF5},
}

// Key forwards a key press to the program; keys it does not know are dropped.
func (t *Term) Key(msg tea.KeyMsg) {
	var mod uv.KeyMod
	if msg.Alt {
		mod = uv.ModAlt
	}
	switch k, ok := keys[msg.Type]; {
	case msg.Paste:
		t.emu.Paste(string(msg.Runes))
	case ok:
		k.Mod |= mod
		t.emu.SendKey(uv.KeyPressEvent(k))
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1:
		t.emu.SendKey(uv.KeyPressEvent{Code: msg.Runes[0], Text: string(msg.Runes), Mod: mod})
	case msg.Type == tea.KeyRunes:
		t.emu.SendText(string(msg.Runes))
	case msg.Type >= tea.KeyCtrlA && msg.Type <= tea.KeyCtrlZ:
		t.emu.SendKey(uv.KeyPressEvent{Code: 'a' + rune(msg.Type-tea.KeyCtrlA), Mod: mod | uv.ModCtrl})
	}
}

var buttons = map[tea.MouseButton]uv.MouseButton{
	tea.MouseButtonLeft:       uv.MouseLeft,
	tea.MouseButtonMiddle:     uv.MouseMiddle,
	tea.MouseButtonRight:      uv.MouseRight,
	tea.MouseButtonWheelUp:    uv.MouseWheelUp,
	tea.MouseButtonWheelDown:  uv.MouseWheelDown,
	tea.MouseButtonWheelLeft:  uv.MouseWheelLeft,
	tea.MouseButtonWheelRight: uv.MouseWheelRight,
}

// Mouse forwards a mouse event at (x, y), counted from the terminal's top-left corner. The emulator encodes
// it the way the program asked for, and drops it if the program did not turn mouse reporting on.
func (t *Term) Mouse(msg tea.MouseMsg, x, y int) {
	m := uv.Mouse{X: x, Y: y, Button: buttons[msg.Button]}
	if msg.Alt {
		m.Mod |= uv.ModAlt
	}
	if msg.Ctrl {
		m.Mod |= uv.ModCtrl
	}
	if msg.Shift {
		m.Mod |= uv.ModShift
	}
	switch {
	case tea.MouseEvent(msg).IsWheel():
		t.emu.SendMouse(uv.MouseWheelEvent(m))
	case msg.Action == tea.MouseActionPress:
		t.emu.SendMouse(uv.MouseClickEvent(m))
	case msg.Action == tea.MouseActionRelease:
		t.emu.SendMouse(uv.MouseReleaseEvent(m))
	default:
		t.emu.SendMouse(uv.MouseMotionEvent(m))
	}
}
