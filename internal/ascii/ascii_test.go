package ascii

import "testing"

// TestSanitize is the contract for Sanitize.
func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "smart quotes and em dash",
			in:   "\u201cHello\u201d \u2014 Josh",
			want: "\"Hello\" - Josh",
		},
		{
			name: "emoji is dropped",
			in:   "wave \U0001F44B",
			want: "wave ",
		},
		{
			name: "emoji in the middle",
			in:   "a\U0001F600b",
			want: "ab",
		},
		{
			name: "single smart quote",
			in:   "it\u2019s",
			want: "it's",
		},
		{
			name: "en dash",
			in:   "a\u2013b",
			want: "a-b",
		},
		{
			name: "ellipsis",
			in:   "wait\u2026",
			want: "wait...",
		},
		{
			name: "nbsp",
			in:   "a\u00A0b",
			want: "a b",
		},
		{
			name: "bullet",
			in:   "\u2022 item",
			want: "* item",
		},
		{
			name: "accented latin",
			in:   "caf\u00e9 na\u00efve",
			want: "cafe naive",
		},
		{
			name: "more accents",
			in:   "\u00c0\u00c9\u00ce\u00d5\u00dc\u00f1\u00e7\u00f8\u00df\u00e6\u0153",
			want: "AEIOUncossaeoe",
		},
		{
			name: "newline stays",
			in:   "a\nb",
			want: "a\nb",
		},
		{
			name: "tab becomes space",
			in:   "a\tb",
			want: "a b",
		},
		{
			name: "controls are dropped",
			in:   "a\x00b\x1bc\x7fd",
			want: "abcd",
		},
		{
			name: "markdown marks stay",
			in:   "**bold** and _under_",
			want: "**bold** and _under_",
		},
		{
			name: "plain ascii",
			in:   "Hello, world! (ok) 100%",
			want: "Hello, world! (ok) 100%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
