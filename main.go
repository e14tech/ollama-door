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
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/e14tech/ollama-door/internal/ascii"
	"github.com/e14tech/ollama-door/internal/config"
	"github.com/e14tech/ollama-door/internal/dropfile"
	"github.com/e14tech/ollama-door/internal/ollama"
	"github.com/e14tech/ollama-door/internal/theme"
	"github.com/e14tech/ollama-door/internal/wrap"
)

// defaultSystem is used when the caller does not pass -system and the
// config file does not set one. The model is an assistant, not the sysop.
const defaultSystem = "You are a helpful AI assistant on a retro BBS. You are not the sysop and not a person; never invent a name or backstory. Reply in plain ASCII only, no markdown, emoji, or special characters. Keep answers short, under 6 lines, even if asked for more."

// version is printed by -version. Override it at build time:
//
//	CGO_ENABLED=0 go build -ldflags "-X main.version=1.0.1" -o ollama-door .
var version = "1.0.1"

// historyLimit is how many messages we send, counting the system message.
// Older turns are dropped so a long call does not grow the request forever.
const historyLimit = 20

// errTimeUp ends the call because the drop file's minutes ran out.
var errTimeUp = errors.New("time is up")

// errHangup means the carrier is gone: stdin closed, a read failed,
// stdout broke, or the process was signaled. The caller is not there
// to read a goodbye, so run turns this into a quiet status 0.
var errHangup = errors.New("caller hung up")

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
	// idle is how long to wait at the prompt with no line typed.
	// Zero disables the idle hangup.
	idle time.Duration
	// timeout is how long a reply may sit with no new text.
	// Zero disables that stall check.
	timeout time.Duration
	// down is set when the startup ping could not reach Ollama.
	down bool
	// after starts a timer. Nil means time.After. Tests pass a fake
	// so they can fire the warning without waiting minutes.
	after func(time.Duration) <-chan time.Time
	// log is the sysop log. Nil means do not log.
	log *callLog
}

func main() {
	// Interrupt, SIGTERM, and SIGHUP all mean the carrier or the BBS
	// is going away. NotifyContext cancels ctx when one of them arrives.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	code := realMain(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func realMain(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("ollama-door", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "Usage: ollama-door -model NAME -dropfile PATH\n")
		fs.PrintDefaults()
	}

	showVersion := fs.Bool("version", false, "print the version and exit")
	configPath := fs.String("config", "", "optional JSON config file")
	host := fs.String("host", "http://localhost:11434", "Ollama server base URL")
	model := fs.String("model", "", "Ollama model name (required unless set in -config)")
	dropPath := fs.String("dropfile", "", "path to DOOR32.SYS or DOOR.SYS (how a BBS launches the door)")
	width := fs.Int("width", 80, "optional column override for a local test; classic terminals use 40 or 80")
	themeName := fs.String("theme", "mono", "optional theme override: mono or green (default: from the drop file)")
	idle := fs.Duration("idle", 5*time.Minute, "hang up if the caller types nothing for this long; 0 disables")
	timeout := fs.Duration("timeout", 60*time.Second, "cancel a reply that makes no progress for this long; 0 disables")
	maxMin := fs.Int("maxmin", 0, "cap session minutes for a sysop; 0 uses the drop file only")
	system := fs.String("system", defaultSystem, "system prompt sent as the first chat message")
	logPath := fs.String("log", "", "log file path; empty means no log file. %n and {node} become the node number")
	logChat := fs.Bool("logchat", false, "log chat text when -log or -verbose is on")
	verbose := fs.Bool("verbose", false, "send log lines to stderr for local debugging")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(out, "ollama-door %s\n", version)
		return 0
	}

	// flag.Visit lists only flags the operator typed. That is how a
	// config file can fill the gaps without blocking an explicit flag.
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	opt := defaultOptions()
	themeFromConfig := false
	if *configPath != "" {
		file, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(errOut, "ollama-door: config: %v\r\n", err)
			return 2
		}
		var errApply error
		opt, themeFromConfig, errApply = applyConfig(opt, file)
		if errApply != nil {
			fmt.Fprintf(errOut, "ollama-door: config: %v\r\n", errApply)
			return 2
		}
	}
	opt = applyFlags(opt, visited, flagOptions{
		host: *host, model: *model, system: *system, theme: *themeName,
		width: *width, idle: *idle, timeout: *timeout, maxMin: *maxMin,
		logPath: *logPath, logChat: *logChat, verbose: *verbose,
	})

	if opt.model == "" {
		fmt.Fprintf(errOut, "ollama-door: -model is required\r\n")
		fs.Usage()
		return 2
	}
	if opt.width < 1 {
		fmt.Fprintf(errOut, "ollama-door: -width must be at least 1\r\n")
		fs.Usage()
		return 2
	}
	if opt.idle < 0 {
		fmt.Fprintf(errOut, "ollama-door: -idle must be 0 or positive\r\n")
		fs.Usage()
		return 2
	}
	if opt.timeout < 0 {
		fmt.Fprintf(errOut, "ollama-door: -timeout must be 0 or positive\r\n")
		fs.Usage()
		return 2
	}
	if opt.maxMin < 0 {
		fmt.Fprintf(errOut, "ollama-door: -maxmin must be 0 or positive\r\n")
		fs.Usage()
		return 2
	}
	// Reject a bad theme before any drop-file note, so a typo exits 2
	// without looking like the door started.
	if visited["theme"] || themeFromConfig {
		if _, err := theme.Parse(opt.theme); err != nil {
			fmt.Fprintf(errOut, "ollama-door: -theme must be mono or green\r\n")
			fs.Usage()
			return 2
		}
	}

	info, _ := loadDropfile(*dropPath, errOut)
	th, err := pickTheme(visited["theme"] || themeFromConfig, opt.theme, info.ANSI)
	if err != nil {
		fmt.Fprintf(errOut, "ollama-door: -theme must be mono or green\r\n")
		fs.Usage()
		return 2
	}

	alias := strings.TrimSpace(ascii.Sanitize(info.Alias))
	client := ollama.New(opt.host)
	minutes, limited := applyMaxMin(info, opt.maxMin)
	lg, err := openLog(expandLogPath(opt.logPath, info.Node), info.Node, alias, opt.logChat, opt.verbose, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "ollama-door: log: %v\r\n", err)
		lg, _ = openLog("", info.Node, alias, opt.logChat, opt.verbose, errOut)
	}
	if lg != nil {
		defer lg.close()
	}

	s := session{
		client:   client,
		model:    opt.model,
		system:   withCaller(opt.system, alias),
		width:    opt.width,
		theme:    th,
		alias:    alias,
		minutes:  minutes,
		deadline: sessionDeadline(minutes, limited),
		idle:     opt.idle,
		timeout:  opt.timeout,
		log:      lg,
	}
	if err := client.Ping(ctx); errors.Is(err, ollama.ErrUnreachable) {
		// The caller sees the napping line. The dial error stays in the
		// log, which is quiet unless -log or -verbose is set.
		if lg != nil {
			lg.aiError(err)
		}
		s.down = true
	}
	if err := run(ctx, in, out, errOut, s); err != nil {
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

// applyMaxMin turns a drop-file limit and the -maxmin cap into one budget.
// maxMin 0 means "no cap". A drop file with HasLimit and 0 minutes stays
// at 0, which is already out of time. No drop-file limit plus a positive
// cap uses the cap. Neither one means there is no session clock.
func applyMaxMin(info dropfile.Info, maxMin int) (minutes int, limited bool) {
	if info.HasLimit {
		minutes = info.MinutesLeft
		if maxMin > 0 && minutes > maxMin {
			minutes = maxMin
		}
		return minutes, true
	}
	if maxMin > 0 {
		return maxMin, true
	}
	return 0, false
}

// sessionDeadline is when the call must end.
// limited false returns the zero time, which means "no deadline".
// A limit of 0 returns the current time, so the context is already done.
func sessionDeadline(minutes int, limited bool) time.Time {
	if !limited {
		return time.Time{}
	}
	if minutes < 0 {
		minutes = 0
	}
	return time.Now().Add(time.Duration(minutes) * time.Minute)
}

// sessionContext is the clock for one visit.
// A zero deadline is "no limit", so the context is only canceled when
// the parent is. A real deadline becomes context.WithDeadline.
func sessionContext(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if deadline.IsZero() {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, deadline)
}

func sessionOver(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func pastDeadline(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}

// sessionWarns says when to mention 5 minutes left and 1 minute left.
// A visit that starts at or below a mark does not warn for that mark,
// because the moment has already passed.
func sessionWarns(minutes int) (five, one time.Duration, fiveOn, oneOn bool) {
	if minutes > 5 {
		five = time.Duration(minutes-5) * time.Minute
		fiveOn = true
	}
	if minutes > 1 {
		one = time.Duration(minutes-1) * time.Minute
		oneOn = true
	}
	return five, one, fiveOn, oneOn
}

func run(ctx context.Context, in io.Reader, out, errOut io.Writer, s session) error {
	// reason is what the log records when the call ends. Hangup is the
	// default because most early returns are a dropped carrier.
	reason := "hangup"
	defer func() {
		if s.log != nil {
			s.log.end(reason)
		}
	}()
	if s.log != nil {
		s.log.event("start", "model", s.model, "theme", s.theme.Name, "width", s.width)
	}
	timeUp := func(atPrompt bool) error {
		reason = "timeup"
		return hangOrErr(writeTimeUp(out, atPrompt))
	}

	// hangCtx is the carrier. It is canceled by SIGHUP, SIGINT, SIGTERM
	// (the parent ctx), by stdin closing, or by the first stdout error.
	// Chat uses this context, so a hangup aborts the HTTP request.
	// The session clock is separate: it does not cancel hangCtx, and
	// canceling hangCtx does not look like the minutes ran out.
	hangCtx, hangCancel := context.WithCancel(ctx)
	defer hangCancel()
	ew := newErrWriter(out, hangCancel)
	out = ew

	if _, err := io.WriteString(out, s.theme.Start); err != nil {
		return nil
	}
	defer io.WriteString(out, s.theme.End)

	if err := writeBanner(out, s.alias, s.minutes, s.width); err != nil {
		return nil
	}
	sessCtx, sessCancel := sessionContext(context.WithoutCancel(ctx), s.deadline)
	defer sessCancel()
	if sessionOver(sessCtx) {
		return timeUp(false)
	}
	if s.down {
		// The server was down at startup. Say so, then still offer the prompt.
		if err := writeNotice(out, friendlyAI(ollama.ErrUnreachable)); err != nil {
			return nil
		}
	}

	var history []ollama.Message
	if s.system != "" {
		history = append(history, ollama.Message{Role: ollama.RoleSystem, Content: s.system})
	}

	// readLines cancels hangCtx when stdin ends, so a reply that is
	// streaming stops even though this loop is inside takeTurn.
	lines := readLines(hangCtx, hangCancel, in)

	prompt := func() error {
		_, err := io.WriteString(out, "> ")
		return err
	}
	if err := prompt(); err != nil {
		return nil
	}
	// Idle timers are replaced after every line. Session warnings are
	// armed once, from the start of the call, and stay in this select.
	// Neither set is read while takeTurn is streaming.
	stop, warn := s.armIdle()
	left5, left1 := s.armSession()

	for {
		select {
		case <-hangCtx.Done():
			// The caller is gone. Do not print a goodbye.
			return nil

		case <-sessCtx.Done():
			if hangCtx.Err() != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			return nil

		case ev, ok := <-lines:
			// A line that already arrived was typed before the carrier
			// dropped. Honor it. EOF and read errors are the hangup.
			if !ok || ev.err != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			line := strings.TrimSpace(ev.text)
			if line == "" {
				if err := prompt(); err != nil {
					return nil
				}
				stop, warn = s.armIdle()
				continue
			}
			if isQuit(line) {
				reason = "quit"
				_, err := io.WriteString(out, "Goodbye.\r\n")
				return hangOrErr(err)
			}
			line = strings.TrimSpace(ascii.Sanitize(line))
			if line == "" {
				if err := prompt(); err != nil {
					return nil
				}
				stop, warn = s.armIdle()
				continue
			}
			var err error
			history, err = s.takeTurn(hangCtx, out, errOut, history, line)
			// Hangup cancels the request. The session clock does not.
			if errors.Is(err, errHangup) || hangCtx.Err() != nil {
				return nil
			}
			// A reply in progress is not cut off by the session clock.
			// When it returns, the minutes may already have run out.
			if errors.Is(err, errTimeUp) || sessionOver(sessCtx) {
				return timeUp(false)
			}
			if err != nil {
				reason = "error"
				return err
			}
			if err := prompt(); err != nil {
				return nil
			}
			stop, warn = s.armIdle()

		case <-left1:
			left1 = nil
			if hangCtx.Err() != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			if err := writeNotice(out, "1 minute left."); err != nil {
				return nil
			}
			if err := prompt(); err != nil {
				return nil
			}

		case <-left5:
			left5 = nil
			if hangCtx.Err() != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			if err := writeNotice(out, "5 minutes left."); err != nil {
				return nil
			}
			if err := prompt(); err != nil {
				return nil
			}

		case <-warn:
			// One idle warning per wait. The stop timer keeps running.
			warn = nil
			if hangCtx.Err() != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			if err := writeNotice(out, "Still there? One minute left."); err != nil {
				return nil
			}
			if err := prompt(); err != nil {
				return nil
			}

		case <-stop:
			if hangCtx.Err() != nil {
				return nil
			}
			if sessionOver(sessCtx) {
				return timeUp(true)
			}
			reason = "idle"
			_, err := io.WriteString(out, "\r\nIdle too long. Goodbye!\r\n")
			return hangOrErr(err)
		}
	}
}

// writeTimeUp tells the caller the BBS clock has run out.
// atPrompt is true when "> " is still sitting on the current row.
func writeTimeUp(out io.Writer, atPrompt bool) error {
	text := "Your time is up. Returning to the BBS...\r\n"
	if atPrompt {
		text = "\r\n" + text
	}
	_, err := io.WriteString(out, text)
	return err
}

// writeNotice prints one status line under the prompt and leaves the
// cursor ready for prompt() to draw "> " again.
func writeNotice(out io.Writer, text string) error {
	_, err := io.WriteString(out, "\r\n"+text+"\r\n")
	return err
}

// lineEvent is one read from the caller.
type lineEvent struct {
	text string
	err  error
}

// readLines scans r on its own goroutine. Scan blocks, so the prompt
// loop cannot select on it directly. Each finished line is sent on the
// returned channel.
//
// The goroutine exits in one of three ways:
//
//  1. Scan returns. That is EOF or a read error. defer cancel() runs
//     first, which aborts a streaming reply, and then the channel is
//     closed. A read error is sent if the prompt loop is still receiving.
//  2. It is blocked sending a line and ctx is canceled. The select takes
//     ctx.Done() and returns, so shutdown does not wait on that send.
//  3. Scan is blocked inside Read. A context cannot interrupt that call.
//     Closing stdin unblocks it. The BBS does that when the carrier drops.
//     Exiting the process does too.
//
// cancel may run more than once. run also cancels on the way out, which
// wakes a goroutine stuck on the send in case 2.
func readLines(ctx context.Context, cancel context.CancelFunc, r io.Reader) <-chan lineEvent {
	ch := make(chan lineEvent)
	go func() {
		defer close(ch)
		defer cancel()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ev := lineEvent{text: sc.Text()}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
		if err := sc.Err(); err != nil {
			// Cancel before the send. takeTurn may be blocked in Chat
			// and will not be reading this channel.
			cancel()
			select {
			case <-ctx.Done():
			case ch <- lineEvent{err: err}:
			}
		}
	}()
	return ch
}

// hangOrErr turns a carrier failure into a nil error so the process
// exits 0. Any other error is returned as-is.
func hangOrErr(err error) error {
	if err == nil || errors.Is(err, errHangup) || errors.Is(err, context.Canceled) || isCarrier(err) {
		return nil
	}
	return err
}

func isCarrier(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)
}

// errWriter remembers the first Write error and keeps returning it.
// A broken stdout is a hangup, so stop runs once and cancels the chat.
type errWriter struct {
	w    io.Writer
	stop func()

	mu  sync.Mutex
	err error
}

func newErrWriter(w io.Writer, stop func()) *errWriter {
	return &errWriter{w: w, stop: stop}
}

func (e *errWriter) Write(p []byte) (int, error) {
	e.mu.Lock()
	if e.err != nil {
		err := e.err
		e.mu.Unlock()
		return 0, err
	}
	n, err := e.w.Write(p)
	failed := err != nil && e.err == nil
	if failed {
		e.err = err
	}
	stop := e.stop
	e.mu.Unlock()
	if failed && stop != nil {
		stop()
	}
	return n, err
}

func (e *errWriter) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

func writerFailed(w io.Writer) bool {
	ew, ok := w.(*errWriter)
	return ok && ew.Err() != nil
}

// idleWaits is how long to wait for a line, and when to warn.
// A limit under two minutes has no warning, so the warning is not the
// whole wait. Zero or negative means the idle hangup is off.
func idleWaits(idle time.Duration) (stop, warn time.Duration, warnOn bool) {
	if idle <= 0 {
		return 0, 0, false
	}
	if idle >= 2*time.Minute {
		return idle, idle - time.Minute, true
	}
	return idle, 0, false
}

// armIdle starts the timers for one wait at the prompt.
// The channels from the previous wait are dropped, so a reply that ran
// long does not use up the idle budget. time.After is enough here: a
// door arms the timer once per line, not thousands of times a second.
func (s session) armIdle() (stop, warn <-chan time.Time) {
	stopFor, warnFor, warnOn := idleWaits(s.idle)
	if stopFor <= 0 {
		return nil, nil
	}
	stop = s.wakeAfter(stopFor)
	if warnOn {
		warn = s.wakeAfter(warnFor)
	}
	return stop, warn
}

// armSession starts the 5-minute and 1-minute warnings once per call.
func (s session) armSession() (five, one <-chan time.Time) {
	fiveFor, oneFor, fiveOn, oneOn := sessionWarns(s.minutes)
	if fiveOn {
		five = s.wakeAfter(fiveFor)
	}
	if oneOn {
		one = s.wakeAfter(oneFor)
	}
	return five, one
}

func (s session) wakeAfter(d time.Duration) <-chan time.Time {
	if s.after != nil {
		return s.after(d)
	}
	return time.After(d)
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

const (
	msgNapping = "The AI is napping right now. Try again in a bit."
	msgNoModel = "The AI model isn't installed. Please tell the sysop."
	msgGeneric = "Something went wrong talking to the AI. Try again."
	msgSlow    = "The AI is taking too long. Try again."
)

// friendlyAI maps an Ollama error to one short line for the caller.
// errors.Is walks the %w chain. errors.As pulls out a *StatusError
// when the chain holds one, which is how a 404 is recognized even if
// a future wrapper forgets to unwrap ErrModelNotFound.
func friendlyAI(err error) string {
	var status *ollama.StatusError
	switch {
	case errors.Is(err, ollama.ErrUnreachable):
		return msgNapping
	case errors.Is(err, ollama.ErrModelNotFound):
		return msgNoModel
	case errors.As(err, &status) && status.Code == 404:
		return msgNoModel
	case errors.Is(err, ollama.ErrStall), errors.Is(err, context.DeadlineExceeded):
		return msgSlow
	default:
		return msgGeneric
	}
}

// takeTurn sends one caller line and writes the reply as it arrives.
// Each chunk is sanitized, then wrapped. A finished line goes out as
// soon as the next word does not fit. Flush writes whatever is left.
func (s session) takeTurn(ctx context.Context, out, errOut io.Writer, history []ollama.Message, line string) ([]ollama.Message, error) {
	if pastDeadline(s.deadline) {
		return history, errTimeUp
	}
	// The user line is part of this request so the model can answer it.
	// If the answer never arrives, we drop the line again. Otherwise the
	// next turn would be two user messages in a row. A fragment that
	// already reached the screen stays on the screen; it is not stored.
	history = append(history, ollama.Message{Role: ollama.RoleUser, Content: line})
	history = trimHistory(history)

	// ctx is the hangup context. The session deadline is not a parent of
	// reqCtx, so the minutes running out do not cancel this request.
	// A stall timer is a child cancel with ErrStall as the cause.
	reqCtx, stopReq := context.WithCancelCause(ctx)
	defer stopReq(context.Canceled)
	var stall *time.Timer
	if s.timeout > 0 {
		stall = time.AfterFunc(s.timeout, func() {
			stopReq(ollama.ErrStall)
		})
		defer stall.Stop()
	}

	lw := wrap.NewLineWriter(out, s.width)
	var spoken strings.Builder
	started := time.Now()
	_, err := s.client.Chat(reqCtx, ollama.ChatRequest{
		Model:    s.model,
		Messages: history,
		Stream:   true,
	}, func(chunk ollama.ChatResponse) error {
		if stall != nil {
			stall.Reset(s.timeout)
		}
		// Each chunk is a whole piece of text, so Sanitize can run on it alone.
		text := ascii.Sanitize(chunk.Message.Content)
		spoken.WriteString(text)
		return lw.WriteString(text)
	})
	// End a half-written line before any apology, so the message
	// starts on its own row.
	flushErr := lw.Flush()
	if writerFailed(out) || errors.Is(err, context.Canceled) || isCarrier(err) {
		return history, errHangup
	}
	elapsed := time.Since(started)
	if err != nil {
		history = history[:len(history)-1]
		// The caller already sees friendlyAI. The raw error stays in the
		// log, which is discarded unless -log or -verbose is on.
		if s.log != nil {
			s.log.aiError(err)
			s.log.turn(elapsed, len(line), spoken.Len(), line, spoken.String())
		}
		if _, werr := io.WriteString(out, friendlyAI(err)+"\r\n"); werr != nil {
			return history, errHangup
		}
		return history, nil
	}
	if flushErr != nil {
		return history, flushErr
	}
	if s.log != nil {
		s.log.turn(elapsed, len(line), spoken.Len(), line, spoken.String())
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
