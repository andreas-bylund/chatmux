package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean text is returned unchanged", "hello world", "hello world"},
		{"emoji and non-ascii survive", "héj 👋 åäö", "héj 👋 åäö"},
		// The escape is what turns the rest of a sequence into a command. Dropping
		// it leaves inert text behind, rather than parsing sequences out.
		{"csi loses its escape", "hello \x1b[2Jworld", "hello [2Jworld"},
		{"osc loses its escape and terminator", "\x1b]52;c;aGk=\x07hi", "]52;c;aGk=hi"},
		{"del is dropped", "a\x7fb", "ab"},
		{"c1 is dropped", "a\u009bb", "ab"},
		{"newline becomes a space", "one\ntwo", "one two"},
		{"carriage return becomes a space", "one\rtwo", "one two"},
		{"tab becomes a space", "one\ttwo", "one two"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitize(tc.in); got != tc.want {
				t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeErrKeepsErrorIdentity(t *testing.T) {
	sentinel := errors.New("boom \x1b[2J")
	got := sanitizeErr(sentinel)

	if strings.ContainsRune(got.Error(), '\x1b') {
		t.Errorf("Error() = %q, still contains an escape", got.Error())
	}
	if !errors.Is(got, sentinel) {
		t.Error("errors.Is no longer finds the wrapped error")
	}
	// Identity, not equivalence, is the point: an error with nothing to strip
	// must come back untouched rather than wrapped.
	clean := errors.New("nothing to strip")
	if sanitizeErr(clean) != clean { //nolint:errorlint
		t.Error("an error with nothing to strip should be returned as-is")
	}
	if sanitizeErr(nil) != nil {
		t.Error("sanitizeErr(nil) should be nil")
	}
}

// A source is untrusted input. Supervise is the only path from a source to a
// sink, so this is the guarantee the renderers and the log rely on: what they
// receive can never be a terminal control sequence.
func TestSuperviseSanitizesUntrustedSourceOutput(t *testing.T) {
	c := &collector{}
	src := &fakeSource{fn: func(ctx context.Context, run int, sink Sink) error {
		sink.Message(Message{
			User:    "mall\x1b[31mory",
			Text:    "hi \x1b[2J\x07 there",
			Channel: "chan\x1bnel",
		})
		return Permanent(errors.New("done \x1b[2J"))
	}}

	Supervise(context.Background(), src, c, Retry{})

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(c.msgs))
	}
	got := c.msgs[0]
	for field, v := range map[string]string{"User": got.User, "Text": got.Text, "Channel": got.Channel} {
		if strings.ContainsRune(v, '\x1b') {
			t.Errorf("%s = %q, an escape reached the sink", field, v)
		}
	}
	if want := "hi [2J there"; got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}

	for _, st := range c.statuses {
		if st.Err != nil && strings.ContainsRune(st.Err.Error(), '\x1b') {
			t.Errorf("status error %q reached the sink with an escape", st.Err)
		}
	}
}
