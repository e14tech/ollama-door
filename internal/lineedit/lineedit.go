// Package lineedit reads one line from a BBS door's stdin.
//
// A door has no local terminal echo and no line discipline. The caller
// sees nothing unless we write it back, and Enter arrives as CR, not LF.
// Some programs send CRLF, LFCR, or CR followed by a NUL. Those are one
// Enter, not a blank extra line.
package lineedit

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// DefaultMax is the character cap when Options.Max is left at 0.
const DefaultMax = 500

// Options controls echo and the length cap.
type Options struct {
	// Echo writes each accepted key back to out. Turn this off when a
	// real terminal is already echoing, or the caller sees every letter twice.
	Echo bool
	// Max is how many characters one line may hold. Zero means DefaultMax.
	// Extra printable characters are dropped. When Echo is on, each dropped
	// character beeps (ASCII BEL).
	Max int
}

// Reader pulls bytes from in and returns a finished line at a time.
type Reader struct {
	in   *bufio.Reader
	out  io.Writer
	echo bool
	max  int
	line []byte
	// drop remembers that the previous byte was Enter, so the next byte
	// can be the other half of the same key (LF after CR, CR after LF,
	// or NUL after CR). It is not a peek: peeking would wait for another
	// key and Enter would look dead.
	drop byte
}

const (
	dropNone byte = 0
	dropCR   byte = '\r'
	dropLF   byte = '\n'
)

// New starts a reader. out may be nil when Echo is false.
func New(in io.Reader, out io.Writer, opt Options) *Reader {
	max := opt.Max
	if max <= 0 {
		max = DefaultMax
	}
	if out == nil {
		out = io.Discard
	}
	return &Reader{
		in:   bufio.NewReader(in),
		out:  out,
		echo: opt.Echo,
		max:  max,
	}
}

// Next returns the next finished line.
// An empty line and a nil error means the caller pressed Enter on a blank row.
// io.EOF means the stream ended. A partial line with no Enter is returned
// together with io.EOF. Any other error is a read or echo failure.
func (r *Reader) Next() (string, error) {
	for {
		b, err := r.in.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(r.line) > 0 {
					return r.take(), io.EOF
				}
				return "", io.EOF
			}
			return "", err
		}
		if r.drop != 0 {
			drop := r.drop
			r.drop = dropNone
			if drop == dropLF && (b == '\n' || b == 0) {
				continue
			}
			if drop == dropCR && b == '\r' {
				continue
			}
		}
		switch b {
		case '\r', '\n':
			if b == '\r' {
				r.drop = dropLF
			} else {
				r.drop = dropCR
			}
			if err := r.echoBytes([]byte("\r\n")); err != nil {
				return "", err
			}
			return r.take(), nil
		case 0x08, 0x7F:
			if len(r.line) == 0 {
				continue
			}
			r.line = r.line[:len(r.line)-1]
			if err := r.echoBytes([]byte("\b \b")); err != nil {
				return "", err
			}
		case 0x15: // Ctrl-U, clear the line
			if len(r.line) == 0 {
				continue
			}
			n := len(r.line)
			r.line = r.line[:0]
			if err := r.echoBytes(bytes.Repeat([]byte("\b \b"), n)); err != nil {
				return "", err
			}
		case 0x03, 0x04: // Ctrl-C or Ctrl-D on an empty line quits
			if len(r.line) == 0 {
				if err := r.echoBytes([]byte("\r\n")); err != nil {
					return "", err
				}
				return "quit", nil
			}
		case 0x1b:
			if err := r.swallowESC(); err != nil {
				return r.end(err)
			}
		case 0xff:
			if err := r.swallowIAC(); err != nil {
				return r.end(err)
			}
		default:
			if b < 0x20 || b > 0x7e {
				continue
			}
			if len(r.line) >= r.max {
				if err := r.echoBytes([]byte{0x07}); err != nil {
					return "", err
				}
				continue
			}
			r.line = append(r.line, b)
			if err := r.echoBytes([]byte{b}); err != nil {
				return "", err
			}
		}
	}
}

// end turns a failure while swallowing a sequence into Next's result.
// EOF keeps a partial line. Anything else is returned as-is.
func (r *Reader) end(err error) (string, error) {
	if errors.Is(err, io.EOF) && len(r.line) > 0 {
		return r.take(), io.EOF
	}
	return "", err
}

func (r *Reader) take() string {
	s := string(r.line)
	r.line = r.line[:0]
	return s
}

func (r *Reader) echoBytes(p []byte) error {
	if !r.echo || len(p) == 0 {
		return nil
	}
	_, err := r.out.Write(p)
	return err
}

// swallowESC drops an ANSI sequence such as an arrow key (ESC [ A).
// The ESC byte is already consumed.
func (r *Reader) swallowESC() error {
	b, err := r.in.ReadByte()
	if err != nil {
		return err
	}
	if b != '[' && b != 'O' {
		return nil
	}
	if b == 'O' {
		_, err = r.in.ReadByte()
		return err
	}
	for {
		b, err = r.in.ReadByte()
		if err != nil {
			return err
		}
		// CSI ends on a byte in @ through ~.
		if b >= 0x40 && b <= 0x7e {
			return nil
		}
	}
}

// swallowIAC drops one telnet command. The IAC byte (0xFF) is already consumed.
func (r *Reader) swallowIAC() error {
	cmd, err := r.in.ReadByte()
	if err != nil {
		return err
	}
	switch cmd {
	case 0xff:
		return nil
	case 0xfa: // SB ... IAC SE
		for {
			b, err := r.in.ReadByte()
			if err != nil {
				return err
			}
			if b != 0xff {
				continue
			}
			b, err = r.in.ReadByte()
			if err != nil {
				return err
			}
			if b == 0xf0 { // SE
				return nil
			}
		}
	case 0xfb, 0xfc, 0xfd, 0xfe: // WILL WONT DO DONT, plus one option byte
		_, err = r.in.ReadByte()
		return err
	default:
		return nil
	}
}
