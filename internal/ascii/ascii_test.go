package ascii

import "testing"

// TestSanitize is the contract for Sanitize.
// Cases stay skipped until the function is real. Delete t.Skip, then
// make `go test ./internal/ascii` pass.
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("TODO")
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
