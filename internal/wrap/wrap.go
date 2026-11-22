// Package wrap folds plain text to a fixed column width and ends every
// line with CRLF, which DOS, Apple II, and Commodore 64 terminals expect.
package wrap

import (
	"io"
	"strings"
)

// Wrap returns text broken into lines of at most width columns.
//
// Split on spaces. A word longer than width is cut into width-sized
// pieces so it cannot run off the screen. A newline ends the current
// line. Two newlines in a row leave a blank line, which keeps paragraphs
// apart. Each output line, including the last, ends with CRLF ("\r\n").
// Empty text returns empty text, with no extra blank line.
//
// width is the screen size from the -width flag. Classic doors use 40 or 80.
// A width below 1 is treated as 1.
//
// The text is plain ASCII (see ascii.Sanitize), so len is the column count.
func Wrap(text string, width int) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	w := NewLineWriter(&b, width)
	// strings.Builder.Write never fails.
	_ = w.WriteString(text)
	_ = w.Flush()
	return b.String()
}

// LineWriter accepts text as it arrives (streamed tokens, for example)
// and writes wrapped lines to out. Each finished line ends with CRLF.
//
// A line is written when the next word does not fit, when the input has
// a newline, or when Flush is called. A line that is exactly width stays
// buffered until then, so a following newline does not become a blank line.
type LineWriter struct {
	out   io.Writer
	width int
	line  string // words already placed on this row
	word  string // the word we are still reading
}

// NewLineWriter wraps text to width columns as it is written.
func NewLineWriter(out io.Writer, width int) *LineWriter {
	if width < 1 {
		width = 1
	}
	return &LineWriter{out: out, width: width}
}

// WriteString adds the next piece of text.
func (w *LineWriter) WriteString(s string) error {
	for i := 0; i < len(s); i++ {
		if err := w.one(s[i]); err != nil {
			return err
		}
	}
	return nil
}

// Flush writes the unfinished line, if there is one.
func (w *LineWriter) Flush() error {
	if err := w.endWord(); err != nil {
		return err
	}
	if w.line == "" {
		return nil
	}
	return w.emit()
}

func (w *LineWriter) one(c byte) error {
	switch c {
	case '\r':
		return nil
	case '\n':
		return w.newline()
	case ' ', '\t':
		return w.endWord()
	default:
		w.word += string(c)
		if len(w.word) <= w.width {
			return nil
		}
		// The word itself is wider than the screen. Finish the current
		// row first, then cut off a full-width piece.
		if w.line != "" {
			if err := w.emit(); err != nil {
				return err
			}
		}
		if err := w.writeRow(w.word[:w.width]); err != nil {
			return err
		}
		w.word = w.word[w.width:]
		return nil
	}
}

func (w *LineWriter) endWord() error {
	if w.word == "" {
		return nil
	}
	word := w.word
	w.word = ""
	if w.line == "" {
		w.line = word
		return nil
	}
	if len(w.line)+1+len(word) <= w.width {
		w.line += " " + word
		return nil
	}
	if err := w.emit(); err != nil {
		return err
	}
	w.line = word
	return nil
}

func (w *LineWriter) newline() error {
	if err := w.endWord(); err != nil {
		return err
	}
	return w.emit()
}

func (w *LineWriter) emit() error {
	row := w.line
	w.line = ""
	return w.writeRow(row)
}

func (w *LineWriter) writeRow(s string) error {
	if _, err := io.WriteString(w.out, s); err != nil {
		return err
	}
	_, err := io.WriteString(w.out, "\r\n")
	return err
}
