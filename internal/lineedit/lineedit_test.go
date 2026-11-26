package lineedit

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLines(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		max    int
		quiet  bool
		lines  []string
		echoed string
	}{
		{name: "cr", in: "hi\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "crlf", in: "hi\r\n", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "cr nul", in: "hi\r\x00", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "lf", in: "hi\n", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "lfcr", in: "hi\n\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "two enters", in: "\r\n\n", lines: []string{"", ""}, echoed: "\r\n\r\n"},
		{name: "cr cr", in: "\r\r", lines: []string{"", ""}, echoed: "\r\n\r\n"},
		{
			name:   "del",
			in:     "hel\x7flo\r",
			lines:  []string{"helo"},
			echoed: "hel\b \blo\r\n",
		},
		{
			name:   "backspace",
			in:     "hel\x08lo\r",
			lines:  []string{"helo"},
			echoed: "hel\b \blo\r\n",
		},
		{name: "backspace empty", in: "\x7f\x08hi\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "arrow", in: "hi\x1b[D\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "csi", in: "\x1b[1;5Aok\n", lines: []string{"ok"}, echoed: "ok\r\n"},
		{name: "ss3", in: "x\x1bOA\r", lines: []string{"x"}, echoed: "x\r\n"},
		{
			name:   "overflow",
			in:     "abcdef\r",
			max:    4,
			lines:  []string{"abcd"},
			echoed: "abcd\a\a\r\n",
		},
		{
			name:   "ctrl-u",
			in:     "abcd\x15hi\r",
			lines:  []string{"hi"},
			echoed: "abcd\b \b\b \b\b \b\b \bhi\r\n",
		},
		{name: "ctrl-c empty", in: "\x03", lines: []string{"quit"}, echoed: "\r\n"},
		{name: "ctrl-d empty", in: "\x04", lines: []string{"quit"}, echoed: "\r\n"},
		{name: "ctrl-c mid line", in: "ab\x03c\r", lines: []string{"abc"}, echoed: "abc\r\n"},
		{name: "nonascii", in: "a\xc3\xa9b\r", lines: []string{"ab"}, echoed: "ab\r\n"},
		{name: "iac will", in: "\xff\xfb\x01hi\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "iac sb", in: "\xff\xfa\x1f\x00\xff\xf0hi\r", lines: []string{"hi"}, echoed: "hi\r\n"},
		{name: "echo off", in: "hi\r", quiet: true, lines: []string{"hi"}, echoed: ""},
		{name: "partial eof", in: "hi", lines: []string{"hi"}, echoed: "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, echoed := readAll(t, tt.in, !tt.quiet, tt.max)
			if strings.Join(lines, "|") != strings.Join(tt.lines, "|") {
				t.Fatalf("lines %#v, want %#v", lines, tt.lines)
			}
			if echoed != tt.echoed {
				t.Fatalf("echoed %q, want %q", echoed, tt.echoed)
			}
		})
	}
}

func readAll(t *testing.T, in string, echo bool, max int) ([]string, string) {
	t.Helper()
	var out bytes.Buffer
	rd := New(strings.NewReader(in), &out, Options{Echo: echo, Max: max})
	var lines []string
	for {
		line, err := rd.Next()
		if err == nil || line != "" {
			lines = append(lines, line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
	}
	return lines, out.String()
}
