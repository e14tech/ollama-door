package wrap

import (
	"strings"
	"testing"
)

// TestWrap is the contract for Wrap.
func TestWrap(t *testing.T) {
	sentence := "The BBS door wraps this reply so a forty column screen can read it."
	tests := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{
			name:  "break on the space",
			text:  "hello world",
			width: 5,
			want:  "hello\r\nworld\r\n",
		},
		{
			name:  "short line still ends with CRLF",
			text:  "hi",
			width: 80,
			want:  "hi\r\n",
		},
		{
			name:  "40 columns",
			text:  sentence,
			width: 40,
			want:  "The BBS door wraps this reply so a forty\r\ncolumn screen can read it.\r\n",
		},
		{
			name:  "80 columns keeps one line",
			text:  sentence,
			width: 80,
			want:  sentence + "\r\n",
		},
		{
			name:  "hard break a long word",
			text:  "abcdefghij",
			width: 4,
			want:  "abcd\r\nefgh\r\nij\r\n",
		},
		{
			name:  "hard break after a short word",
			text:  "hi abcdefghij",
			width: 4,
			want:  "hi\r\nabcd\r\nefgh\r\nij\r\n",
		},
		{
			name:  "paragraph break",
			text:  "one\n\ntwo",
			width: 80,
			want:  "one\r\n\r\ntwo\r\n",
		},
		{
			name:  "crlf input",
			text:  "a\r\nb",
			width: 80,
			want:  "a\r\nb\r\n",
		},
		{
			name:  "empty",
			text:  "",
			width: 80,
			want:  "",
		},
		{
			name:  "width one",
			text:  "ab",
			width: 1,
			want:  "a\r\nb\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Wrap(tt.text, tt.width); got != tt.want {
				t.Errorf("Wrap(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
		})
	}
}

// TestLineWriterStreams checks that a full line goes out at the next
// word boundary, and the leftover waits for Flush.
func TestLineWriterStreams(t *testing.T) {
	var b strings.Builder
	w := NewLineWriter(&b, 5)

	if err := w.WriteString("hel"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteString("lo "); err != nil {
		t.Fatal(err)
	}
	if b.String() != "" {
		t.Fatalf("line held until the next word, got %q", b.String())
	}
	if err := w.WriteString("wor"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteString("ld "); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "hello\r\n"; got != want {
		t.Fatalf("after the second word: %q, want %q", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "hello\r\nworld\r\n"; got != want {
		t.Fatalf("flush: %q, want %q", got, want)
	}
}
