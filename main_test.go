package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/e14tech/ollama-door/internal/dropfile"
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

func TestSessionDeadline(t *testing.T) {
	if !sessionDeadline(60, false).IsZero() {
		t.Fatal("no limit should not set a deadline")
	}
	zero := sessionDeadline(0, true)
	if zero.IsZero() || zero.After(time.Now()) {
		t.Fatalf("a limit of 0 should already be due, got %v", zero)
	}
	d := sessionDeadline(1, true)
	if d.Before(time.Now()) || d.After(time.Now().Add(2*time.Minute)) {
		t.Fatalf("deadline %v", d)
	}
}

func TestApplyMaxMin(t *testing.T) {
	tests := []struct {
		name    string
		info    dropfile.Info
		max     int
		minutes int
		limited bool
	}{
		{name: "none", max: 0},
		{name: "cap only", max: 10, minutes: 10, limited: true},
		{name: "file only", info: dropfile.Info{MinutesLeft: 60, HasLimit: true}, minutes: 60, limited: true},
		{name: "cap lower", info: dropfile.Info{MinutesLeft: 60, HasLimit: true}, max: 10, minutes: 10, limited: true},
		{name: "file lower", info: dropfile.Info{MinutesLeft: 5, HasLimit: true}, max: 10, minutes: 5, limited: true},
		{name: "zero stays", info: dropfile.Info{HasLimit: true}, max: 30, limited: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, limited := applyMaxMin(tt.info, tt.max)
			if got != tt.minutes || limited != tt.limited {
				t.Fatalf("applyMaxMin = %d, %v", got, limited)
			}
		})
	}
}

func TestSessionWarns(t *testing.T) {
	tests := []struct {
		minutes       int
		five, one     time.Duration
		fiveOn, oneOn bool
	}{
		{minutes: 0},
		{minutes: 1},
		{minutes: 5, one: 4 * time.Minute, oneOn: true},
		{minutes: 6, five: time.Minute, fiveOn: true, one: 5 * time.Minute, oneOn: true},
		{minutes: 10, five: 5 * time.Minute, fiveOn: true, one: 9 * time.Minute, oneOn: true},
	}
	for _, tt := range tests {
		five, one, fiveOn, oneOn := sessionWarns(tt.minutes)
		if five != tt.five || one != tt.one || fiveOn != tt.fiveOn || oneOn != tt.oneOn {
			t.Fatalf("minutes %d: %s %v, %s %v", tt.minutes, five, fiveOn, one, oneOn)
		}
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
	code := realMain(context.Background(), nil, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "-model is required") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRealMainBadTheme(t *testing.T) {
	var errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-theme", "amber"}, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "mono or green") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRealMainNoDropfileNote(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-theme", "mono"}, strings.NewReader("quit\n"), &out, &errOut)
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
	code := realMain(context.Background(),
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
	code := realMain(context.Background(),
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
	code := realMain(context.Background(),
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
	code := realMain(context.Background(),
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
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusOK)
			return
		}
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
	code := realMain(context.Background(),
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
	code := realMain(context.Background(),
		[]string{"-model", "m", "-host", srv.URL, "-theme", "mono"},
		strings.NewReader("hi\nbye\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), msgNoModel) || !strings.Contains(out.String(), "Goodbye.\r\n") {
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
	if !strings.Contains(out.String(), "Your time is up. Returning to the BBS...\r\n") {
		t.Fatalf("out %q", out.String())
	}
	if strings.Contains(out.String(), "> ") {
		t.Fatalf("prompted a caller who is out of time: %q", out.String())
	}
}

func TestRunTimeUpDuringChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"pong\"},\"done\":true}\n")
	}))
	defer srv.Close()

	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()
	// Keep stdin open. Closing it would be a hangup and would cancel
	// the reply, which is not what the session clock does.
	r, w := io.Pipe()
	defer w.Close()
	go func() {
		io.WriteString(w, "hi\n")
	}()
	var out, errOut bytes.Buffer
	err := run(context.Background(), r, &out, &errOut, session{
		client:   client,
		model:    "m",
		theme:    theme.Mono,
		width:    80,
		minutes:  1,
		deadline: time.Now().Add(40 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("run: %v, stderr %q", err, errOut.String())
	}
	text := out.String()
	pong := strings.Index(text, "pong\r\n")
	up := strings.Index(text, "Your time is up. Returning to the BBS...")
	if pong < 0 || up < 0 || pong > up {
		t.Fatalf("out %q stderr %q", text, errOut.String())
	}
	if strings.Contains(text, "not answering") {
		t.Fatalf("a finished reply looked like an Ollama failure: %q", text)
	}
}

func TestIdleWaits(t *testing.T) {
	tests := []struct {
		name   string
		idle   time.Duration
		stop   time.Duration
		warn   time.Duration
		warnOn bool
	}{
		{name: "off", idle: 0},
		{name: "negative", idle: -time.Second},
		{name: "short", idle: 30 * time.Second, stop: 30 * time.Second},
		{name: "under two minutes", idle: 90 * time.Second, stop: 90 * time.Second},
		{name: "two minutes", idle: 2 * time.Minute, stop: 2 * time.Minute, warn: time.Minute, warnOn: true},
		{name: "default", idle: 5 * time.Minute, stop: 5 * time.Minute, warn: 4 * time.Minute, warnOn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stop, warn, warnOn := idleWaits(tt.idle)
			if stop != tt.stop || warn != tt.warn || warnOn != tt.warnOn {
				t.Fatalf("idleWaits(%s) = %s, %s, %v", tt.idle, stop, warn, warnOn)
			}
		})
	}
}

func TestIdleTimeout(t *testing.T) {
	themes := []theme.Theme{theme.Mono, theme.Green}
	for _, th := range themes {
		t.Run(th.Name, func(t *testing.T) {
			r, w := io.Pipe()
			defer w.Close()
			var out bytes.Buffer
			start := time.Now()
			err := run(context.Background(), r, &out, io.Discard, session{
				theme: th,
				width: 80,
				idle:  30 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > time.Second {
				t.Fatalf("idle wait took %s", time.Since(start))
			}
			text := out.String()
			if !strings.Contains(text, "\r\nIdle too long. Goodbye!\r\n") {
				t.Fatalf("out %q", text)
			}
			if !strings.HasPrefix(text, th.Start) || !strings.HasSuffix(text, th.End) {
				t.Fatalf("theme not wrapped around the hangup: %q", text)
			}
			wantEsc := strings.Count(th.Start, "\x1b") + strings.Count(th.End, "\x1b")
			if got := strings.Count(text, "\x1b"); got != wantEsc {
				t.Fatalf("escape count %d, want %d in %q", got, wantEsc, text)
			}
		})
	}
}

func TestIdleDisabled(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), strings.NewReader("quit\n"), &out, io.Discard, session{
		theme: theme.Mono,
		width: 80,
		idle:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Idle too long") {
		t.Fatalf("idle fired while disabled: %q", out.String())
	}
	if !strings.Contains(out.String(), "Goodbye.\r\n") {
		t.Fatalf("out %q", out.String())
	}
}

func TestLineArrivesBeforeIdle(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	go func() {
		io.WriteString(w, "quit\n")
	}()
	var out bytes.Buffer
	err := run(context.Background(), r, &out, io.Discard, session{
		theme: theme.Mono,
		width: 80,
		idle:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Idle too long") || strings.Contains(text, "Still there") {
		t.Fatalf("idle text on a typed quit: %q", text)
	}
	if !strings.Contains(text, "Goodbye.\r\n") {
		t.Fatalf("out %q", text)
	}
}

func TestIdleDoesNotFireDuringReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"pong\"},\"done\":true}\n")
	}))
	defer srv.Close()

	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()
	r, w := io.Pipe()
	defer w.Close()
	// The write blocks until the reader starts, so the line is waiting
	// before the idle timer is armed. The pipe stays open afterward,
	// which is a caller who does not hang up.
	go func() {
		io.WriteString(w, "hello\n")
	}()

	var out bytes.Buffer
	err := run(context.Background(), r, &out, io.Discard, session{
		client: client,
		model:  "m",
		theme:  theme.Mono,
		width:  80,
		idle:   40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	pong := strings.Index(text, "pong\r\n")
	idleAt := strings.Index(text, "Idle too long. Goodbye!")
	if pong < 0 || idleAt < 0 || pong > idleAt {
		t.Fatalf("out %q", text)
	}
}

func TestIdleWarningThenHangup(t *testing.T) {
	idle := 2 * time.Minute
	warnCh := make(chan time.Time)
	stopCh := make(chan time.Time)
	r, w := io.Pipe()
	defer w.Close()

	var got lockedBuffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(context.Background(), r, &got, io.Discard, session{
			theme: theme.Mono,
			width: 80,
			idle:  idle,
			after: func(d time.Duration) <-chan time.Time {
				switch d {
				case idle:
					return stopCh
				case idle - time.Minute:
					return warnCh
				default:
					return make(chan time.Time)
				}
			},
		})
	}()

	waitFor(t, got.String, "> ")
	warnCh <- time.Now()
	waitFor(t, got.String, "\r\nStill there? One minute left.\r\n")
	if strings.Contains(got.String(), "Idle too long") {
		t.Fatalf("cutoff fired with the warning: %q", got.String())
	}
	stopCh <- time.Now()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
	text := got.String()
	if !strings.Contains(text, "\r\nIdle too long. Goodbye!\r\n") {
		t.Fatalf("out %q", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Fatalf("idle text contained an escape: %q", text)
	}
}

func TestRealMainIdleFlag(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	var out, errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-theme", "mono", "-idle", "25ms"}, r, &out, &errOut)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Idle too long. Goodbye!\r\n") {
		t.Fatalf("out %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("mono idle hangup sent ANSI")
	}
}

func TestRealMainIdleNegative(t *testing.T) {
	var errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-idle", "-1s"}, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "-idle must be 0 or positive") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

// lockedBuffer is a bytes.Buffer that run and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSessionTimeUpAtPrompt(t *testing.T) {
	themes := []theme.Theme{theme.Mono, theme.Green}
	for _, th := range themes {
		t.Run(th.Name, func(t *testing.T) {
			r, w := io.Pipe()
			defer w.Close()
			var out bytes.Buffer
			start := time.Now()
			err := run(context.Background(), r, &out, io.Discard, session{
				theme:    th,
				width:    80,
				minutes:  1,
				deadline: time.Now().Add(40 * time.Millisecond),
				idle:     time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > time.Second {
				t.Fatalf("waited %s", time.Since(start))
			}
			text := out.String()
			if !strings.Contains(text, "\r\nYour time is up. Returning to the BBS...\r\n") {
				t.Fatalf("out %q", text)
			}
			if strings.Contains(text, "Idle too long") {
				t.Fatalf("idle won over the session clock: %q", text)
			}
			if !strings.HasPrefix(text, th.Start) || !strings.HasSuffix(text, th.End) {
				t.Fatalf("theme missing around time-up: %q", text)
			}
			wantEsc := strings.Count(th.Start, "\x1b") + strings.Count(th.End, "\x1b")
			if got := strings.Count(text, "\x1b"); got != wantEsc {
				t.Fatalf("escape count %d, want %d", got, wantEsc)
			}
		})
	}
}

func TestIdleSoonerThanSession(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	var out bytes.Buffer
	err := run(context.Background(), r, &out, io.Discard, session{
		theme:    theme.Mono,
		width:    80,
		minutes:  1,
		deadline: time.Now().Add(2 * time.Second),
		idle:     30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Idle too long. Goodbye!") {
		t.Fatalf("out %q", text)
	}
	if strings.Contains(text, "Returning to the BBS") {
		t.Fatalf("session clock won over idle: %q", text)
	}
}

func TestZeroMinutesExitsImmediately(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), strings.NewReader("hi\n"), &out, io.Discard, session{
		theme:    theme.Mono,
		width:    80,
		deadline: sessionDeadline(0, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Your time is up. Returning to the BBS...\r\n") {
		t.Fatalf("out %q", text)
	}
	if strings.Contains(text, "> ") {
		t.Fatalf("prompted a caller with 0 minutes: %q", text)
	}
}

func TestSessionWarnings(t *testing.T) {
	fiveCh := make(chan time.Time)
	oneCh := make(chan time.Time)
	r, w := io.Pipe()
	defer w.Close()

	var got lockedBuffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(context.Background(), r, &got, io.Discard, session{
			theme:    theme.Mono,
			width:    80,
			minutes:  10,
			deadline: time.Now().Add(10 * time.Minute),
			after: func(d time.Duration) <-chan time.Time {
				switch d {
				case 5 * time.Minute:
					return fiveCh
				case 9 * time.Minute:
					return oneCh
				default:
					return make(chan time.Time)
				}
			},
		})
	}()

	waitFor(t, got.String, "> ")
	fiveCh <- time.Now()
	waitFor(t, got.String, "\r\n5 minutes left.\r\n")
	if strings.Contains(got.String(), "1 minute left.") {
		t.Fatalf("1 minute warning came early: %q", got.String())
	}
	oneCh <- time.Now()
	waitFor(t, got.String, "\r\n1 minute left.\r\n")
	if strings.Contains(got.String(), "Returning to the BBS") {
		t.Fatalf("time ran out during the warnings: %q", got.String())
	}
	if strings.Contains(got.String(), "\x1b") {
		t.Fatalf("warnings contained an escape: %q", got.String())
	}
	io.WriteString(w, "quit\n")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestSessionWarningAfterReply(t *testing.T) {
	fiveCh := make(chan time.Time)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"pong\"},\"done\":true}\n")
	}))
	defer srv.Close()
	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()

	r, w := io.Pipe()
	defer w.Close()
	go func() {
		io.WriteString(w, "hello\n")
	}()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fiveCh <- time.Now()
	}()

	var got lockedBuffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(context.Background(), r, &got, io.Discard, session{
			client:   client,
			model:    "m",
			theme:    theme.Mono,
			width:    80,
			minutes:  10,
			deadline: time.Now().Add(10 * time.Minute),
			after: func(d time.Duration) <-chan time.Time {
				if d == 5*time.Minute {
					return fiveCh
				}
				return make(chan time.Time)
			},
		})
	}()

	waitFor(t, got.String, "5 minutes left.")
	text := got.String()
	pong := strings.Index(text, "pong\r\n")
	note := strings.Index(text, "5 minutes left.")
	if pong < 0 || note < 0 || pong > note {
		t.Fatalf("out %q", text)
	}
	w.Close()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestRealMainZeroMinutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "door32.sys")
	body := "2\n0\n38400\nSynchronet\n1\nJosh\nJosh\n10\n0\n1\n1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-theme", "mono", "-idle", "0", "-dropfile", path}, strings.NewReader("hi\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "Your time is up. Returning to the BBS...\r\n") {
		t.Fatalf("out %q", text)
	}
	if strings.Contains(text, "> ") {
		t.Fatalf("prompted: %q", text)
	}
}

func TestEOFAtPrompt(t *testing.T) {
	r, w := io.Pipe()
	w.Close()
	var out bytes.Buffer
	err := run(context.Background(), r, &out, io.Discard, session{
		theme: theme.Mono,
		width: 80,
		idle:  time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Goodbye") || strings.Contains(text, "Idle too long") || strings.Contains(text, "Returning to the BBS") {
		t.Fatalf("printed a farewell to a caller who was gone: %q", text)
	}
}

func TestEOFDuringStreamCancels(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"Hi\"},\"done\":false}\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()

	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()
	r, w := io.Pipe()
	go func() {
		io.WriteString(w, "hello\n")
		<-started
		w.Close()
	}()

	var out, errOut bytes.Buffer
	err := run(context.Background(), r, &out, &errOut, session{
		client: client,
		model:  "m",
		theme:  theme.Mono,
		width:  80,
		idle:   time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v stderr %q", err, errOut.String())
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not see the request context canceled")
	}
	text := out.String()
	if strings.Contains(text, "Goodbye") || strings.Contains(text, "not answering") || strings.Contains(text, "Returning to the BBS") {
		t.Fatalf("out %q stderr %q", text, errOut.String())
	}
}

func TestWriteErrorDuringStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"pong\"},\"done\":true}\n")
	}))
	defer srv.Close()
	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()

	r, w := io.Pipe()
	defer w.Close()
	go func() {
		io.WriteString(w, "hello\n")
	}()

	out := &epipeWriter{}
	var errOut bytes.Buffer
	err := run(context.Background(), r, out, &errOut, session{
		client: client,
		model:  "m",
		theme:  theme.Mono,
		width:  80,
		idle:   time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v stderr %q out %q", err, errOut.String(), out.String())
	}
	if strings.Contains(errOut.String(), "ollama chat") || strings.Contains(out.String(), "not answering") || strings.Contains(out.String(), "Goodbye") {
		t.Fatalf("noisy hangup stderr %q out %q", errOut.String(), out.String())
	}
}

func TestSIGHUPAtPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, w := io.Pipe()
	defer w.Close()
	var got lockedBuffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, r, &got, io.Discard, session{
			theme: theme.Mono,
			width: 80,
			idle:  time.Hour,
		})
	}()
	waitFor(t, got.String, "> ")
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
	text := got.String()
	if strings.Contains(text, "Goodbye") || strings.Contains(text, "Idle too long") || strings.Contains(text, "Returning to the BBS") {
		t.Fatalf("printed a farewell after SIGHUP: %q", text)
	}
}

// epipeWriter accepts the banner, then fails the reply with EPIPE.
type epipeWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (e *epipeWriter) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if bytes.Contains(p, []byte("pong")) {
		return 0, syscall.EPIPE
	}
	return e.buf.Write(p)
}

func (e *epipeWriter) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buf.String()
}

func TestFriendlyAI(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "unreachable", err: fmt.Errorf("dial: %w", ollama.ErrUnreachable), want: msgNapping},
		{name: "model sentinel", err: ollama.ErrModelNotFound, want: msgNoModel},
		{
			name: "404 status",
			err:  &ollama.StatusError{Code: 404, Status: "404 Not Found", Body: "model not found"},
			want: msgNoModel,
		},
		{
			name: "500 status",
			err:  &ollama.StatusError{Code: 500, Status: "500 Internal Server Error", Body: "boom"},
			want: msgGeneric,
		},
		{name: "bad stream", err: fmt.Errorf("decode: %w", ollama.ErrBadStream), want: msgGeneric},
		{name: "stall", err: ollama.ErrStall, want: msgSlow},
		{name: "deadline", err: context.DeadlineExceeded, want: msgSlow},
		{name: "other", err: fmt.Errorf("boom"), want: msgGeneric},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := friendlyAI(tt.err); got != tt.want {
				t.Fatalf("friendlyAI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFriendlyErrorsAndHistory(t *testing.T) {
	var mu sync.Mutex
	var got []ollama.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var req ollama.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		n := len(got)
		got = append(got, req)
		mu.Unlock()
		switch n {
		case 0:
			http.Error(w, "model not found", http.StatusNotFound)
		default:
			fmt.Fprintf(w, "{\"message\":{\"role\":\"assistant\",\"content\":%q},\"done\":true}\n", "ok")
		}
	}))
	defer srv.Close()

	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()
	var out, errOut bytes.Buffer
	err := run(context.Background(), strings.NewReader("one\ntwo\nquit\n"), &out, &errOut, session{
		client: client,
		model:  "missing",
		system: "sys",
		theme:  theme.Mono,
		width:  80,
		idle:   time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, msgNoModel+"\r\n") || !strings.Contains(text, "ok\r\n") {
		t.Fatalf("out %q", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Fatal("friendly text contained an escape")
	}
	if !strings.Contains(errOut.String(), "404") {
		t.Fatalf("stderr %q", errOut.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("requests %d", len(got))
	}
	for _, m := range got[1].Messages {
		if m.Content == "one" {
			t.Fatalf("failed user line was kept: %+v", got[1].Messages)
		}
	}
	last := got[1].Messages[len(got[1].Messages)-1]
	if last.Role != ollama.RoleUser || last.Content != "two" {
		t.Fatalf("second request ended with %+v", last)
	}
}

func TestFriendlyServerErrors(t *testing.T) {
	tests := []struct {
		name string
		body func(http.ResponseWriter)
		want string
	}{
		{
			name: "500",
			body: func(w http.ResponseWriter) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			want: msgGeneric,
		},
		{
			name: "bad json",
			body: func(w http.ResponseWriter) {
				io.WriteString(w, "not-json\n")
			},
			want: msgGeneric,
		},
		{
			name: "partial then bad json",
			body: func(w http.ResponseWriter) {
				io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"done\":false}\n")
				io.WriteString(w, "not-json\n")
			},
			want: msgGeneric,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/chat" {
					w.WriteHeader(http.StatusOK)
					return
				}
				tt.body(w)
			}))
			defer srv.Close()
			client := ollama.New(srv.URL)
			client.HTTP = srv.Client()
			var out, errOut bytes.Buffer
			err := run(context.Background(), strings.NewReader("hi\nquit\n"), &out, &errOut, session{
				client: client,
				model:  "m",
				theme:  theme.Green,
				width:  80,
				idle:   time.Hour,
			})
			if err != nil {
				t.Fatalf("run: %v stderr %q", err, errOut.String())
			}
			text := out.String()
			msgAt := strings.Index(text, tt.want+"\r\n")
			if msgAt < 0 {
				t.Fatalf("out %q stderr %q", text, errOut.String())
			}
			if strings.Contains(tt.name, "partial") {
				hello := strings.Index(text, "Hello\r\n")
				if hello < 0 || hello > msgAt {
					t.Fatalf("partial line was not finished before the message: %q", text)
				}
			}
			if !strings.HasPrefix(text, theme.Green.Start) || !strings.HasSuffix(text, theme.Green.End) {
				t.Fatalf("theme missing: %q", text)
			}
			if !strings.Contains(text, "Goodbye.\r\n") {
				t.Fatalf("did not return to the prompt: %q", text)
			}
		})
	}
}

func TestStallTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Headers go out, then nothing. The client is waiting for a
		// token. Canceling the request context should wake both sides.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := ollama.New(srv.URL)
	client.HTTP = srv.Client()
	var out, errOut bytes.Buffer
	start := time.Now()
	err := run(context.Background(), strings.NewReader("hi\nquit\n"), &out, &errOut, session{
		client:  client,
		model:   "m",
		theme:   theme.Mono,
		width:   80,
		idle:    time.Hour,
		timeout: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("stall took %s", time.Since(start))
	}
	if !strings.Contains(out.String(), msgSlow+"\r\n") || !strings.Contains(out.String(), "Goodbye.\r\n") {
		t.Fatalf("out %q stderr %q", out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "ollama") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestClosedListenerNapping(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	var out, errOut bytes.Buffer
	code := realMain(context.Background(),
		[]string{"-model", "m", "-host", "http://" + addr, "-theme", "mono", "-idle", "0"},
		strings.NewReader("hi\nquit\n"),
		&out, &errOut,
	)
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	text := out.String()
	if strings.Count(text, msgNapping) < 1 {
		t.Fatalf("out %q stderr %q", text, errOut.String())
	}
	if !strings.Contains(text, "Goodbye.\r\n") {
		t.Fatalf("did not return to the prompt: %q", text)
	}
	if !strings.Contains(errOut.String(), "unreachable") && !strings.Contains(errOut.String(), "connection") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestRealMainMaxMinNegative(t *testing.T) {
	var errOut bytes.Buffer
	code := realMain(context.Background(), []string{"-model", "m", "-maxmin", "-1"}, strings.NewReader(""), io.Discard, &errOut)
	if code != 2 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "-maxmin must be 0 or positive") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func waitFor(t *testing.T, got func() string, sub string) {
	t.Helper()
	dead := time.Now().Add(2 * time.Second)
	for time.Now().Before(dead) {
		if strings.Contains(got(), sub) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", sub, got())
}
