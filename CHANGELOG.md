# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-09

First public release.

### Added

- Full-screen TUI with one scrollable pane per source, a unified single-column
  mode, and a status bar showing connection state and message rate.
- Per-source supervision: dropped connections reconnect with exponential backoff
  and jitter, and errors that retrying cannot fix stop only that source.
- TOML configuration with presets, written commented by `-init-config`.
- Hide filters (bot commands, nicks, regexps, minimum length) and highlight rules
  (words, nicks, regexps).
- Optional logging to date-stamped `.log` and `.jsonl` files, rolling over at
  midnight.
- `-plain` mode writing one line per message to stdout, enabled automatically
  when stdout is not a terminal.
- Twitch client over anonymous IRC with keepalive and dead-link detection.
- YouTube client over Data API v3 with quota-aware polling and backlog skipping.
- Kick client over the Pusher WebSocket.

### Fixed

- The message-rate counter no longer panics on a chat message timestamped before
  1970. Twitch's `tmi-sent-ts` is taken from the IRC tag without a range check,
  so a bogus value indexed the counter's bucket ring negatively and crashed the
  client instead of misreporting a number in the status bar.

### Security

- Control characters are stripped from all chat text, nicknames and error
  messages before they reach the terminal or the log. A chat message containing
  an ANSI escape could previously repaint the screen, move the cursor over
  earlier output, or emit sequences that some terminals answer on stdin — and it
  was replayed when a saved log was later printed.

[Unreleased]: https://github.com/andreas-bylund/chatmux/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/andreas-bylund/chatmux/releases/tag/v0.1.0
