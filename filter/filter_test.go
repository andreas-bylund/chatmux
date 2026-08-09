package filter

import (
	"testing"

	"github.com/andreas-bylund/chatmux/chat"
)

func msg(user, text string) chat.Message {
	return chat.Message{User: user, Text: text}
}

func TestHiding(t *testing.T) {
	f, err := New(Rules{
		HideCommands: true,
		HideUsers:    []string{"NightBot", " streamelements "},
		HidePatterns: []string{`(?i)buy followers`},
		MinLength:    2,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		m    chat.Message
		show bool
	}{
		{"normal message", msg("bob", "hello"), true},
		{"bot command", msg("bob", "!uptime"), false},
		{"hidden user, case-insensitive", msg("nightbot", "hi"), false},
		{"hidden user, whitespace trimmed in config", msg("StreamElements", "hi"), false},
		{"spam pattern", msg("bob", "BUY FOLLOWERS now"), false},
		{"too short", msg("bob", "k"), false},
		{"whitespace only", msg("bob", "   "), false},
		{"exactly min length", msg("bob", "ok"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, show := f.Apply(tc.m); show != tc.show {
				t.Errorf("Apply(%q by %q) show = %v, want %v", tc.m.Text, tc.m.User, show, tc.show)
			}
		})
	}
}

func TestMinLengthCountsRunesNotBytes(t *testing.T) {
	f, err := New(Rules{MinLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Four runes, seven bytes. Counting bytes would wrongly let it through a
	// longer threshold, and counting them here would wrongly hide it.
	if _, show := f.Apply(msg("bob", "hejå")); !show {
		t.Error("a four-rune message was hidden by a four-rune minimum")
	}
	if _, show := f.Apply(msg("bob", "åäö")); show {
		t.Error("a three-rune message passed a four-rune minimum")
	}
}

func TestHighlighting(t *testing.T) {
	f, err := New(Rules{
		HighlightWords:    []string{"Andreas", "godot"},
		HighlightUsers:    []string{"BestFriend"},
		HighlightPatterns: []string{`\bship it\b`},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		m    chat.Message
		want bool
	}{
		{"word, case-insensitive", msg("bob", "hej ANDREAS"), true},
		{"word as substring", msg("bob", "andreasson said hi"), true},
		{"second word", msg("bob", "using Godot 4"), true},
		{"user", msg("bestfriend", "anything"), true},
		{"pattern", msg("bob", "lgtm, ship it"), true},
		{"nothing matches", msg("bob", "just chatting"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, show := f.Apply(tc.m)
			if !show {
				t.Fatal("highlighting must never hide a message")
			}
			if got.Highlight != tc.want {
				t.Errorf("Highlight = %v, want %v", got.Highlight, tc.want)
			}
		})
	}
}

func TestToggleDisablesHidingButNotHighlighting(t *testing.T) {
	f, err := New(Rules{HideCommands: true, HighlightWords: []string{"look"}})
	if err != nil {
		t.Fatal(err)
	}

	if _, show := f.Apply(msg("bob", "!command")); show {
		t.Fatal("filter should start enabled")
	}

	if on := f.Toggle(); on {
		t.Error("Toggle should have returned false after disabling")
	}
	if _, show := f.Apply(msg("bob", "!command")); !show {
		t.Error("a disabled filter should show everything")
	}

	m, _ := f.Apply(msg("bob", "look at this"))
	if !m.Highlight {
		t.Error("highlighting should survive the toggle")
	}

	if on := f.Toggle(); !on {
		t.Error("Toggle should have returned true after re-enabling")
	}
	if _, show := f.Apply(msg("bob", "!command")); show {
		t.Error("re-enabling did not restore hiding")
	}
}

func TestActive(t *testing.T) {
	tests := []struct {
		name string
		r    Rules
		want bool
	}{
		{"nothing configured", Rules{}, false},
		{"highlighting only is not a hiding rule", Rules{HighlightWords: []string{"x"}}, false},
		{"hide commands", Rules{HideCommands: true}, true},
		{"hide users", Rules{HideUsers: []string{"bot"}}, true},
		{"hide patterns", Rules{HidePatterns: []string{"x"}}, true},
		{"min length", Rules{MinLength: 3}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := New(tc.r)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Active(); got != tc.want {
				t.Errorf("Active() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBadRegexpIsReported(t *testing.T) {
	if _, err := New(Rules{HidePatterns: []string{"("}}); err == nil {
		t.Error("an invalid hide pattern should be rejected")
	}
	if _, err := New(Rules{HighlightPatterns: []string{"["}}); err == nil {
		t.Error("an invalid highlight pattern should be rejected")
	}
}

func TestEmptyPatternsAreIgnored(t *testing.T) {
	f, err := New(Rules{HidePatterns: []string{"", "   "}})
	if err != nil {
		t.Fatal(err)
	}
	// An empty regexp matches everything; treating it as a rule would hide the
	// entire chat.
	if _, show := f.Apply(msg("bob", "hello")); !show {
		t.Error("blank patterns should not hide anything")
	}
}
