package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
	"github.com/charmbracelet/x/ansi"
)

const (
	testWidth  = 40
	testHeight = 5
)

func testPane(limit int) *pane {
	return newPane("twitch:test", "twitch", limit, &renderOpts{timestamps: false, showTag: false})
}

func fill(p *pane, n int, prefix string) {
	base := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		p.add(chat.Message{
			Platform: "twitch",
			Time:     base.Add(time.Duration(i) * time.Second),
			User:     "u",
			Text:     prefix + itoa(i),
		}, false)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// plain strips styling so assertions compare text, not escape codes.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func TestViewAlwaysFillsTheRequestedHeight(t *testing.T) {
	p := testPane(100)
	for _, n := range []int{0, 1, 3, 50} {
		p := p
		if n > 0 {
			p = testPane(100)
			fill(p, n, "m")
		}
		got := p.view(testWidth, testHeight)
		if len(got) != testHeight {
			t.Errorf("with %d messages: view returned %d lines, want %d", n, len(got), testHeight)
		}
	}
}

func TestPinnedViewShowsNewestMessages(t *testing.T) {
	p := testPane(100)
	fill(p, 20, "m")

	got := plain(p.view(testWidth, testHeight))
	if !strings.Contains(got[len(got)-1], "m19") {
		t.Errorf("bottom line = %q, want the newest message", got[len(got)-1])
	}
	if !strings.Contains(got[0], "m15") {
		t.Errorf("top line = %q, want m15", got[0])
	}
}

func TestScrollUpUnpinsAndScrollBackRepins(t *testing.T) {
	p := testPane(100)
	fill(p, 50, "m")

	if !p.pinned {
		t.Fatal("a fresh pane should be pinned to the bottom")
	}

	p.scroll(-3, testWidth, testHeight)
	if p.pinned {
		t.Fatal("scrolling up should unpin")
	}
	got := plain(p.view(testWidth, testHeight))
	if !strings.Contains(got[len(got)-1], "m46") {
		t.Errorf("after scrolling up 3, bottom = %q, want m46", got[len(got)-1])
	}

	p.scroll(3, testWidth, testHeight)
	if !p.pinned {
		t.Error("scrolling back to the bottom should re-pin")
	}
}

// The important scroll property: reading back through history must not be
// yanked around by new messages arriving.
func TestScrolledViewIsStableAsMessagesArrive(t *testing.T) {
	p := testPane(1000)
	fill(p, 40, "m")

	p.scroll(-10, testWidth, testHeight)
	before := plain(p.view(testWidth, testHeight))

	fill(p, 25, "later")

	after := plain(p.view(testWidth, testHeight))
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("line %d changed while scrolled up: %q became %q", i, before[i], after[i])
		}
	}
	if p.pinned {
		t.Error("new messages should not re-pin a scrolled pane")
	}
}

func TestPinnedViewFollowsNewMessages(t *testing.T) {
	p := testPane(1000)
	fill(p, 10, "m")
	fill(p, 1, "newest")

	got := plain(p.view(testWidth, testHeight))
	if !strings.Contains(got[len(got)-1], "newest0") {
		t.Errorf("bottom = %q, want the newest message", got[len(got)-1])
	}
}

func TestScrollClampsAtBothEnds(t *testing.T) {
	p := testPane(1000)
	fill(p, 30, "m")

	p.scroll(-10000, testWidth, testHeight)
	got := plain(p.view(testWidth, testHeight))
	if !strings.Contains(got[0], "m0") {
		t.Errorf("after scrolling far up, top = %q, want the oldest message", got[0])
	}

	p.scroll(10000, testWidth, testHeight)
	if !p.pinned {
		t.Error("scrolling far down should pin to the bottom")
	}
}

func TestToTopAndToBottom(t *testing.T) {
	p := testPane(1000)
	fill(p, 30, "m")

	p.toTop()
	if p.pinned {
		t.Error("toTop should unpin")
	}
	if got := plain(p.view(testWidth, testHeight)); !strings.Contains(got[0], "m0") {
		t.Errorf("toTop: top = %q, want m0", got[0])
	}

	p.toBottom()
	if !p.pinned {
		t.Error("toBottom should pin")
	}
}

func TestEvictionRespectsTheLimit(t *testing.T) {
	const limit = 100
	p := testPane(limit)
	fill(p, 1000, "m")

	// Trimming happens in batches, so allow the slack the batching introduces.
	if len(p.entries) > limit+limit/4+1 {
		t.Errorf("pane holds %d entries, want roughly %d", len(p.entries), limit)
	}
	if p.dropped == 0 {
		t.Error("nothing was evicted despite exceeding the limit")
	}
	if p.dropped+len(p.entries) != 1000 {
		t.Errorf("dropped(%d) + held(%d) = %d, want 1000", p.dropped, len(p.entries), p.dropped+len(p.entries))
	}

	// The newest message must survive eviction.
	if got := plain(p.view(testWidth, testHeight)); !strings.Contains(got[len(got)-1], "m999") {
		t.Errorf("bottom = %q, want m999", got[len(got)-1])
	}
}

func TestEvictionKeepsAScrolledAnchorValid(t *testing.T) {
	p := testPane(100)
	fill(p, 100, "m")
	p.toTop()

	// Push the anchored message out of the buffer entirely.
	fill(p, 500, "later")

	got := p.view(testWidth, testHeight)
	if len(got) != testHeight {
		t.Fatalf("view returned %d lines after eviction, want %d", len(got), testHeight)
	}
	if p.anchorMsg < p.dropped {
		t.Errorf("anchor %d points at an evicted message (dropped %d)", p.anchorMsg, p.dropped)
	}
}

func TestLongMessagesWrap(t *testing.T) {
	p := testPane(100)
	p.add(chat.Message{
		Platform: "twitch",
		Time:     time.Now(),
		User:     "u",
		Text:     strings.Repeat("word ", 40),
	}, false)

	lines := p.linesOf(0, testWidth)
	if len(lines) < 2 {
		t.Fatalf("a long message produced %d lines, want several", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > testWidth {
			t.Errorf("line %d is %d columns wide, want at most %d", i, w, testWidth)
		}
	}
}

// Wrapping must neither lose nor repeat words, at any width.
func TestWrappingPreservesTheMessageExactly(t *testing.T) {
	const text = "if you are the one playing the game u should know what happened son is a noob"

	for _, width := range []int{20, 33, 40, 57, 80, 200} {
		p := testPane(10)
		p.add(chat.Message{Time: time.Now(), User: "michaelklive", Text: text}, false)

		var got []string
		for _, l := range plain(p.linesOf(0, width)) {
			got = append(got, strings.Fields(l)...)
		}

		want := append([]string{"michaelklive:"}, strings.Fields(text)...)
		if len(got) != len(want) {
			t.Errorf("width %d: got %d words, want %d\n got: %v\nwant: %v", width, len(got), len(want), got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("width %d: word %d = %q, want %q", width, i, got[i], want[i])
			}
		}
	}
}

func TestResizeInvalidatesTheWrapCache(t *testing.T) {
	p := testPane(100)
	p.add(chat.Message{Time: time.Now(), User: "u", Text: strings.Repeat("word ", 20)}, false)

	narrow := len(p.linesOf(0, 20))
	wide := len(p.linesOf(0, 200))
	if narrow <= wide {
		t.Errorf("narrow render produced %d lines and wide produced %d; the cache did not rebuild", narrow, wide)
	}
}

func TestAbsLineAndAnchorAtRoundTrip(t *testing.T) {
	p := testPane(1000)
	fill(p, 30, "m")

	total := p.totalLines(testWidth)
	for abs := 0; abs < total; abs++ {
		msg, line := p.anchorAt(abs, testWidth)
		if got := p.absLine(msg, line, testWidth); got != abs {
			t.Fatalf("anchorAt(%d) -> (%d,%d) -> absLine %d", abs, msg, line, got)
		}
	}
}

func TestSystemNotesRender(t *testing.T) {
	p := testPane(100)
	p.add(chat.Message{Time: time.Now(), Text: "twitch:test reconnected"}, true)

	got := plain(p.view(testWidth, testHeight))
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "reconnected") {
		t.Errorf("system note missing from the view:\n%s", joined)
	}
}

func TestSystemNotesDoNotCountTowardsTheRate(t *testing.T) {
	p := testPane(100)
	now := time.Now()
	p.add(chat.Message{Time: now, Text: "note"}, true)
	if got := p.rate.perMinute(now); got != 0 {
		t.Errorf("rate = %d after only a system note, want 0", got)
	}
}
