package chat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// collector is a Sink that records everything it is given.
type collector struct {
	mu       sync.Mutex
	msgs     []Message
	statuses []Status
}

func (c *collector) Message(m Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
}

func (c *collector) Status(s Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statuses = append(c.statuses, s)
}

func (c *collector) states() []State {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]State, len(c.statuses))
	for i, s := range c.statuses {
		out[i] = s.State
	}
	return out
}

type fakeSource struct {
	mu   sync.Mutex
	runs int
	fn   func(ctx context.Context, run int, sink Sink) error
}

func (f *fakeSource) Label() string    { return "fake:one" }
func (f *fakeSource) Platform() string { return "fake" }

func (f *fakeSource) Run(ctx context.Context, sink Sink) error {
	f.mu.Lock()
	f.runs++
	n := f.runs
	f.mu.Unlock()
	return f.fn(ctx, n, sink)
}

func (f *fakeSource) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs
}

// fastRetry keeps the tests quick while preserving the backoff shape.
func fastRetry() Retry {
	return Retry{
		Initial:    time.Millisecond,
		Max:        4 * time.Millisecond,
		Factor:     2,
		ResetAfter: time.Hour,
	}
}

func TestSuperviseStopsOnPermanentError(t *testing.T) {
	src := &fakeSource{fn: func(context.Context, int, Sink) error {
		return Permanent(errors.New("no API key"))
	}}
	c := &collector{}

	done := make(chan struct{})
	go func() {
		Supervise(t.Context(), src, c, fastRetry())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Supervise did not return on a permanent error")
	}

	if got := src.count(); got != 1 {
		t.Errorf("source ran %d times, want 1", got)
	}
	states := c.states()
	if len(states) == 0 || states[len(states)-1] != StateFailed {
		t.Errorf("final state = %v, want failed", states)
	}
}

func TestSuperviseRetriesTransientFailures(t *testing.T) {
	const target = 4
	reached := make(chan struct{})

	src := &fakeSource{fn: func(ctx context.Context, run int, _ Sink) error {
		if run == target {
			close(reached)
			<-ctx.Done()
			return nil
		}
		return errors.New("dropped")
	}}
	c := &collector{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		Supervise(ctx, src, c, fastRetry())
		close(done)
	}()

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("source only ran %d times, want %d", src.count(), target)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Supervise did not return after cancellation")
	}

	var sawReconnecting bool
	for _, s := range c.states() {
		if s == StateReconnecting {
			sawReconnecting = true
		}
	}
	if !sawReconnecting {
		t.Error("no reconnecting status was reported")
	}
	if last := c.states()[len(c.states())-1]; last != StateStopped {
		t.Errorf("final state = %v, want stopped", last)
	}
}

func TestSuperviseHonoursMaxAttempts(t *testing.T) {
	src := &fakeSource{fn: func(context.Context, int, Sink) error {
		return errors.New("nope")
	}}
	c := &collector{}

	r := fastRetry()
	r.MaxAttempts = 3

	done := make(chan struct{})
	go func() {
		Supervise(t.Context(), src, c, r)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Supervise ignored MaxAttempts")
	}
	if got := src.count(); got != 3 {
		t.Errorf("source ran %d times, want 3", got)
	}
	if last := c.states()[len(c.states())-1]; last != StateFailed {
		t.Errorf("final state = %v, want failed", last)
	}
}

func TestSuperviseStampsSourceLabel(t *testing.T) {
	src := &fakeSource{fn: func(_ context.Context, _ int, sink Sink) error {
		sink.Status(Status{State: StateConnected})
		sink.Message(Message{Text: "hi"})
		return Permanent(errors.New("done"))
	}}
	c := &collector{}
	Supervise(t.Context(), src, c, fastRetry())

	if len(c.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(c.msgs))
	}
	if c.msgs[0].Source != "fake:one" {
		t.Errorf("Message.Source = %q, want fake:one", c.msgs[0].Source)
	}
	for _, s := range c.statuses {
		if s.Source != "fake:one" {
			t.Errorf("Status.Source = %q, want fake:one", s.Source)
		}
	}
}

func TestSuperviseReturnsWhenContextIsAlreadyDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := &fakeSource{fn: func(context.Context, int, Sink) error {
		t.Error("source ran despite a cancelled context")
		return nil
	}}

	done := make(chan struct{})
	go func() {
		Supervise(ctx, src, &collector{}, fastRetry())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Supervise blocked on a cancelled context")
	}
}

func TestRetryDefaults(t *testing.T) {
	r := Retry{}.withDefaults()
	if r.Initial <= 0 || r.Max < r.Initial || r.Factor <= 1 || r.ResetAfter <= 0 {
		t.Errorf("withDefaults produced an unusable policy: %+v", r)
	}

	// A Max below Initial would make the backoff shrink instead of grow.
	r = Retry{Initial: time.Minute, Max: time.Second}.withDefaults()
	if r.Max < r.Initial {
		t.Errorf("Max = %v, want at least Initial (%v)", r.Max, r.Initial)
	}
}

func TestJitterStaysInBounds(t *testing.T) {
	const d = time.Second
	for i := 0; i < 200; i++ {
		got := jitter(d)
		if got < 800*time.Millisecond || got > 1200*time.Millisecond {
			t.Fatalf("jitter(%v) = %v, want within ±20%%", d, got)
		}
	}
	if jitter(0) != 0 {
		t.Error("jitter(0) should be 0")
	}
}

func TestPermanentUnwraps(t *testing.T) {
	base := errors.New("root cause")
	err := Permanent(base)
	if !errors.Is(err, base) {
		t.Error("Permanent should keep the wrapped error reachable")
	}
	if !IsPermanent(err) {
		t.Error("IsPermanent should recognise its own wrapper")
	}
	if IsPermanent(base) {
		t.Error("a plain error should not be permanent")
	}
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) should be nil")
	}
}

func TestMultiSinkFansOut(t *testing.T) {
	a, b := &collector{}, &collector{}
	ms := MultiSink{a, b}
	ms.Message(Message{Text: "x"})
	ms.Status(Status{State: StateConnected})

	for i, c := range []*collector{a, b} {
		if len(c.msgs) != 1 || len(c.statuses) != 1 {
			t.Errorf("sink %d got %d messages and %d statuses, want 1 and 1", i, len(c.msgs), len(c.statuses))
		}
	}
}
