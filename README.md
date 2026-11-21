# ollama-door

A BBS door that chats with a local [Ollama](https://ollama.com) server.

The pieces are in place and the program builds. The functions that talk
to Ollama, strip non-ASCII, and wrap lines are stubs with `TODO` bodies.
The read loop echoes each line so you can run it in a terminal today.

## What a BBS door is

A door is a program a bulletin board runs for one caller. Synchronet,
Mystic, ENiGMA, and similar Linux BBS software accept the telnet or SSH
session, then start the door as an ordinary process. The caller types
on the door's standard input. Whatever the door writes to standard
output is what the caller sees. When the process exits, the caller is
back at the BBS menu.

The BBS owns the connection. Your job is the conversation.

## Design

Callers on DOS, Apple II, and Commodore 64 terminals need plain bytes:

- **7-bit ASCII only.** Smart quotes and dashes become ASCII punctuation.
  Emoji and other non-ASCII characters are dropped. See `internal/ascii`.
- **Word wrap at 40 or 80 columns.** Apple II and C64 screens are 40
  columns; a typical DOS terminal is 80. Every line ends with CRLF
  (`\r\n`). See `internal/wrap`.
- **Chat is HTTP to a local Ollama.** `POST /api/chat`, streaming
  (newline-delimited JSON). See `internal/ollama`.
- **Flags, standard library only.** No third-party modules.

```
caller  ->  BBS (telnet/ssh)  ->  stdin  ->  ollama-door
                                              |  ascii.Sanitize
                                              |  wrap.Wrap
                                              |  ollama.Client.Chat
                                         stdout  ->  BBS  ->  caller
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-host` | `http://localhost:11434` | Ollama base URL |
| `-model` | (required) | Model name, for example `llama3.2` |
| `-width` | `80` | Screen width. Pass `-width 40` for a 40-column terminal |
| `-system` | empty | System prompt, sent once when chat is implemented |

Today those flags are parsed and the loop echoes. `-model` is required
so the real chat path has it from the start. Ollama is not contacted yet.

## Layout

```
main.go                  flags and the stdin loop
internal/ascii/          Sanitize(string) string
internal/wrap/           Wrap(text string, width int) string
internal/ollama/         Client, Chat, and the /api/chat JSON types
```

`internal` means only this module can import those packages. Each
package is one job, which keeps `main` small.

Tests in `ascii` and `wrap` are table-driven and call `t.Skip("TODO")`.
`go test ./...` passes. The cases are the spec: when you implement a
function, delete the `t.Skip` line and make those cases pass.

## Build

On the machine where you will run it:

```sh
go build -o ollama-door .
```

`CGO_ENABLED=0` forces the pure-Go build. This module imports only the
standard library, so the result is a static binary you can copy onto a
BBS host that does not have Go or a matching C library:

```sh
CGO_ENABLED=0 go build -o ollama-door .
```

Cross-compile from your laptop for the BBS machine. `GOOS` and `GOARCH`
pick the target. The binary is still named `ollama-door`.

```sh
# 64-bit PC
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ollama-door .

# 64-bit ARM (Raspberry Pi and similar)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ollama-door .

# 32-bit PC
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -o ollama-door .
```

Check the work before you copy the binary over:

```sh
gofmt -l .
go vet ./...
go test ./...
```

`gofmt -l` prints files that differ from gofmt's layout. An empty
listing means the tree is formatted. Run `gofmt -w .` to fix files.

## Try it locally

You do not need a running Ollama server yet. The loop echoes.

```sh
go run . -model llama3.2
```

Type a line and press Enter. The same line comes back, and the cursor
moves to the next row. End the program with Ctrl-D (Unix) or Ctrl-Z
then Enter (Windows). That closes stdin, which is also how the BBS
signals that the caller is gone.

See the CRLF bytes (`0d 0a`) with:

```sh
printf 'Hello\n' | go run . -model llama3.2 | od -An -tx1
```

You should see `48 65 6c 6c 6f 0d 0a` (`Hello` followed by CR LF).

`Sanitize` and `Wrap` still return their input unchanged, so a Unicode
character you type is echoed as-is, and a long line is not wrapped yet.
A status line on stderr shows the flags. It is for you, not for the
caller's screen:

```sh
go run . -model llama3.2 -width 40 -system "You are a door on a BBS."
```

`-model` missing exits 2 and prints the flag help.

Wire the BBS door entry to the `ollama-door` binary when the chat loop
is real. The BBS starts it with the caller's terminal already connected
to stdin and stdout.

## Your next steps

1. Open `internal/ascii/ascii.go`. Implement `Sanitize` using the rules
   in the comment (a `map[rune]string` plus a range over the string is
   enough). In `ascii_test.go`, delete `t.Skip("TODO")`. Run
   `go test ./internal/ascii`.
2. Open `internal/wrap/wrap.go`. Implement `Wrap`: split on spaces,
   fill lines up to `width`, end every line with `\r\n`. Leave a word
   longer than `width` on one line. An existing newline starts a line,
   and empty text stays empty. Delete `t.Skip("TODO")` in
   `wrap_test.go` and run `go test ./internal/wrap`.
3. Watch a real streamed reply so the JSON matches the structs in
   `internal/ollama/types.go`:

   ```sh
   curl -N http://localhost:11434/api/chat \
     -H 'Content-Type: application/json' \
     -d '{"model":"llama3.2","messages":[{"role":"user","content":"hi"}],"stream":true}'
   ```

   Each line is one `ChatResponse`. The last line has `"done": true`.
4. Fill in `Client.Chat` in `internal/ollama/ollama.go`. POST the JSON
   to `{BaseURL}/api/chat` with `net/http` and `encoding/json`. Honor
   `ctx`. On a stream, read line by line (`bufio.Scanner` or
   `bufio.Reader`) and call `onChunk`. Return the final object.
5. Replace the echo in `main.go` with the chat loop described in the
   TODO: keep a `[]ollama.Message`, send `-system` once as
   `ollama.RoleSystem`, and write each chunk through `ascii.Sanitize`
   and `wrap.Wrap`. Run a short conversation with `go run . -model ...`.
6. From the repo root, run `gofmt -w .`, `go vet ./...`, `go test ./...`,
   and `CGO_ENABLED=0 go build -o ollama-door .`.

Later, if you want the caller's name from the BBS, read its drop file
(`door.sys` or `door32.sys`) and add that name to the system prompt.
The door still reads the caller on stdin.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
