# Contributing

Thanks for taking the time. Bug reports, ideas and pull requests are all welcome.

## Getting started

```bash
git clone https://github.com/andreas-bylund/Stream-chat-terminal.git
cd Stream-chat-terminal
make build
make check     # go vet + go test ./...
```

Go 1.25 or newer is required. There are no other build dependencies.

## Before you open a pull request

- `make check` passes, and `make race` passes for anything touching concurrency.
- `gofmt -s -w .` has been run — CI rejects unformatted code.
- New behaviour comes with a test. The packages are deliberately easy to test:
  `chat.Sink` is an interface, and every platform client parses protocol data in
  a function that takes bytes and returns a `chat.Message`.
- Exported identifiers have doc comments, starting with the identifier's name.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(kick): follow channel renames
fix(twitch): survive a NOTICE with no msg-id tag
docs: document the -plain flag
```

Types in use: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`,
`chore`. The scope is usually the package (`chat`, `twitch`, `youtube`, `kick`,
`config`, `filter`, `recorder`, `ui`, `cli`).

Write the subject in the imperative mood, and explain *why* in the body when the
change is not obvious. One logical change per commit.

## Adding a platform

A platform is anything that can implement `chat.Source`:

```go
type Source interface {
    Run(ctx context.Context, sink chat.Sink) error
    Label() string     // "platform:target", as the user typed it
    Platform() string  // bare platform name, for tagging and colouring
}
```

1. Create a package next to `twitch/`, `youtube/` and `kick/`.
2. `Run` blocks until the connection drops or `ctx` is done. Report
   `chat.StateConnected` through the sink once you are actually receiving, and
   return an error when you stop.
3. Wrap failures that retrying cannot fix — a missing credential, a channel that
   does not exist — in `chat.Permanent`. Everything else is retried for you by
   `chat.Supervise` with backoff; do not write your own reconnect loop.
4. Register the platform in `parseSource` in `main.go`.
5. Give the platform a tag and colour in `ui/theme.go`.
6. Document it in the README under "Platforms", including whether it needs
   credentials and whether the API is official.

Please do not add a platform that requires the user to hand over account
credentials, and note clearly in the README when an endpoint is unofficial and
may break.

## Design notes

A few constraints that are load-bearing, so a change that ignores them will look
fine and behave badly:

- **Source goroutines must never block on rendering.** A busy channel produces
  messages faster than a terminal can draw them. `ui.sink` buffers and hands
  batches to Bubble Tea at ~20fps; do not call into the renderer per message.
- **Filtering happens once, before the fan-out.** Hidden messages reach neither
  the screen nor the log, so the log matches what was on screen.
- **`chat.Message` is a file format.** The recorder writes it verbatim in JSONL
  mode, so renaming a field breaks other people's `jq` scripts. Add fields with
  `omitempty`; do not rename or remove them without a note in the changelog.
- **Reconnect policy lives in `chat.Supervise`,** not in the platform clients.
- **Chat is untrusted input aimed at a terminal.** `chat.Supervise` strips
  control characters from everything a source emits, because an ESC in a message
  is not a character but a command. Render sanitized text; never route source
  output around that boundary.

## Reporting bugs

Open an issue with the version (`stream-chat -version`), your OS and terminal,
the exact command you ran, and what happened instead. For anything involving a
specific channel, include the source string — chat protocols behave differently
depending on the channel's settings.

## Security

Please do not open a public issue for a security problem. See
[SECURITY.md](SECURITY.md).

## Licence

By contributing you agree that your contribution is licensed under the
[MIT License](LICENSE).
