// Command ollama-door is a BBS door: a program the bulletin board starts
// for one caller. The BBS already accepted telnet or SSH. This process
// reads what the caller types on stdin and writes the caller's screen
// on stdout.
//
// Chat is not wired up yet. The loop echoes each line so the program
// compiles and can be tried in a terminal.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/e14tech/ollama-door/internal/ascii"
	"github.com/e14tech/ollama-door/internal/ollama"
	"github.com/e14tech/ollama-door/internal/wrap"
)

func main() {
	host := flag.String("host", "http://localhost:11434", "Ollama server base URL")
	model := flag.String("model", "", "Ollama model name (required)")
	width := flag.Int("width", 80, "screen width in columns; classic terminals use 40 or 80")
	system := flag.String("system", "", "optional system prompt for the chat")
	flag.Parse()

	if *model == "" {
		fmt.Fprint(os.Stderr, "ollama-door: -model is required\r\n")
		flag.Usage()
		os.Exit(2)
	}

	client := ollama.New(*host)

	// TODO: keep a []ollama.Message history. When *system is not empty,
	// start the history with {Role: ollama.RoleSystem, Content: *system}.
	// For each stdin line, append {Role: ollama.RoleUser, Content: line},
	// call client.Chat with Stream set, and write every chunk's
	// Message.Content through ascii.Sanitize and wrap.Wrap to stdout.
	// Stop echoing the caller's raw line once that path works.
	fmt.Fprintf(os.Stderr, "ollama-door: echo mode, model=%s host=%s width=%d\r\n",
		*model, client.BaseURL, *width)
	if *system != "" {
		fmt.Fprintf(os.Stderr, "ollama-door: system prompt saved for later (%d bytes)\r\n", len(*system))
	}

	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		line := in.Text()
		out := wrap.Wrap(ascii.Sanitize(line), *width)
		// Wrap will end lines with CRLF once it is implemented.
		// Until the stub returns text unchanged, add the ending here.
		if !strings.HasSuffix(out, "\r\n") {
			out += "\r\n"
		}
		fmt.Print(out)
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ollama-door: read stdin: %v\r\n", err)
		os.Exit(1)
	}
}
