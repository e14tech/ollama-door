package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/e14tech/ollama-door/internal/config"
	"github.com/e14tech/ollama-door/internal/lineedit"
)

// options is the door's settings after defaults, the config file, and
// explicit flags have been merged.
type options struct {
	host     string
	model    string
	system   string
	theme    string
	width    int
	idle     time.Duration
	timeout  time.Duration
	maxMin   int
	logPath  string
	logChat  bool
	verbose  bool
	echo     string
	maxInput int
}

func defaultOptions() options {
	return options{
		host:     "http://localhost:11434",
		system:   defaultSystem,
		theme:    "mono",
		width:    80,
		idle:     5 * time.Minute,
		timeout:  60 * time.Second,
		echo:     "auto",
		maxInput: lineedit.DefaultMax,
	}
}

// flagOptions is the values flag.Parse stored. applyFlags copies a field
// only when that flag was present on the command line.
type flagOptions struct {
	host     string
	model    string
	system   string
	theme    string
	width    int
	idle     time.Duration
	timeout  time.Duration
	maxMin   int
	logPath  string
	logChat  bool
	verbose  bool
	echo     string
	maxInput int
}

// applyConfig copies keys that the file actually set.
// The bool reports whether "theme" was one of those keys.
func applyConfig(o options, file config.File) (options, bool, error) {
	themeSet := false
	if file.Host != nil {
		o.host = *file.Host
	}
	if file.Model != nil {
		o.model = *file.Model
	}
	if file.System != nil {
		o.system = *file.System
	}
	if file.Theme != nil {
		o.theme = *file.Theme
		themeSet = true
	}
	if file.Width != nil {
		o.width = *file.Width
	}
	if file.Idle != nil {
		d, err := time.ParseDuration(*file.Idle)
		if err != nil {
			return o, false, fmt.Errorf("idle: %w", err)
		}
		o.idle = d
	}
	if file.Timeout != nil {
		d, err := time.ParseDuration(*file.Timeout)
		if err != nil {
			return o, false, fmt.Errorf("timeout: %w", err)
		}
		o.timeout = d
	}
	if file.MaxMin != nil {
		o.maxMin = *file.MaxMin
	}
	if file.Log != nil {
		o.logPath = *file.Log
	}
	if file.LogChat != nil {
		o.logChat = *file.LogChat
	}
	if file.Verbose != nil {
		o.verbose = *file.Verbose
	}
	if file.Echo != nil {
		if !validEcho(*file.Echo) {
			return o, false, fmt.Errorf("echo: must be auto, on, or off")
		}
		o.echo = strings.ToLower(strings.TrimSpace(*file.Echo))
	}
	if file.MaxInput != nil {
		if *file.MaxInput < 1 {
			return o, false, fmt.Errorf("maxinput: must be at least 1")
		}
		o.maxInput = *file.MaxInput
	}
	return o, themeSet, nil
}

// applyFlags lets an explicit command-line flag replace the config file.
// visited comes from flag.Visit, which skips flags the operator did not type.
func applyFlags(o options, visited map[string]bool, f flagOptions) options {
	if visited["host"] {
		o.host = f.host
	}
	if visited["model"] {
		o.model = f.model
	}
	if visited["system"] {
		o.system = f.system
	}
	if visited["theme"] {
		o.theme = f.theme
	}
	if visited["width"] {
		o.width = f.width
	}
	if visited["idle"] {
		o.idle = f.idle
	}
	if visited["timeout"] {
		o.timeout = f.timeout
	}
	if visited["maxmin"] {
		o.maxMin = f.maxMin
	}
	if visited["log"] {
		o.logPath = f.logPath
	}
	if visited["logchat"] {
		o.logChat = f.logChat
	}
	if visited["verbose"] {
		o.verbose = f.verbose
	}
	if visited["echo"] {
		o.echo = f.echo
	}
	if visited["maxinput"] {
		o.maxInput = f.maxInput
	}
	return o
}
