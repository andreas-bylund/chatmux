package chat

import (
	"strings"
	"unicode"
)

// sanitize strips control characters from untrusted text.
//
// Chat is written by strangers and rendered straight into a terminal, where an
// ESC is not a character but a command: a message can clear the screen, move the
// cursor over earlier lines, or use OSC sequences that some terminals answer on
// stdin. The same text is appended to log files that get cat'd later, which
// replays the sequence long after the stream ended.
//
// Nothing legitimate in a chat line needs a C0 or C1 control code, so they are
// dropped outright rather than escaped. Whitespace controls collapse to a space
// so removing them cannot run two words together.
func sanitize(s string) string {
	// The overwhelming majority of messages are clean, and a busy channel puts
	// hundreds a second through here, so scan before allocating.
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// Dropped: C0, DEL and C1.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizedError reports a cleaned message while staying transparent to
// errors.Is and errors.As, so wrapping does not hide what the error was.
type sanitizedError struct {
	err error
	msg string
}

func (e sanitizedError) Error() string { return e.msg }
func (e sanitizedError) Unwrap() error { return e.err }

// sanitizeErr cleans an error's message the same way sanitize cleans chat text.
// Platform errors quote server-supplied strings, which are no more trustworthy
// than the chat itself.
func sanitizeErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	clean := sanitize(msg)
	if clean == msg {
		return err
	}
	return sanitizedError{err: err, msg: clean}
}
