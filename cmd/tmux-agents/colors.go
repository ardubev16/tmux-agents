package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/charmbracelet/x/term"
)

var (
	oscColor = regexp.MustCompile(`\x1b\](1[01]);rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`)
	daReply  = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// queryColors asks the terminal for its background and foreground (OSC 11 and 10; tmux answers for the pane).
// termenv will not ask inside tmux, so it is done here, before Bubble Tea takes the terminal.
func queryColors() string {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return ""
	}
	defer tty.Close()
	state, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return ""
	}
	defer func() { _ = term.Restore(tty.Fd(), state) }()
	// The device attributes query last: every terminal answers it, so its reply ends the wait even when the
	// colors get none.
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b]10;?\x1b\\\x1b[c"); err != nil {
		return ""
	}
	_ = tty.SetReadDeadline(time.Now().Add(time.Second)) // not every platform supports one on a tty
	var reply []byte
	buf := make([]byte, 256)
	for !daReply.Match(reply) {
		n, err := tty.Read(buf)
		reply = append(reply, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(reply)
}

// promptBackground is the terminal's background moved 12% toward its foreground, as "#rrggbb", from the replies
// to queryColors: a shade of the theme that sets prompts apart in any theme. ok is false without both colors.
func promptBackground(reply string) (string, bool) {
	var rgb [2][3]float64 // background, foreground
	found := [2]bool{}
	for _, m := range oscColor.FindAllStringSubmatch(reply, -1) {
		which := 0
		if m[1] == "10" {
			which = 1
		}
		for i, hex := range m[2:] {
			v, _ := strconv.ParseUint(hex, 16, 16) // the pattern allows only 1-4 hex digits
			rgb[which][i] = float64(v) / float64(uint64(1)<<(4*len(hex))-1)
		}
		found[which] = true
	}
	if !found[0] || !found[1] {
		return "", false
	}
	var out [3]int
	for i := range out {
		out[i] = int((rgb[0][i]+(rgb[1][i]-rgb[0][i])*0.12)*255 + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2]), true
}
