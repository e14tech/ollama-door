// Command ollama-door is a BBS door: a program the bulletin board starts
// for one caller. The BBS already accepted telnet or SSH. This process
// reads what the caller types on stdin and writes the caller's screen
// on stdout.
//
// A BBS passes -dropfile with the DOOR32.SYS or DOOR.SYS it just wrote.
// That file supplies the caller's name, ANSI flag, and minutes left.
// -theme and -width override those choices for a local test.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/e14tech/ollama-door/internal/ascii"
	"github.com/e14tech/ollama-door/internal/dropfile"
	"github.com/e14tech/ollama-door/internal/ollama"
	"github.com/e14tech/ollama-door/internal/theme"
	"github.com/e14tech/ollama-door/internal/wrap"
)

// defaultSystem is used when the caller does not pass -system.
const defaultSystem = "You are the friendly AI sysop of a retro BBS. Reply in plain ASCII only, no markdown, emoji, or special characters. Keep answers short, under 6 lines."

// historyLimit is how many messages we send, counting the system message.
// Older turns are dropped so a long call does not grow the request forever.
const historyLimit = 20

// errTimeUp ends the call because the drop file's minutes ran out.
var errTimeUp = errors.New("time is up")

// session is one caller's visit.
type session struct {
	client   *ollama.Client
	model    string
	system   string
	width    int
	theme    theme.Theme
	alias    string
	minutes  int
	deadline time.Time
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func realMain(args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("ollama-door", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "Usage: ollama-door -model NAME -dropfile PATH\n")
		fs.PrintDefaults()
	}

	host := fs.String("host", "http://localhost:11434", "Ollama server base URL")
	model := fs.String("model", "", "Ollama model name (required)")
	dropPath := fs.String("dropfile", "", "path to DOOR32.SYS or DOOR.SYS (how a BBS launches the door)")
	width := fs.Int("width", 80, "optional column override for a local test; classic terminals use 40 or 80")
	themeName := fs.String("theme", "mono", "optional theme override: mono or green (default: from the drop file)")
	system := fs.String("system", defaultSystem, "system prompt sent as the first chat message")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *model == "" {
		fmt.Fprintf(errOut, "ollama-door: -model is required\r\n")
		fs.Usage()
		return 2
	}
	if *width < 1 {
		fmt.Fprintf(errOut, "ollama-door: -width must be at least 1\r\n")
		fs.Usage()
		return 2
	}

	themeSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "theme" {
			themeSet = true
		}
	})
	// Reject a bad -theme before any drop-file note, so a typo exits 2
	// without looking like the door started.
	if themeSet {
		if _, err := theme.Parse(*themeName); err != nil {
			fmt.Fprintf(errOut, "ollama-door: -theme must be mono or green\r\n")
			fs.Usage()
			return 2
		}
	}

	info, dropOK := loadDropfile(*dropPath, errOut)
	th, err := pickTheme(themeSet, *themeName, info.ANSI)
	if err != nil {
		fmt.Fprintf(errOut, "ollama-door: -theme must be mono or green\r\n")
		fs.Usage()
		return 2
	}

	alias := strings.TrimSpace(ascii.Sanitize(info.Alias))
	client := ollama.New(*host)
	fmt.Fprintf(errOut, "ollama-door: model=%s host=%s width=%d theme=%s\r\n",
		*model, client.BaseURL, *width, th.Name)
	if dropOK {
		fmt.Fprintf(errOut, "ollama-door: caller=%s minutes=%d ansi=%t\r\n",
			alias, info.MinutesLeft, info.ANSI)
	}

	s := session{
		client:   client,
		model:    *model,
		system:   withCaller(*system, alias),
		width:    *width,
		theme:    th,
		alias:    alias,
		minutes:  info.MinutesLeft,
		deadline: deadlineFromMinutes(info.MinutesLeft),
	}
	if err := run(context.Background(), in, out, errOut, s); err != nil {
		fmt.Fprintf(errOut, "ollama-door: %v\r\n", err)
		return 1
	}
	return 0
}

// loadDropfile reads path. A missing -dropfile is normal for a quick
// local try and is reported on stderr. A bad path is also reported, and
// the door still starts with empty Info (mono, no time limit, no name).
func loadDropfile(path string, errOut io.Writer) (dropfile.Info, bool) {
	if path == "" {
		fmt.Fprintf(errOut, "ollama-door: no -dropfile given. A BBS should pass -dropfile (DOOR32.SYS or DOOR.SYS).\r\n")
		return dropfile.Info{}, false
	}
	info, err := dropfile.Parse(path)
	if err != nil {
		fmt.Fprintf(errOut, "ollama-door: dropfile: %v\r\n", err)
		fmt.Fprintf(errOut, "ollama-door: continuing without drop file data\r\n")
		return dropfile.Info{}, false
	}
	return info, true
}

// pickTheme chooses the screen theme.
// An explicit -theme flag wins. Otherwise an ANSI caller gets green
// and an ASCII caller (or no drop file) gets mono.
func pickTheme(themeSet bool, requested string, ansi bool) (theme.Theme, error) {
	if themeSet {
		return theme.Parse(requested)
	}
	if ansi {
		return theme.Green, nil
	}
	return theme.Mono, nil
}

// withCaller adds the caller's name to the system prompt when we know it.
func withCaller(system, alias string) string {
	system = strings.TrimSpace(system)
	if alias == "" {
		return system
	}
	note := "The caller's name is " + alias + "."
	if system == "" {
		return note
	}
	return system + " " + note
}

// deadlineFromMinutes returns when the call should end.
// Zero (a missing line, or a drop file that did not set a budget) means
// no cap. A positive value is the minutes field from the drop file.
func deadlineFromMinutes(minutes int) time.Time {
	if minutes <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(minutes) * time.Minute)
}

func pastDeadline(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}

func run(ctx context.Context, in io.Reader, out, errOut io.Writer, s session) error {
	if _, err := io.WriteString(out, s.theme.Start); err != nil {
		return err
	}
	defer io.WriteString(out, s.theme.End)

	if err := writeBanner(out, s.alias, s.minutes, s.width); err != nil {
		return err
	}
	if pastDeadline(s.deadline) {
		_, err := io.WriteString(out, "Your time is up. Goodbye.\r\n")
		return err
	}

	var history []ollama.Message
	if s.system != "" {
		history = append(history, ollama.Message{Role: ollama.RoleSystem, Content: s.system})
	}

	prompt := func() error {
		_, err := io.WriteString(out, "> ")
		return err
	}
	if err := prompt(); err != nil {
		return err
	}

	sc := bufio.NewScanner(in)
	for sc.Scan() {
		if pastDeadline(s.deadline) {
			_, err := io.WriteString(out, "Your time is up. Goodbye.\r\n")
			return err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			if err := prompt(); err != nil {
				return err
			}
			continue
		}
		if isQuit(line) {
			_, err := io.WriteString(out, "Goodbye.\r\n")
			return err
		}
		line = strings.TrimSpace(ascii.Sanitize(line))
		if line == "" {
			if err := prompt(); err != nil {
				return err
			}
			continue
		}
		var err error
		history, err = s.takeTurn(ctx, out, errOut, history, line)
		if errors.Is(err, errTimeUp) {
			_, werr := io.WriteString(out, "Your time is up. Goodbye.\r\n")
			return werr
		}
		if err != nil {
			return err
		}
		if err := prompt(); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

func writeBanner(out io.Writer, alias string, minutes, width int) error {
	var b strings.Builder
	b.WriteString("OLLAMA DOOR\n")
	if alias != "" {
		b.WriteString("Hello, " + alias + ".\n")
	}
	if minutes > 0 {
		b.WriteString(minutesLine(minutes) + "\n")
	}
	b.WriteString("Type quit, /q, or bye to hang up.")
	if _, err := io.WriteString(out, wrap.Wrap(b.String(), width)); err != nil {
		return err
	}
	_, err := io.WriteString(out, "\r\n")
	return err
}

func minutesLine(n int) string {
	if n == 1 {
		return "1 minute left."
	}
	return fmt.Sprintf("%d minutes left.", n)
}

func isQuit(line string) bool {
	switch strings.ToLower(line) {
	case "quit", "/q", "bye":
		return true
	default:
		return false
	}
}

// takeTurn sends one caller line and writes the reply as it arrives.
// Each chunk is sanitized, then wrapped. A finished line goes out as
// soon as the next word does not fit. Flush writes whatever is left.
func (s session) takeTurn(ctx context.Context, out, errOut io.Writer, history []ollama.Message, line string) ([]ollama.Message, error) {
	if pastDeadline(s.deadline) {
		return history, errTimeUp
	}
	history = append(history, ollama.Message{Role: ollama.RoleUser, Content: line})
	history = trimHistory(history)

	reqCtx := ctx
	if !s.deadline.IsZero() {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithDeadline(ctx, s.deadline)
		defer cancel()
	}

	lw := wrap.NewLineWriter(out, s.width)
	var spoken strings.Builder
	_, err := s.client.Chat(reqCtx, ollama.ChatRequest{
		Model:    s.model,
		Messages: history,
		Stream:   true,
	}, func(chunk ollama.ChatResponse) error {
		// Each chunk is a whole piece of text, so Sanitize can run on it alone.
		text := ascii.Sanitize(chunk.Message.Content)
		spoken.WriteString(text)
		return lw.WriteString(text)
	})
	flushErr := lw.Flush()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return history, errTimeUp
		}
		// The model did not finish, so this turn is forgotten.
		history = history[:len(history)-1]
		fmt.Fprintf(errOut, "ollama-door: %v\r\n", err)
		if _, werr := io.WriteString(out, "Sorry, the sysop computer is not answering right now.\r\n"); werr != nil {
			return history, werr
		}
		return history, nil
	}
	if flushErr != nil {
		return history, flushErr
	}
	history = append(history, ollama.Message{
		Role:    ollama.RoleAssistant,
		Content: spoken.String(),
	})
	return trimHistory(history), nil
}

// trimHistory keeps the newest messages, plus a leading system message.
func trimHistory(h []ollama.Message) []ollama.Message {
	if len(h) <= historyLimit {
		return h
	}
	if len(h) > 0 && h[0].Role == ollama.RoleSystem {
		keep := historyLimit - 1
		if keep < 1 {
			return append([]ollama.Message(nil), h[0])
		}
		tail := h[len(h)-keep:]
		out := make([]ollama.Message, 0, historyLimit)
		out = append(out, h[0])
		out = append(out, tail...)
		return out
	}
	tail := h[len(h)-historyLimit:]
	out := make([]ollama.Message, len(tail))
	copy(out, tail)
	return out
}
