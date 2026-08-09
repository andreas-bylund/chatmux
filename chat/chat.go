// Package chat defines the platform-agnostic types every chat source speaks.
//
// A Source produces Messages and reports its connection State to a Sink. The
// Sink is whatever consumes them — the TUI, the file recorder, or a fan-out to
// several of those. Sources never know who is listening.
package chat

import (
	"context"
	"errors"
	"time"
)

// Message is a single chat line, normalised across platforms.
//
// The json tags matter: the recorder writes this struct verbatim in JSONL mode,
// so field names here are a file format other tools may parse.
type Message struct {
	Platform  string    `json:"platform"`
	Source    string    `json:"source"`
	Channel   string    `json:"channel"`
	Time      time.Time `json:"time"`
	User      string    `json:"user"`
	Color     string    `json:"color,omitempty"`
	Text      string    `json:"text"`
	Action    bool      `json:"action,omitempty"`
	Highlight bool      `json:"highlight,omitempty"`
}

// State is where a source is in its connection lifecycle.
type State int

// The states a source moves through. A source starts Connecting, and every
// later state is reported by Supervise rather than by the source itself.
const (
	StateConnecting State = iota
	StateConnected
	StateReconnecting
	StateFailed // gave up: permanent error or attempt limit reached
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateFailed:
		return "failed"
	case StateStopped:
		return "stopped"
	}
	return "unknown"
}

// Status reports a source's connection lifecycle to the Sink.
type Status struct {
	Source  string
	State   State
	Err     error
	Attempt int
	Retry   time.Duration // when reconnecting: how long until the next attempt
}

// Sink receives everything a source produces. Implementations must be safe for
// concurrent use — one goroutine per source calls into the same Sink.
type Sink interface {
	Message(Message)
	Status(Status)
}

// Source is one platform connection. Run blocks until the connection drops or
// ctx is cancelled; Supervise decides whether to call it again.
type Source interface {
	// Run streams into sink until it errors or ctx is done. Returning a
	// Permanent error tells Supervise not to retry.
	Run(ctx context.Context, sink Sink) error
	// Label is the "platform:target" string the user typed.
	Label() string
	// Platform is the bare platform name, for tagging and colouring.
	Platform() string
}

// permanentError marks a failure that retrying cannot fix — a missing API key,
// a channel that does not exist, a malformed target.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so Supervise gives up instead of reconnecting.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// IsPermanent reports whether err (or anything it wraps) is Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// SinkFunc adapts two functions into a Sink.
type SinkFunc struct {
	OnMessage func(Message)
	OnStatus  func(Status)
}

// Message calls OnMessage if it is set, and does nothing otherwise.
func (s SinkFunc) Message(m Message) {
	if s.OnMessage != nil {
		s.OnMessage(m)
	}
}

// Status calls OnStatus if it is set, and does nothing otherwise.
func (s SinkFunc) Status(st Status) {
	if s.OnStatus != nil {
		s.OnStatus(st)
	}
}

// MultiSink fans every message and status out to each sink in order.
type MultiSink []Sink

// Message passes m to every sink in order.
func (ms MultiSink) Message(m Message) {
	for _, s := range ms {
		s.Message(m)
	}
}

// Status passes st to every sink in order.
func (ms MultiSink) Status(st Status) {
	for _, s := range ms {
		s.Status(st)
	}
}
