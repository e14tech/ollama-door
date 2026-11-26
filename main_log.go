package main

import (
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// callLog is the sysop log for one call.
// A nil *callLog is safe to use; the methods do nothing.
type callLog struct {
	l       *slog.Logger
	file    *os.File
	logChat bool
	node    int
	alias   string
}

// expandLogPath replaces %n and {node} with the BBS node number.
// Node 0 means the drop file did not say, and the path gets "0".
func expandLogPath(path string, node int) string {
	n := strconv.Itoa(node)
	path = strings.ReplaceAll(path, "%n", n)
	path = strings.ReplaceAll(path, "{node}", n)
	return path
}

// openLog writes text slog lines to stderr, and also to path when path
// is not empty. The caller should close the result.
func openLog(path string, node int, alias string, logChat bool, stderr io.Writer) (*callLog, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	w := stderr
	var f *os.File
	if path != "" {
		var err error
		f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		w = io.MultiWriter(stderr, f)
	}
	return &callLog{
		l:       slog.New(slog.NewTextHandler(w, nil)),
		file:    f,
		logChat: logChat,
		node:    node,
		alias:   alias,
	}, nil
}

func (c *callLog) close() {
	if c == nil || c.file == nil {
		return
	}
	c.file.Close()
	c.file = nil
}

func (c *callLog) event(name string, extra ...any) {
	if c == nil || c.l == nil {
		return
	}
	args := make([]any, 0, 6+len(extra))
	args = append(args, "node", c.node, "alias", c.alias, "event", name)
	args = append(args, extra...)
	c.l.Info(name, args...)
}

func (c *callLog) end(reason string) {
	c.event("end", "reason", reason)
}

func (c *callLog) aiError(err error) {
	if c == nil || c.l == nil || err == nil {
		return
	}
	c.l.Error("ollama_error", "node", c.node, "alias", c.alias, "event", "ollama_error", "err", err.Error())
}

// turn records how long a reply took and how many characters moved.
// The text itself is included only when -logchat was set.
func (c *callLog) turn(d time.Duration, userChars, asstChars int, user, asst string) {
	extra := []any{
		"latency_ms", d.Milliseconds(),
		"user_chars", userChars,
		"assistant_chars", asstChars,
	}
	if c != nil && c.logChat {
		extra = append(extra, "user", user, "assistant", asst)
	}
	c.event("turn", extra...)
}
