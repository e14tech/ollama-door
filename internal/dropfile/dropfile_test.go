package dropfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		file string
		want Info
	}{
		{
			name: "door32 spec sample",
			file: "testdata/door32.sys",
			want: Info{Alias: "g00r00", ANSI: true, MinutesLeft: 58, HasLimit: true, Node: 1},
		},
		{
			name: "door32 crlf lowercase",
			file: "testdata/crlf/door32.sys",
			want: Info{Alias: "Josh", ANSI: true, MinutesLeft: 60, HasLimit: true, Node: 1},
		},
		{
			name: "door32 mixed-case name",
			file: "testdata/Door32.Sys",
			want: Info{Alias: "Case", ANSI: true, MinutesLeft: 9, HasLimit: true, Node: 1},
		},
		{
			name: "door32 ascii emulation",
			file: "testdata/ascii/door32.sys",
			want: Info{Alias: "PlainUser", ANSI: false, MinutesLeft: 15, HasLimit: true, Node: 1},
		},
		{
			name: "door32 avatar falls back to ansi",
			file: "testdata/avatar/door32.sys",
			want: Info{Alias: "Ava", ANSI: true, MinutesLeft: 20, HasLimit: true, Node: 1},
		},
		{
			name: "door.sys 52 lines",
			file: "testdata/DOOR.SYS",
			want: Info{Alias: "Stud", ANSI: true, MinutesLeft: 126, HasLimit: true, Node: 1},
		},
		{
			name: "door.sys short file uses line 10",
			file: "testdata/short/door.sys",
			want: Info{Alias: "Ada", ANSI: false, MinutesLeft: 0},
		},
		{
			name: "door.sys NG",
			file: "testdata/ng/door.sys",
			want: Info{Alias: "Pat", ANSI: false, MinutesLeft: 5, HasLimit: true},
		},
		{
			name: "door.sys blank alias line",
			file: "testdata/blank36/door.sys",
			want: Info{Alias: "Node User", ANSI: true, MinutesLeft: 15, HasLimit: true},
		},
		{
			name: "door.sys padded graphics line",
			file: "testdata/grspace/door.sys",
			want: Info{Alias: "Ada Lovelace", ANSI: true, MinutesLeft: 12, HasLimit: true},
		},
		{
			name: "shipped example",
			file: filepath.Join("..", "..", "examples", "door32.sys"),
			want: Info{Alias: "Josh", ANSI: true, MinutesLeft: 60, HasLimit: true, Node: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.file)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%s) = %+v, want %+v", tt.file, got, tt.want)
			}
		})
	}
}

func TestCRLFFixtureHasCR(t *testing.T) {
	files := []string{
		"testdata/crlf/door32.sys",
		"testdata/DOOR.SYS",
		filepath.Join("..", "..", "examples", "door32.sys"),
	}
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte("\r\n")) {
			t.Errorf("%s is not CRLF", name)
		}
	}
}

func TestDoorSysFixtureLines(t *testing.T) {
	b, err := os.ReadFile("testdata/DOOR.SYS")
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(b)
	if len(lines) != 52 {
		t.Fatalf("len %d, want 52", len(lines))
	}
	if lines[18] != "126" || lines[19] != "GR" || lines[20] != "23" || lines[35] != "Stud" {
		t.Fatalf("key lines = %q %q %q %q", lines[18], lines[19], lines[20], lines[35])
	}
}

func TestParseRIPAndMax(t *testing.T) {
	for _, emu := range []int{3, 4} {
		dir := t.TempDir()
		path := filepath.Join(dir, "door32.sys")
		body := fmt.Sprintf("2\n0\n38400\nSynchronet\n1\nRip User\nRip\n10\n8\n%d\n1\n", emu)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		if !got.ANSI || got.Alias != "Rip" || got.MinutesLeft != 8 || !got.HasLimit || got.Node != 1 {
			t.Fatalf("emu %d: %+v", emu, got)
		}
	}
}

func TestParseShortDoor32(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "DOOR32.SYS")
	if err := os.WriteFile(path, []byte("2\n0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Info{}) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseBadName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drop.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseMinutesLimit(t *testing.T) {
	tests := []struct {
		name string
		base string
		body string
		want Info
	}{
		{
			name: "door32 zero is a limit",
			base: "door32.sys",
			body: "2\n0\n38400\nSynchronet\n1\nJosh\nJosh\n10\n0\n1\n1\n",
			want: Info{Alias: "Josh", ANSI: true, MinutesLeft: 0, HasLimit: true, Node: 1},
		},
		{
			name: "door32 blank minutes is no limit",
			base: "door32.sys",
			body: "2\n0\n38400\nSynchronet\n1\nJosh\nJosh\n10\n\n1\n1\n",
			want: Info{Alias: "Josh", ANSI: true, Node: 1},
		},
		{
			name: "door32 garbage minutes is no limit",
			base: "DOOR32.SYS",
			body: "2\n0\n38400\nSynchronet\n1\nJosh\nJosh\n10\nsoon\n0\n1\n",
			want: Info{Alias: "Josh", ANSI: false, Node: 1},
		},
		{
			name: "door.sys zero is a limit",
			base: "door.sys",
			body: blankDoorSys(19, "0"),
			want: Info{Alias: "Pat", MinutesLeft: 0, HasLimit: true},
		},
		{
			name: "door.sys missing minutes is no limit",
			base: "DOOR.SYS",
			body: "x\n",
			want: Info{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.base)
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// blankDoorSys builds a 20-line file. Line 10 is Pat, and line n (1-based)
// is set to value when n is the minutes field under test.
func blankDoorSys(n int, value string) string {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "x"
	}
	lines[9] = "Pat"
	lines[n-1] = value
	return strings.Join(lines, "\n") + "\n"
}

func TestNodeNumber(t *testing.T) {
	door32, err := Parse("testdata/door32.sys")
	if err != nil {
		t.Fatal(err)
	}
	if door32.Node != 1 {
		t.Fatalf("DOOR32.SYS line 11: node %d", door32.Node)
	}
	door, err := Parse("testdata/DOOR.SYS")
	if err != nil {
		t.Fatal(err)
	}
	if door.Node != 1 {
		t.Fatalf("DOOR.SYS line 4: node %d", door.Node)
	}
	short, err := Parse("testdata/short/door.sys")
	if err != nil {
		t.Fatal(err)
	}
	if short.Node != 0 {
		t.Fatalf("non-numeric node: %d", short.Node)
	}
}

func TestParseMissing(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "door32.sys"))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v", err)
	}
}
