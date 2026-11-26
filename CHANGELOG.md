# Changelog

## 1.0.3 (2026-11-26)

- `-echo auto` echoes whenever `-dropfile` is set. Synchronet Standard I/O on Linux is untranslated and often a pty, so the old "not a terminal" check left echo off and the caller saw no typing. Without a drop file, auto still echoes only when stdin is not a character device.

## 1.0.2 (2026-11-26)

- Wrapped rows stop one column short of the screen (79 on 80 columns, 39 on 40). A full row makes SyncTERM autowrap, and the door's CRLF was then adding a blank line. Replies, the banner, and status lines all use that limit.

## 1.0.1 (2026-11-26)

- Session logs are quiet by default. Turn lines and other INFO records no longer go to the caller's stderr.
- `-log` writes the log file. `-verbose` copies those lines to stderr for local debugging. `-logchat` applies only when one of those is on.
- During a call, Ollama's raw error stays in that log. The caller still sees only the short friendly line. Startup failures (bad flags, a bad config file, a log file that will not open) still print on stderr before the screen starts.
- The door echoes keystrokes and accepts Enter as CR, CRLF, LFCR, or CR NUL. A BBS does not do that for a Standard I/O door, so without it SyncTERM showed a cursor and ignored typing. `-echo auto` (the default) echoes when stdin is not a terminal. `-echo on` and `-echo off` force it. Backspace, DEL, and Ctrl-U edit the line. Ctrl-C or Ctrl-D on an empty line quits. `-maxinput` caps the line (default 500).

## 1.0.0

First release of ollama-door, a BBS door that chats with a local Ollama server.

- Streams `POST /api/chat` and writes the reply as 7-bit ASCII, word-wrapped, with CRLF line endings.
- `mono` sends no ANSI. `green` is bright green on black using classic 16-color SGR only. SyncTERM's palette chooses the exact shade.
- Reads `DOOR32.SYS` and `DOOR.SYS` for the caller's alias, ANSI flag, minutes left, and node number.
- `-idle` hangs up a quiet prompt. The drop-file minutes are a separate session clock, with warnings at 5 minutes and 1 minute, and `-maxmin` can cap that budget.
- A dropped carrier (EOF, a broken stdout, `SIGHUP`, `SIGINT`, `SIGTERM`) exits quietly and cancels a reply that is still streaming.
- Ollama failures become a short ASCII line and the `>` prompt returns. `-timeout` cancels a reply that stops producing text.
- Optional JSON config in `-config`. Explicit command-line flags win over the file. Built-in defaults fill the rest.
- `-log` writes a `log/slog` text log. `%n` and `{node}` become the node number. Chat text is omitted unless `-logchat` is set.
- `-version` prints the version string baked in at build time.
