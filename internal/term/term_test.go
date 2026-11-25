package term

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRunsAProgramAndForwardsKeys(t *testing.T) {
	changed := make(chan struct{}, 1000)
	cmd := exec.Command("sh", "-c", `printf 'ready\n'; read line; printf 'got %s\n' "$line"`)
	term, err := Start(cmd, 20, 5, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	waitFor := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-changed:
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s; screen:\n%s", what, ansi.Strip(term.View()))
			}
		}
	}
	waitFor("prompt", func() bool { return strings.Contains(term.View(), "ready") })
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("h")}, {Type: tea.KeyRunes, Runes: []rune("i")}, {Type: tea.KeyEnter}} {
		term.Key(k)
	}
	waitFor("answer", func() bool { return strings.Contains(ansi.Strip(term.View()), "got hi") })
	waitFor("exit", term.Exited)
}

func TestForwardsClicksTheWayTheProgramAskedFor(t *testing.T) {
	changed := make(chan struct{}, 1000)
	// Turns on SGR mouse reporting, then shows the 9 bytes of one click.
	cmd := exec.Command("sh", "-c", `stty -icanon -echo; printf '\033[?1000h\033[?1006hready\n'; head -c 9 | od -An -c`)
	term, err := Start(cmd, 40, 5, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	for !strings.Contains(term.View(), "ready") {
		select {
		case <-changed:
		case <-time.After(5 * time.Second):
			t.Fatal("no prompt")
		}
	}
	term.Mouse(tea.MouseMsg{X: 2, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, 2, 1)
	for want := `033   [   <   0   ;   3   ;   2   M`; !strings.Contains(ansi.Strip(term.View()), want); {
		select {
		case <-changed:
		case <-time.After(5 * time.Second):
			t.Fatalf("want %q on screen:\n%s", want, ansi.Strip(term.View()))
		}
	}
}
