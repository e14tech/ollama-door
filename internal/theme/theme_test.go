package theme

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	mono, err := Parse("mono")
	if err != nil {
		t.Fatal(err)
	}
	if mono.Start != "" || mono.End != "" {
		t.Fatalf("mono must not send ANSI, start=%q end=%q", mono.Start, mono.End)
	}
	if strings.Contains(mono.Start+mono.End, "\x1b") {
		t.Fatal("mono contains an escape")
	}

	green, err := Parse("  GREEN ")
	if err != nil {
		t.Fatal(err)
	}
	// Color, clear, home, then the color again so the blank screen stays green on black.
	const wantStart = "\x1b[0;1;32;40m\x1b[2J\x1b[H\x1b[0;1;32;40m"
	if green.Start != wantStart {
		t.Fatalf("green start = %q, want %q", green.Start, wantStart)
	}
	if green.End != "\x1b[0m" {
		t.Fatalf("green end = %q", green.End)
	}
	clearAt := strings.Index(green.Start, "\x1b[2J")
	homeAt := strings.Index(green.Start, "\x1b[H")
	lastColor := strings.LastIndex(green.Start, "\x1b[0;1;32;40m")
	if clearAt <= 0 || homeAt <= clearAt || lastColor <= homeAt {
		t.Fatalf("color is not re-asserted after clear+home: %q", green.Start)
	}
	for _, bad := range []string{"38;5", "38;2", "48;5", "48;2"} {
		if strings.Contains(green.Start, bad) || strings.Contains(green.End, bad) {
			t.Errorf("green theme contains %s", bad)
		}
	}

	if _, err := Parse("amber"); err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
}
