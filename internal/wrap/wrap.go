// Package wrap folds plain text to a fixed column width and ends every
// line with CRLF, which DOS, Apple II, and Commodore 64 terminals expect.
package wrap

// Wrap returns text broken into lines of at most width columns.
//
// Split on spaces. A word longer than width stays whole on its own line
// so a token is never cut in the middle. An existing newline starts a
// new line. Each output line, including the last, ends with CRLF ("\r\n").
// Empty text returns empty text, with no extra blank line.
//
// width is the screen size from the -width flag. Classic doors use 40 or 80.
// The function assumes width >= 1.
//
// TODO: implement the wrap. Until then text is returned unchanged and the
// caller in main still appends a CRLF when one is missing.
func Wrap(text string, width int) string {
	_ = width
	return text
}
