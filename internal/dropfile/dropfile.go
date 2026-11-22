// Package dropfile reads the small text file a BBS writes before it
// starts a door. The door learns the caller's name, whether the terminal
// can do ANSI, and how many minutes are left.
package dropfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Info is the part of a drop file this door uses.
type Info struct {
	Alias       string
	ANSI        bool
	MinutesLeft int
}

// Parse reads a DOOR32.SYS or DOOR.SYS file. The format comes from the
// base name, compared without regard to case, so door32.sys and
// DOOR32.SYS are the same file. Each line has its trailing CR and
// surrounding whitespace removed. Linux BBSes often write DOS CRLF.
func Parse(path string) (Info, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Info{}, fmt.Errorf("read %s: %w", path, err)
	}
	lines := splitLines(b)
	switch name := filepath.Base(path); {
	case strings.EqualFold(name, "DOOR32.SYS"):
		return parseDoor32(lines), nil
	case strings.EqualFold(name, "DOOR.SYS"):
		return parseDoorSys(lines), nil
	default:
		return Info{}, fmt.Errorf("%s: file name must be DOOR32.SYS or DOOR.SYS", path)
	}
}

// splitLines breaks a drop file into fields.
// A blank line still counts, so a missing field does not shift the rest.
// One trailing empty field from the final newline is dropped.
func splitLines(b []byte) []string {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	raw := strings.Split(text, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = strings.TrimSpace(line)
	}
	return out
}

// field returns the 1-based line, or "" when the file is shorter than that.
func field(lines []string, n int) string {
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}

func atoiField(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseDoor32 reads Door32 Revision 1 (February 23, 2001).
// The spec is mirrored at
// https://github.com/NuSkooler/ansi-bbs/blob/master/docs/dropfile_formats/door32_sys.txt
// and http://wyobbs.com/doku.php/door32_sys .
//
// Lines this door uses (1-based):
//
//	2   comm or socket handle (an OS handle, not the caller's name)
//	7   handle / alias
//	9   minutes left
//	10  emulation: 0 ASCII, 1 ANSI, 2 Avatar, 3 RIP, 4 Max Graphics
//
// The spec says Avatar, RIP, and Max Graphics all have an ANSI fallback,
// so emulation 1 through 4 sets ANSI. Emulation 0 stays ASCII.
// A short file is fine: missing lines stay at the zero value.
// Line 2 is not the caller's alias. The alias is line 7.
func parseDoor32(lines []string) Info {
	ansi := false
	switch field(lines, 10) {
	case "1", "2", "3", "4":
		ansi = true
	}
	return Info{
		Alias:       field(lines, 7),
		ANSI:        ansi,
		MinutesLeft: atoiField(field(lines, 9)),
	}
}

// parseDoorSys reads the GAP / Synchronet DOOR.SYS file.
// The 52-line format is documented at
// http://wiki.synchro.net/ref:door.sys
// and in the KG revision reproduced at
// https://wiki.fsxnet.nz/resources:dropfileformats:doorsys .
//
// Lines this door uses (1-based):
//
//	10  full name. Synchronet may put the alias here when "Use Real Name" is off.
//	19  minutes remaining this call
//	20  graphics mode: GR, NG, or 7E
//	21  page length in rows. This door does not use it. Column width is the -width flag.
//	36  alias / handle
//
// Line 20 is the graphics mode. GR means the caller can do ANSI.
// NG and 7E (7-bit, even parity) stay ASCII.
// A 31-line file, or a 52-line file with a blank line 36, has no alias
// on line 36. The name on line 10 is used then. Other missing lines stay zero.
func parseDoorSys(lines []string) Info {
	alias := field(lines, 36)
	if alias == "" {
		alias = field(lines, 10)
	}
	return Info{
		Alias:       alias,
		ANSI:        strings.EqualFold(field(lines, 20), "GR"),
		MinutesLeft: atoiField(field(lines, 19)),
	}
}
