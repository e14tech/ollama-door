// Package theme is the caller's screen style.
//
// Mono sends no ANSI at all, which is safe for a dumb terminal or an
// Apple II. Green is a bright-green-on-black CRT for terminals that
// understand the original 16-color SGR codes (ANSI.SYS, Telix, Qmodem,
// and the ANSI modes of C64 and Apple II terminal programs).
//
// Only classic codes are used. There is no 256-color (38;5) and no
// truecolor (38;2). Those sequences are noise on a period terminal.
package theme

import (
	"fmt"
	"strings"
)

const (
	esc = "\x1b"

	// greenSGR is reset, then intensity, green foreground, black background.
	// The leading 0 clears whatever the terminal had set before.
	greenSGR = esc + "[0;1;32;40m"
	clear    = esc + "[2J"
	home     = esc + "[H"
	reset    = esc + "[0m"
)

// Theme is written around the call.
// Start goes out once before the greeting. End goes out on the way out.
type Theme struct {
	Name  string
	Start string
	End   string
}

// Mono is plain text. Start and End are empty on purpose.
var Mono = Theme{Name: "mono"}

// Green clears the screen, homes the cursor, and paints bright green
// on black. The color is set before the clear so the blank screen is
// black, then set again because some terminals drop the attribute on clear.
var Green = Theme{
	Name:  "green",
	Start: greenSGR + clear + home + greenSGR,
	End:   reset,
}

// Parse returns the theme named name. Names are case-insensitive.
// The known names are "mono" and "green".
func Parse(name string) (Theme, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mono":
		return Mono, nil
	case "green":
		return Green, nil
	default:
		return Theme{}, fmt.Errorf("unknown theme %q", name)
	}
}
