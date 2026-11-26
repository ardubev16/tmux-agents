package main

import "testing"

func TestPromptBackgroundShadesTheTerminalsBackgroundTowardItsText(t *testing.T) {
	// Replies as terminals send them: 4 or 2 hex digits per channel, BEL or ST, then the device attributes.
	reply := "\x1b]11;rgb:0000/0000/0000\x07\x1b]10;rgb:ff/ff/ff\x1b\\\x1b[?62;22c"
	if got, ok := promptBackground(reply); !ok || got != "#1f1f1f" {
		t.Errorf("dark theme: %q %v", got, ok)
	}
	if got, ok := promptBackground("\x1b]11;rgb:ffff/ffff/ffff\x07\x1b]10;rgb:0000/0000/0000\x07"); !ok || got != "#e0e0e0" {
		t.Errorf("light theme: %q %v", got, ok)
	}
	if _, ok := promptBackground("\x1b[?62;22c"); ok {
		t.Error("no colors in the reply: nothing to shade")
	}
}
