# stream-chat

[![CI](https://github.com/andreas-bylund/Stream-chat-terminal/actions/workflows/ci.yml/badge.svg)](https://github.com/andreas-bylund/Stream-chat-terminal/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/andreas-bylund/Stream-chat-terminal.svg)](https://pkg.go.dev/github.com/andreas-bylund/Stream-chat-terminal)
[![Go Report Card](https://goreportcard.com/badge/github.com/andreas-bylund/Stream-chat-terminal)](https://goreportcard.com/report/github.com/andreas-bylund/Stream-chat-terminal)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Read live chat from **Twitch**, **YouTube** and **Kick** in your terminal. Follow
several streams at once, side by side in a full-screen TUI — or as plain lines on
stdout when you want to pipe it somewhere.

```
┌─ twitch:kaicenat ●───────────────────┬─ kick:iceposeidon ●──────────────────┐
│  T  15:15 TuyoDeMora: make. a. bed.  │  K  15:15 zius: yes o7               │
│  T  15:15 NamDaVietBoi: L kaii       │  K  15:15 Buffz: content             │
│  T  15:15 darftarean: The best Duo   │  K  15:15 Thailandbars: KEKW         │
├──────────────────────────────────────┴──────────────────────────────────────┤
│ ● twitch:kaicenat  ● kick:iceposeidon              412 msg/min · ? help      │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Three platforms, one view** — Twitch, YouTube and Kick side by side, or merged
  into a single column.
- **No accounts, no tokens** for Twitch and Kick. YouTube needs a free API key.
- **Survives bad networks** — every source reconnects on its own with exponential
  backoff and jitter; one dead stream never takes the others with it.
- **Filters and highlights** — hide bot commands and known bots, highlight your
  own name or anything matching a regexp.
- **Optional logging** to dated text or JSONL files, so you can grep or `jq` the
  chat afterwards.
- **Pipe-friendly** — `-plain` writes one line per message to stdout and keeps
  status noise on stderr.

## Installation

### With Go

```bash
go install github.com/andreas-bylund/Stream-chat-terminal@latest
```

The binary is installed as `Stream-chat-terminal`; rename it to `stream-chat` if
you prefer the shorter name.

### From source

```bash
git clone https://github.com/andreas-bylund/Stream-chat-terminal.git
cd Stream-chat-terminal
make build          # produces ./stream-chat
```

Requires Go 1.25 or newer.

### Pre-built binaries

Grab an archive for your platform from the
[releases page](https://github.com/andreas-bylund/Stream-chat-terminal/releases).

## Usage

Each argument is either a source in the form `<platform>:<target>`, or the name of
a preset from the config file.

```bash
stream-chat twitch:xqc
stream-chat kick:trainwreckstv
stream-chat youtube:dQw4w9WgXcQ
stream-chat twitch:xqc kick:trainwreckstv youtube:dQw4w9WgXcQ
stream-chat evening                   # a preset from the config file
```

### Keys

| Key | Action |
| --- | --- |
| `q` / `ctrl+c` | quit |
| `↑` `↓` / `k` `j` | scroll one line |
| `pgup` / `pgdn` | scroll one page |
| `g` / `G` | jump to top / bottom |
| `tab` / `shift+tab` | change the focused pane |
| `u` | toggle between split and unified view |
| `t` | show/hide timestamps |
| `c` | show/hide the platform tag |
| `f` | turn the hide-filters on/off |
| `?` | help |

Scrolling up pauses autoscroll (the pane header shows `↑paused`) so new messages
do not yank the text away from under you. `G` resumes.

### Flags

| Flag | Action |
| --- | --- |
| `-config <path>` | use a different config file |
| `-init-config` | write a commented starter config and exit |
| `-list-presets` | list the configured presets |
| `-plain` | plain lines on stdout instead of the TUI |
| `-unified` | merge all sources into one column |
| `-log` / `-no-log` | force logging on/off for this run |
| `-version` | print the version |

### Plain mode

`-plain` writes one line per message to stdout and status changes to stderr, so a
redirected stdout contains chat and nothing else:

```bash
stream-chat -plain twitch:xqc | grep -i my-name
stream-chat -plain twitch:xqc > chat.txt
```

Plain mode turns on automatically when stdout is not a terminal — a TUI cannot be
drawn there anyway.

## Configuration

```bash
stream-chat -init-config       # writes ~/.config/stream-chat/config.toml
```

The generated file is commented. In short:

```toml
[ui]
mode = "split"        # "split" = one column per source, "unified" = all in one
timestamps = true
scrollback = 2000     # messages kept per pane
compact = false       # true hides the per-line platform tag

[log]
enabled = false
dir = "~/.local/share/stream-chat"
format = "text"       # "text", "jsonl" or "both"
split_by_source = false

[filter]
hide_commands = true                              # hides anything starting with "!"
hide_users = ["nightbot", "streamelements"]
hide_patterns = []                                # regexps matched against the text
min_length = 0

[highlight]
words = ["andreas"]   # case-insensitive substrings
users = []
patterns = []         # regexps, for when a substring is too blunt

[retry]
initial = "2s"
max = "60s"
factor = 2.0
reset_after = "2m"    # a connection that held this long resets the backoff
max_attempts = 0      # 0 = never give up

[presets]
evening = ["twitch:xqc", "kick:trainwreckstv"]
```

The config file lives in `$XDG_CONFIG_HOME/stream-chat/config.toml`, falling back
to `~/.config/stream-chat/config.toml`. Unknown keys produce an error that points
at the typo instead of being silently ignored.

### Filters and highlights

Hidden messages reach neither the screen nor the log — the log matches what you
saw. `f` in the TUI turns the hide-filters off, but only going forward: messages
already filtered out are gone. Highlights are unaffected by `f`; they are a
marking, not a filter.

### Logging

With `log.enabled = true` the chat is written to date-stamped files
(`2026-08-09.log` / `.jsonl`) that roll over at midnight and are appended to
between runs. JSONL lines are `chat.Message` verbatim, so they go straight into
`jq`:

```bash
jq -r 'select(.platform=="twitch") | "\(.user): \(.text)"' ~/.local/share/stream-chat/2026-08-09.jsonl
```

## Platforms

### Twitch

Anonymous IRC over TLS (`irc.chat.twitch.tv:6697`). No token required. The client
pings every 60 seconds and treats 150 seconds of silence as a dead connection, so
dropped links are noticed instead of hanging quietly.

```bash
stream-chat twitch:<channel>
```

### YouTube

YouTube Data API v3. Requires a free API key:

1. Go to the [Google Cloud Console](https://console.cloud.google.com/)
2. Create a project and enable **YouTube Data API v3**
3. Create an **API key** under Credentials
4. `export YOUTUBE_API_KEY="your-key"`

The target can be a video ID or a URL (`watch?v=`, `youtu.be/`, `/live/`,
`/embed/`):

```bash
stream-chat youtube:dQw4w9WgXcQ
stream-chat "youtube:https://youtu.be/dQw4w9WgXcQ"
```

Each poll costs 5 quota units; the default allowance of 10,000/day is roughly 2-3
hours of continuous reading. Exhausted quota, an invalid key and disabled chat are
reported as permanent errors and not retried. If the stream has not started yet
the client keeps trying, so you can start it in advance.

### Kick

Pusher WebSocket, the same one the web client uses. No auth.

```bash
stream-chat kick:<channel-slug>
```

**Unofficial** — Kick publishes no chat API, so the app key and event names are
lifted from the web client and can stop working without notice.

## Reconnection

Every source is supervised separately. A dropped connection is retried with
exponential backoff (2s → 60s) plus ±20% jitter, so several sources lost to the
same network blip do not come back in lockstep. A connection that held for two
minutes counts as healthy and resets the backoff.

Errors that retrying cannot fix — a missing API key, a suspended channel,
exhausted quota — are marked permanent and stop that source instead of looping.
The other sources carry on.

Connection trouble shows up both in the pane header (`● connected`, `◌ retry 8s`,
`✕ failed`) and as a line in the stream, so afterwards you can see where in the
chat the gap was.

## Project layout

```
chat/        Message, Sink, Source + the supervisor with backoff
twitch/      IRC client (irc.go = IRCv3 parser)
youtube/     Data API v3 polling
kick/        Pusher WebSocket
config/      TOML config and presets
filter/      hide and highlight rules
recorder/    file logging
ui/          Bubble Tea TUI (panes, scrollback, status bar)
main.go      CLI and wiring
plain.go     stdout mode
```

Add a platform by implementing `chat.Source` (`Run`, `Label`, `Platform`) and
registering it in `parseSource` in `main.go`. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the details.

## Development

```bash
make test     # go test ./...
make race     # go test -race ./...
make check    # vet + tests
make lint     # golangci-lint (install it first)
```

## Dependencies

- [`coder/websocket`](https://github.com/coder/websocket) — Kick's WebSocket
- [`charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea) +
  [`lipgloss`](https://github.com/charmbracelet/lipgloss) + `x/ansi` — the TUI
- [`BurntSushi/toml`](https://github.com/BurntSushi/toml) — config
- [`mattn/go-isatty`](https://github.com/mattn/go-isatty) — TTY detection

Twitch and YouTube use only the standard library.

## Contributing

Bug reports, ideas and pull requests are welcome. Start with
[CONTRIBUTING.md](CONTRIBUTING.md); participants are expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE) © Andreas Bylund

Not affiliated with, endorsed by, or sponsored by Twitch, YouTube or Kick.
