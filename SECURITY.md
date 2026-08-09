# Security Policy

## Supported versions

This is a small project: security fixes go into the latest release. Please make
sure you can reproduce the issue on the most recent version before reporting.

## Reporting a vulnerability

Please do not open a public issue.

Use GitHub's private reporting on the
[Security tab](https://github.com/andreas-bylund/chatmux/security/advisories/new),
or email **andreas@andreasbylund.se**.

Include what you did, what happened, and what you expected. A proof of concept
helps but is not required. You can expect an initial reply within a week.

## Scope

`chatmux` is a read-only client: it connects out to Twitch, YouTube and Kick,
it never authenticates as you on Twitch or Kick, and it never sends messages.
Things that are in scope anyway:

- Chat content is untrusted input. Control characters are stripped from every
  message before it reaches the screen or the log — see `sanitize` in
  `chat/sanitize.go` — so anything that still escapes the terminal rendering, or
  crashes the client, is a bug worth reporting.
- The config file and any file path derived from it — including source labels
  used to build log filenames.
- Anything that leaks `YOUTUBE_API_KEY` outside the process, for example into a
  log file, an error message, or a URL that gets written to disk.

Out of scope: vulnerabilities in Twitch, YouTube or Kick themselves, and the fact
that Kick's endpoints are unofficial and may change.
