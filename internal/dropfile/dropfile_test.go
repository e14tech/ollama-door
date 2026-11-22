package dropfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
			want: Info{Alias: "g00r00", ANSI: true, MinutesLeft: 58},
		},
		{
			name: "door32 crlf lowercase",
			file: "testdata/crlf/door32.sys",
			want: Info{Alias: "Josh", ANSI: true, MinutesLeft: 60},
		},
		{
			name: "door32 mixed-case name",
			file: "testdata/Door32.Sys",
			want: Info{Alias: "Case", ANSI: true, MinutesLeft: 9},
		},
		{
			name: "door32 ascii emulation",
			file: "testdata/ascii/door32.sys",
			want: Info{Alias: "PlainUser", ANSI: false, MinutesLeft: 15},
		},
		{
			name: "door32 avatar falls back to ansi",
			file: "testdata/avatar/door32.sys",
			want: Info{Alias: "Ava", ANSI: true, MinutesLeft: 20},
		},
		{
			name: "door.sys 52 lines",
			file: "testdata/DOOR.SYS",
			want: Info{Alias: "Stud", ANSI: true, MinutesLeft: 126},
		},
		{
			name: "door.sys short file uses line 10",
			file: "testdata/short/door.sys",
			want: Info{Alias: "Ada", ANSI: false, MinutesLeft: 0},
		},
		{
			name: "door.sys NG",
			file: "testdata/ng/door.sys",
			want: Info{Alias: "Pat", ANSI: false, MinutesLeft: 5},
		},
		{
			name: "door.sys blank alias line",
			file: "testdata/blank36/door.sys",
			want: Info{Alias: "Node User", ANSI: true, MinutesLeft: 15},
		},
		{
			name: "door.sys padded graphics line",
			file: "testdata/grspace/door.sys",
			want: Info{Alias: "Ada Lovelace", ANSI: true, MinutesLeft: 12},
		},
		{
			name: "shipped example",
			file: filepath.Join("..", "..", "examples", "door32.sys"),
			want: Info{Alias: "Josh", ANSI: true, MinutesLeft: 60},
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
		if !got.ANSI || got.Alias != "Rip" || got.MinutesLeft != 8 {
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

func TestParseMissing(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "door32.sys"))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v", err)
	}
}
