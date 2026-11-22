package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/e14tech/ollama-door/internal/ollama"
	"github.com/e14tech/ollama-door/internal/theme"
)

func TestPickTheme(t *testing.T) {
	tests := []struct {
		name      string
		set       bool
		requested string
		ansi      bool
		want      string
		wantErr   bool
	}{
		{name: "dropfile ansi", ansi: true, want: "green"},
		{name: "dropfile ascii", ansi: false, want: "mono"},
		{name: "explicit mono wins", set: true, requested: "mono", ansi: true, want: "mono"},
		{name: "explicit green", set: true, requested: "green", ansi: false, want: "green"},
		{name: "bad name", set: true, requested: "amber", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickTheme(tt.set, tt.requested, tt.ansi)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.want {
				t.Fatalf("theme %s, want %s", got.Name, tt.want)
			}
		})
	}
}

func TestDeadlineFromMinutes(t *testing.T) {
	if !deadlineFromMinutes(0).IsZero() {
		t.Fatal("zero minutes should not set a deadline")
	}
	d := deadlineFromMinutes(1)
	if d.Before(time.Now()) || d.After(time.Now().Add(2*time.Minute)) {
		t.Fatalf("deadline %v", d)
	}
}

func TestTrimHistoryKeepsSystem(t *testing.T) {
	h := []ollama.Message{{Role: ollama.RoleSystem, Content: "sys"}}
	for i := 0; i < 25; i++ {
		role := ollama.RoleUser
		if i%2 == 1 {
			role = ollama.RoleAssistant
		}
		h = append(h, ollama.Message{Role: role, Content: fmt.Sprintf("m%d", i)})
	}
	got := trimHistory(h)
	if len(got) != historyLimit {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Content != "sys" {
		t.Fatalf("system = %q", got[0].Content)
	}
	// 25 turns plus system is 26. Keep the system and the last 19 turns: m6..m24.
	if got[1].Content != "m6" || got[len(got)-1].Content != "m24" {
		t.Fatalf("tail starts %q ends %q", got[1].Content, got[len(got)-1].Content)
	}
}

func TestTrimHistoryNoSystem(t *testing.T) {
	var h []ollama.Message
	for i := 0; i < 25; i++ {
		h = append(h, ollama.Message{Role: ollama.RoleUser, Content: fmt.Sprintf("m%d", i)})
	}
	got := trimHistory(h)
	if len(got) != historyLimit || got[0].Content != "m5" || got[len(got)-1].Content != "m24" {
		t.Fatalf("%s .. %s (len %d)", got[0].Content, got[len(got)-1].Content, len(got))
	}
}

func TestRealMainRequiresModel(t *testing.T) {
	var errOut bytes.Buffer
	code := realMain(nil, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "-model is required") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRealMainBadTheme(t *testing.T) {
	var errOut bytes.Buffer
	code := realMain([]string{"-model", "m", "-theme", "amber"}, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "mono or green") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRealMainNoDropfileNote(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain([]string{"-model", "m", "-theme", "mono"}, strings.NewReader("quit\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "no -dropfile") {
		t.Fatalf("stderr %q", errOut.String())
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("mono output contains an escape")
	}
	if !strings.Contains(out.String(), "Goodbye.\r\n") || !strings.Contains(out.String(), "> ") {
		t.Fatalf("out %q", out.String())
	}
}

func TestRealMainBadDropfileContinues(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "m", "-theme", "mono", "-dropfile", "missing-door32.sys"},
		strings.NewReader("/q\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "continuing without drop file data") {
		t.Fatalf("stderr %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Goodbye.\r\n") {
		t.Fatalf("out %q", out.String())
	}
}

func TestRealMainGreenFromDropfile(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "m", "-dropfile", "examples/door32.sys"},
		strings.NewReader("bye\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	text := out.String()
	if !strings.HasPrefix(text, theme.Green.Start) || !strings.HasSuffix(text, theme.Green.End) {
		t.Fatalf("theme bytes missing: %q", text)
	}
	wantEsc := strings.Count(theme.Green.Start, "\x1b") + strings.Count(theme.Green.End, "\x1b")
	if got := strings.Count(text, "\x1b"); got != wantEsc {
		t.Fatalf("escape count %d, want %d", got, wantEsc)
	}
	for _, bad := range []string{"38;5", "38;2", "48;5", "48;2"} {
		if strings.Contains(text, bad) {
			t.Fatalf("output contains %s", bad)
		}
	}
	if !strings.Contains(text, "Hello, Josh.\r\n") || !strings.Contains(text, "60 minutes left.\r\n") {
		t.Fatalf("banner %q", text)
	}
}

func TestRealMainThemeOverride(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "m", "-dropfile", "examples/door32.sys", "-theme", "mono"},
		strings.NewReader("QUIT\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("explicit mono still sent ANSI")
	}
	if !strings.Contains(out.String(), "Hello, Josh.\r\n") {
		t.Fatalf("out %q", out.String())
	}
}

func TestRealMainASCIIDropfile(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "m", "-dropfile", "internal/dropfile/testdata/ascii/door32.sys"},
		strings.NewReader("\n  \nBye\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("ASCII drop file selected green")
	}
	if !strings.Contains(out.String(), "Hello, PlainUser.\r\n") {
		t.Fatalf("out %q", out.String())
	}
}

func TestRealMainChat(t *testing.T) {
	var mu sync.Mutex
	var got []ollama.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollama.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		chunks := []string{"caf", "é", " — hi", " \U0001F44B"}
		for _, piece := range chunks {
			fmt.Fprintf(w, "{\"message\":{\"role\":\"assistant\",\"content\":%q},\"done\":false}\n", piece)
		}
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true}\n")
	}))
	defer srv.Close()

	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "llama3.2", "-host", srv.URL, "-theme", "mono", "-width", "80", "-dropfile", "examples/door32.sys"},
		strings.NewReader("hello\nquit\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	text := out.String()
	if strings.Contains(text, "\x1b") || strings.Contains(text, "👋") || strings.Contains(text, "é") {
		t.Fatalf("unsanitized output %q", text)
	}
	if !strings.Contains(text, "cafe - hi\r\n") {
		t.Fatalf("out %q", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("requests %d", len(got))
	}
	req := got[0]
	if !req.Stream || req.Model != "llama3.2" {
		t.Fatalf("req %+v", req)
	}
	if len(req.Messages) < 2 || req.Messages[0].Role != ollama.RoleSystem || req.Messages[1].Content != "hello" {
		t.Fatalf("messages %+v", req.Messages)
	}
	wantSystem := defaultSystem + " The caller's name is Josh."
	if req.Messages[0].Content != wantSystem {
		t.Fatalf("system %q", req.Messages[0].Content)
	}
}

func TestRealMainOllamaErrorContinues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	var out, errOut bytes.Buffer
	code := realMain(
		[]string{"-model", "m", "-host", srv.URL, "-theme", "mono"},
		strings.NewReader("hi\nbye\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), "not answering") || !strings.Contains(out.String(), "Goodbye.\r\n") {
		t.Fatalf("out %q", out.String())
	}
	if !strings.Contains(errOut.String(), "404") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRunTimeAlreadyUp(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), strings.NewReader("hi\n"), &out, io.Discard, session{
		theme:    theme.Mono,
		width:    80,
		deadline: time.Now().Add(-time.Second),
		minutes:  1,
		alias:    "Ada",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Your time is up. Goodbye.\r\n") {
		t.Fatalf("out %q", out.String())
	}
	if strings.Contains(out.String(), "> ") {
		t.Fatalf("prompted a caller who is out of time: %q", out.String())
	}
}

// deadlineTripper stands in for a chat call that the context stopped.
type deadlineTripper struct{}

func (deadlineTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestRunTimeUpDuringChat(t *testing.T) {
	client := ollama.New("http://ollama.invalid")
	client.HTTP = &http.Client{Transport: deadlineTripper{}}
	var out, errOut bytes.Buffer
	err := run(context.Background(), strings.NewReader("hi\n"), &out, &errOut, session{
		client:   client,
		model:    "m",
		theme:    theme.Mono,
		width:    40,
		deadline: time.Now().Add(time.Minute),
		minutes:  1,
	})
	if err != nil {
		t.Fatalf("run: %v, stderr %q", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Your time is up. Goodbye.\r\n") {
		t.Fatalf("out %q stderr %q", out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "not answering") {
		t.Fatalf("time-up looked like an Ollama failure: %q", out.String())
	}
}
