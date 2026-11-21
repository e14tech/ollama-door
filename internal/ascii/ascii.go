// Package ascii turns caller-visible text into plain 7-bit ASCII.
//
// Old BBS terminals (DOS, Apple II, Commodore 64) draw bytes 0-127.
// Smart quotes, dashes, and ellipses have direct ASCII stand-ins.
// Emoji and every other non-ASCII rune are dropped.
package ascii

// Sanitize returns s as 7-bit ASCII.
//
// Transliterate common punctuation, then drop every rune above 0x7F:
//
//	U+2018 and U+2019 (single quotes) -> '
//	U+201C and U+201D (double quotes) -> "
//	U+2013 and U+2014 (en and em dash) -> -
//	U+2026 (ellipsis) -> ...
//	U+00A0 (non-breaking space) -> ASCII space
//
// Everything else above 0x7F, including emoji, is removed with no
// replacement. Bytes 0x00-0x7F pass through unchanged, including
// tabs and newlines.
//
// TODO: implement the map and the filter. Until then s is returned unchanged.
func Sanitize(s string) string {
	return s
}
