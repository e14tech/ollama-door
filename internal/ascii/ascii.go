// Package ascii turns caller-visible text into plain 7-bit ASCII.
//
// Old BBS terminals (DOS, Apple II, Commodore 64) draw bytes 0-127.
// Smart quotes, dashes, and ellipses have direct ASCII stand-ins.
// Emoji and every other non-ASCII rune are dropped.
package ascii

import "strings"

// fold is accented Latin letters that become a plain ASCII base.
// The base can be more than one letter, as in "ss" for ß.
var fold = []struct {
	to      string
	letters string
}{
	{"A", "ÀÁÂÃÄÅ"},
	{"a", "àáâãäå"},
	{"AE", "Æ"},
	{"ae", "æ"},
	{"C", "Ç"},
	{"c", "ç"},
	{"D", "Ð"},
	{"d", "ð"},
	{"E", "ÈÉÊË"},
	{"e", "èéêë"},
	{"I", "ÌÍÎÏ"},
	{"i", "ìíîï"},
	{"N", "Ñ"},
	{"n", "ñ"},
	{"O", "ÒÓÔÕÖØ"},
	{"o", "òóôõöø"},
	{"OE", "Œ"},
	{"oe", "œ"},
	{"ss", "ß"},
	{"Th", "Þ"},
	{"th", "þ"},
	{"U", "ÙÚÛÜ"},
	{"u", "ùúûü"},
	{"Y", "ÝŸ"},
	{"y", "ýÿ"},
}

// translit maps a non-ASCII rune to its ASCII stand-in.
// Runes that are not listed are dropped.
var translit = buildTranslit()

func buildTranslit() map[rune]string {
	m := map[rune]string{
		// Smart single quotes.
		'\u2018': "'",
		'\u2019': "'",
		'\u201A': "'",
		'\u201B': "'",
		// Smart double quotes, including « ».
		'\u201C': `"`,
		'\u201D': `"`,
		'\u201E': `"`,
		'\u201F': `"`,
		'\u00AB': `"`,
		'\u00BB': `"`,
		// Dashes. One hyphen keeps a wrapped line the same width.
		'\u2010': "-",
		'\u2011': "-",
		'\u2012': "-",
		'\u2013': "-",
		'\u2014': "-",
		'\u2015': "-",
		'\u2026': "...",
		'\u2022': "*",
		'\u00B7': "*",
	}
	// Unicode spaces become a normal space so words do not glue together.
	for _, r := range []rune{'\u00A0', '\u2000', '\u2001', '\u2002', '\u2003', '\u2009', '\u200A', '\u202F'} {
		m[r] = " "
	}
	for _, row := range fold {
		for _, r := range row.letters {
			if _, exists := m[r]; exists {
				panic("duplicate transliteration for " + string(r))
			}
			m[r] = row.to
		}
	}
	return m
}

// Sanitize returns s as 7-bit printable ASCII.
//
// A tab becomes a space. A newline is kept. Every other control
// character, including carriage return and DEL, is dropped.
// Common punctuation and accented Latin letters are transliterated.
// Emoji and anything else above 0x7F with no entry in the map is removed.
// Marks such as * and _ are kept; this function only changes characters.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7F:
			// Other controls (NUL, ESC, CR, DEL) would confuse a terminal.
		case r < 0x7F:
			b.WriteRune(r)
		default:
			if repl, ok := translit[r]; ok {
				b.WriteString(repl)
			}
		}
	}
	return b.String()
}
