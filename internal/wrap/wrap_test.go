package wrap

import "testing"

// TestWrap is the contract for Wrap.
// Cases stay skipped until the function is real. Delete t.Skip, then
// make `go test ./internal/wrap` pass.
func TestWrap(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("TODO")
			if got := Wrap(tt.text, tt.width); got != tt.want {
				t.Errorf("Wrap(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
		})
	}
}
