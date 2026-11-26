# ollama-door

A BBS door that chats with a local [Ollama](https://ollama.com) server.

The caller types a line. The door sends the conversation to Ollama and
writes the reply as plain ASCII, wrapped to the screen width. Every line
the caller sees ends with CRLF (`\r\n`).

A BBS launches the door with `-dropfile`, pointing at the `DOOR32.SYS`
or `DOOR.SYS` it just wrote. That file is the normal way to learn who
is calling, whether their terminal can do ANSI, and how many minutes
they have left. `-theme` and `-width` are optional overrides for a local
test or a sysop tweak.

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
  Accented letters become the base letter. Emoji and other non-ASCII
  characters are dropped. See `internal/ascii`.
- **Word wrap at 40 or 80 columns.** Apple II and C64 screens are 40
  columns; a typical DOS terminal is 80. A word longer than the width
  is cut so it cannot run off the screen. Every line ends with CRLF
  (`\r\n`). See `internal/wrap`.
- **Two themes, no boxes.** `mono` sends no ANSI at all. `green` is a
  blank bright-green-on-black screen and a cursor. See `internal/theme`.
- **Chat is HTTP to a local Ollama.** `POST /api/chat`, streaming
  (newline-delimited JSON). See `internal/ollama`.
- **The drop file is the caller's ID card.** Alias, ANSI, and minutes
  left. See `internal/dropfile`.
- **Flags, standard library only.** No third-party modules.

```
caller  ->  BBS (telnet/ssh)  ->  stdin  ->  ollama-door
                                      dropfile (alias, ANSI, minutes)
                                              |  theme
                                              |  ascii.Sanitize
                                              |  wrap
                                              |  ollama.Client.Chat
                                         stdout  ->  BBS  ->  caller
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-model` | (required) | Model name, for example `llama3.2` |
| `-dropfile` | none | Path to `DOOR32.SYS` or `DOOR.SYS`. This is how a BBS starts the door |
| `-host` | `http://localhost:11434` | Ollama base URL |
| `-width` | `80` | Optional column override. Classic terminals use 40 or 80 |
| `-theme` | from the drop file, else `mono` | Optional override: `mono` or `green` |
| `-idle` | `5m` | Hang up if the caller types nothing for this long. `0` turns it off |
| `-timeout` | `60s` | Cancel a reply that sits with no new text for this long. `0` turns it off |
| `-maxmin` | `0` | Cap the session minutes. `0` uses the drop file only |
| `-system` | built-in assistant prompt | First chat message. The default says the model is not the sysop and not a person |
| `-config` | none | Optional JSON file. Explicit flags override it |
| `-log` | stderr only | Log file path. `%n` and `{node}` become the node number |
| `-logchat` | off | Also write the chat text into the log |
| `-version` |  | Print the version and exit |

`-theme` and `-width` are for a local test and for a sysop who wants to
force a setting. When `-theme` is left off, an ANSI caller (drop file
emulation, or `GR` in `DOOR.SYS`) gets `green`, and an ASCII caller gets
`mono`. With no drop file the door uses `mono` and 80 columns, and says
so on stderr.

The drop file's minutes field is the session clock. `HasLimit` is set
only when that line is present, so `0` means the caller is already out
of time, and a missing line means there is no clock. No `-dropfile` is
the same as no clock. The door warns once at 5 minutes left and again
at 1 minute left, and only while the `>` prompt is waiting. At zero it
prints `Your time is up. Returning to the BBS...` and exits. A reply
that is already streaming is allowed to finish; the session deadline
does not cancel that HTTP call. `-maxmin 30` caps a longer drop-file
budget at 30 minutes. `-maxmin 0` leaves the drop file in charge. If
the file has no limit, a positive `-maxmin` is the whole budget.

`-idle` is a second clock, and it runs only while the `>` prompt is
waiting for a line. A long Ollama reply does not count. The default is
5 minutes. About a minute before the limit, when the limit is at least
2 minutes, the door prints `Still there? One minute left.` When the
limit is reached it prints `Idle too long. Goodbye!` and exits.
`-idle 0` turns that off. `-idle 30s` is useful while you are testing.
Synchronet also has Maximum Inactivity, which hangs up the socket from
the BBS side. `-idle` is the door's own wait.

## Hangups

EOF on stdin, a failed read, a failed write on stdout (`EPIPE` or
`EIO`), and `SIGHUP`, `SIGINT`, or `SIGTERM` end the call with status 0
and no goodbye. Nobody is left to read one. If a reply is streaming,
that HTTP request is canceled. The session clock does not cancel it.

The line reader is a goroutine. It exits when the read returns, or when
it is waiting to hand off a line and the hangup context is canceled. A
read blocked in the kernel ends when stdin is closed, which is what the
BBS does when the carrier drops.

## When the AI is not there

A failed turn does not end the call. The caller sees one plain line and
the `>` prompt comes back. The Go error is written to stderr for the
sysop.

| What happened | What the caller sees |
| --- | --- |
| Ollama cannot be reached | The AI is napping right now. Try again in a bit. |
| The model is not installed (HTTP 404) | The AI model isn't installed. Please tell the sysop. |
| Any other server or stream error | Something went wrong talking to the AI. Try again. |
| No new text for `-timeout` | The AI is taking too long. Try again. |

`-timeout` starts when the request starts and resets each time a piece
of the reply arrives. A long answer that keeps producing text is left
alone. `0` disables the stall check. The timer is a context wrapped
around that one request. Hanging up and the session clock still use
their own contexts.

The user line is sent with the request, then removed again if no answer
comes back. The next turn is not two user lines in a row. Text that
already reached the screen stays there. A half-written line is finished
with CRLF before the apology.

Before the prompt, the door asks Ollama `GET /api/version`. If nothing
is listening, the napping line is printed once and the prompt still
appears.

The screen stays plain. There is no box and no border. `green` clears
the display, homes the cursor, and leaves the `>` prompt on a black
screen. `mono` never sends an escape byte, so a dumb terminal or an
Apple II is safe.

The chat keeps the system message and the last turns, about 20 messages
in all, so a long call does not grow the request without limit.

## Themes

`mono` is the safe theme. The door writes text and CRLF only.

`green` is a period CRT: bright green on black, using only the original
16-color SGR codes. The door does not pick the shade of green. A terminal
program such as [SyncTERM](https://syncterm.bbsdev.net/) does, from its
palette. The sequence is:

```
ESC[0;1;32;40m ESC[2J ESC[H ESC[0;1;32;40m
```

`ESC[0;1;32;40m` means reset, intensity, green foreground, black
background. The color is set before the clear so the blank screen is
black, then set again after `ESC[2J` and `ESC[H`. Some terminals forget
the attribute when they clear. On the way out the door sends `ESC[0m`.

ANSI.SYS, Telix, Qmodem, and the ANSI modes of C64 and Apple terminal
programs understand these codes. They do not understand 256-color
(`38;5`) or truecolor (`38;2`). Those sequences show up as garbage, so
this door never sends them.

## Drop file

The base name picks the format, ignoring case. `door32.sys` and
`DOOR32.SYS` are the same kind of file. Linux BBSes often write the
lowercase name and DOS CRLF line endings. Each line is trimmed.

`DOOR32.SYS` (Door32 Revision 1) lines this door reads:

| Line | Field |
| --- | --- |
| 7 | Alias |
| 9 | Minutes left |
| 10 | Emulation: 0 ASCII, 1 ANSI, 2 Avatar, 3 RIP, 4 Max Graphics |

Emulation 1 through 4 counts as ANSI. The Door32 spec says Avatar, RIP,
and Max Graphics all have an ANSI fallback. Emulation 0 is `mono`.
Line 2 is a socket handle, not the caller's name. This door uses stdin
and stdout, so it does not take that handle.

`DOOR.SYS` (the 52-line GAP / Synchronet file):

| Line | Field |
| --- | --- |
| 10 | Full name, used when line 36 is missing or blank |
| 19 | Minutes left |
| 20 | Graphics: `GR` is ANSI, `NG` and `7E` are not |
| 21 | Page length in rows. Not used. Width stays on `-width` |
| 36 | Alias |

A short file is fine. Missing lines stay empty or zero.

Sources: the Door32 spec mirrored in
[door32_sys.txt](https://github.com/NuSkooler/ansi-bbs/blob/master/docs/dropfile_formats/door32_sys.txt),
and the Synchronet [DOOR.SYS](http://wiki.synchro.net/ref:door.sys) page.

## Layout

```
main.go                  flags, the drop file, and the stdin loop
internal/ascii/          Sanitize(string) string
internal/wrap/           Wrap and LineWriter (CRLF, 40 or 80 columns)
internal/ollama/         Client, Chat, and the /api/chat JSON types
internal/theme/          mono and green
internal/dropfile/       DOOR32.SYS and DOOR.SYS
examples/door32.sys      a sample drop file for a local test
examples/ollama-door.json sample sysop config
CHANGELOG.md             release notes
```

`internal` means only this module can import those packages. Each
package is one job, which keeps `main` small.

## Build

On the machine where you will run it:

```sh
go build -o ollama-door .
```

`CGO_ENABLED=0` forces the pure-Go build. This module imports only the
standard library, so the result is a static binary you can copy onto a
BBS host that does not have Go or a matching C library:

```sh
CGO_ENABLED=0 go build -ldflags "-X main.version=1.0.0" -o ollama-door .
```

`make build` runs that command. `main.version` is what `-version` prints.

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

`-model` missing, `-width` below 1, or a `-theme` other than `mono` or
`green` exits 2 and prints the flag help.

## Quick local test

You need Ollama listening on `http://localhost:11434` and a model pulled.
`examples/door32.sys` is a small Synchronet-style drop file: alias Josh,
60 minutes, emulation 1 (ANSI). It uses CRLF, the way a Linux BBS writes
the file. Because emulation is ANSI, the door picks the green theme
unless you override it.

```sh
ollama pull llama3.2
go run . -model llama3.2 -dropfile examples/door32.sys
```

The screen clears to black, the greeting says hello to Josh, and the
cursor sits at `>`. Type a line and press Enter. The reply is plain
ASCII, wrapped to 80 columns. End the call with `quit`, `/q`, or `bye`.
Closing stdin (Ctrl-D on Unix, Ctrl-Z then Enter on Windows) also ends
it. That is how the BBS signals that the caller is gone.

A 40-column screen, still using the drop file for the name and the theme:

```sh
go run . -model llama3.2 -dropfile examples/door32.sys -width 40
```

Force mono when you want to see the bytes with no color, for example
under `od`:

```sh
printf 'quit\n' | go run . -model llama3.2 -dropfile examples/door32.sys -theme mono | od -An -tx1
```

You should see `0d 0a` (CR LF) at the ends of lines, and no `1b` bytes.

If Ollama is not running, the door prints a short apology and the `>`
prompt comes back. The details are on stderr. Stderr is for you, not
for the caller's screen.

Running with no `-dropfile` still works (`mono`, 80 columns). Stderr
notes that a BBS is expected to pass one.

## Synchronet setup

This is the shape of an external program on Synchronet (including a
Gentoo install). The field names below are from the Synchronet wiki.
Menu keystrokes differ by version, so use the pages linked here rather
than a memorized path. Older SCFG screens label the drop file type
"Mystic DOOR32.SYS"; current ones say `DOOR32.SYS`.

Add an online program under SCFG → External Programs → Online Programs.
The settings that matter for this door:

| SCFG field | Value |
| --- | --- |
| Start-up Directory | `../xtrn/ollama-door` |
| Command Line | `ollama-door -dropfile %f -config ollama-door.json` |
| Native Executable | Yes |
| I/O Method | Standard |
| Multiple Concurrent Users | Yes |
| Use Shell or New Context | No |
| BBS Drop File Type | `DOOR32.SYS` |
| Place Drop File In | Node Directory |

I/O Method `Standard` connects the caller's terminal to stdin and stdout.
`Socket` is for a door that takes the TCP handle out of `DOOR32.SYS`
line 2. This door does not. It reads and writes the standard streams.

Put `examples/ollama-door.json` next to the binary and edit the model
name. The command line in the table is enough: `%f` is the drop file
Synchronet just wrote. On Linux that name is often lowercase
(`door32.sys`). The door matches either.

`%F` is the same path without automatic quotes. Use it when Use Shell
or New Context is No and the path has no spaces. `%w` is the caller's
column count if you want `-width %w` as well.

A sample config:

```json
{
  "host": "http://127.0.0.1:11434",
  "model": "llama3.2",
  "theme": "green",
  "width": 80,
  "idle": "5m",
  "timeout": "60s",
  "maxmin": 30,
  "log": "ollama-door-%n.log",
  "logchat": false
}
```

Built-in defaults fill any key you leave out. A flag typed on the
command line replaces the file. `-theme` on the command line also
replaces the drop file's ANSI choice. A theme that appears only in the
config file does that too.

The log is `log/slog` text. Each line has a timestamp, the node, the
alias, and an event: `start`, `end` with a reason (`quit`, `idle`,
`timeup`, or `hangup`), `ollama_error`, or `turn` (how long the reply
took, and how many characters moved). The words of the chat stay out
of the file unless `logchat` is true. `%n` and `{node}` in the path
become the node number from `DOOR32.SYS` line 11 or `DOOR.SYS` line 4.

`%w` is the client's terminal width. Leave `-theme` off so the drop
file picks `green` or `mono`.

Ollama has to be reachable from the BBS host. On the same Gentoo
machine the default `-host` is enough.

Time Options → Maximum Time caps the minutes Synchronet writes into the
drop file. The door ends the call when that many minutes pass. Monitor
Time Left is separate: when it is Yes, Synchronet itself disconnects
the caller at zero. See the wiki for the rest of that menu.

Docs:

- [Install Doors](http://wiki.synchro.net/howto:door:index)
- [External Programs](http://wiki.synchro.net/config:external_programs)
- [Command Lines](http://wiki.synchro.net/config:cmdline) (`%F`, `%w`, and the other specifiers)
- [DOOR.SYS](http://wiki.synchro.net/ref:door.sys)

## Your next steps

The chat loop, sanitizer, wrapper, Ollama client, themes, drop file
reader, and idle hangup are in place. A few small exercises are left:

1. **Warning lead time.** `idleWaits` warns one minute before the end,
   and only when `-idle` is at least 2 minutes. Change it so a 90 second
   limit warns 30 seconds ahead, and add a row to `TestIdleWaits`.
2. **Node number.** `DOOR32.SYS` line 11 is the node. Read it in
   `internal/dropfile` and add `Node 1` to the greeting.
3. **`/time`.** When the caller types `/time`, print how many minutes
   are left on the session clock. `HasLimit` false means there is no
   clock to show.

Run a short conversation when you want to see the whole path:

```sh
go run . -model llama3.2 -dropfile examples/door32.sys
```

From the repo root, `gofmt -w .`, `go vet ./...`, `go test ./...`, and
`CGO_ENABLED=0 go build -o ollama-door .` should all succeed.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
