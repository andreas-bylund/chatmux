package chat

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Retry controls how Supervise reconnects a dropped source.
type Retry struct {
	Initial     time.Duration // delay before the first reconnect
	Max         time.Duration // ceiling for the exponential growth
	Factor      float64       // multiplier applied after each failed attempt
	ResetAfter  time.Duration // a run lasting this long counts as healthy: backoff resets
	MaxAttempts int           // consecutive failures before giving up; 0 = never give up
}

// DefaultRetry is tuned for a chat reader left running for hours: quick to come
// back from a blip, but backing off to once a minute for a stream that is
// simply offline.
func DefaultRetry() Retry {
	return Retry{
		Initial:    2 * time.Second,
		Max:        60 * time.Second,
		Factor:     2,
		ResetAfter: 2 * time.Minute,
	}
}

func (r Retry) withDefaults() Retry {
	d := DefaultRetry()
	if r.Initial <= 0 {
		r.Initial = d.Initial
	}
	if r.Max <= 0 {
		r.Max = d.Max
	}
	if r.Max < r.Initial {
		r.Max = r.Initial
	}
	if r.Factor <= 1 {
		r.Factor = d.Factor
	}
	if r.ResetAfter <= 0 {
		r.ResetAfter = d.ResetAfter
	}
	return r
}

// sourceSink is the trust boundary every source passes through. It stamps the
// source label onto everything flowing past, so individual sources never have to
// repeat their own name, and it strips control characters, so a new platform
// cannot forget to and hand the terminal a message that is really a command.
type sourceSink struct {
	label string
	inner Sink
}

func (s sourceSink) Message(m Message) {
	m.Source = s.label
	m.User = sanitize(m.User)
	m.Text = sanitize(m.Text)
	m.Channel = sanitize(m.Channel)
	s.inner.Message(m)
}

func (s sourceSink) Status(st Status) {
	st.Source = s.label
	st.Err = sanitizeErr(st.Err)
	s.inner.Status(st)
}

// Supervise runs src until ctx is cancelled, reconnecting with exponential
// backoff whenever it drops. It returns when ctx is done, when src fails
// permanently, or when the attempt limit is hit.
func Supervise(ctx context.Context, src Source, sink Sink, r Retry) {
	r = r.withDefaults()
	s := sourceSink{label: src.Label(), inner: sink}

	delay := r.Initial
	attempt := 0

	for {
		if ctx.Err() != nil {
			s.Status(Status{State: StateStopped})
			return
		}

		attempt++
		s.Status(Status{State: StateConnecting, Attempt: attempt})

		start := time.Now()
		err := src.Run(ctx, s)
		lasted := time.Since(start)

		if ctx.Err() != nil {
			s.Status(Status{State: StateStopped})
			return
		}
		if IsPermanent(err) {
			s.Status(Status{State: StateFailed, Err: err, Attempt: attempt})
			return
		}

		// A connection that stayed up a good while was healthy; treat the next
		// drop as a fresh incident rather than escalating an old backoff.
		if lasted >= r.ResetAfter {
			delay, attempt = r.Initial, 1
		}

		if r.MaxAttempts > 0 && attempt >= r.MaxAttempts {
			if err == nil {
				err = fmt.Errorf("connection closed")
			}
			s.Status(Status{
				State:   StateFailed,
				Err:     fmt.Errorf("giving up after %d attempts: %w", attempt, err),
				Attempt: attempt,
			})
			return
		}

		wait := jitter(delay)
		s.Status(Status{State: StateReconnecting, Err: err, Attempt: attempt, Retry: wait})

		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			s.Status(Status{State: StateStopped})
			return
		}

		delay = min(time.Duration(float64(delay)*r.Factor), r.Max)
	}
}

// jitter spreads reconnects by ±20% so several sources dropped by the same
// network blip do not stampede back in lockstep.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	f := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * f)
}
