package kick

import (
	"encoding/json"
	"testing"
)

// pusherData wraps an object the way Pusher does: the envelope's "data" field
// is a JSON *string* containing the real payload.
func pusherData(t *testing.T, inner string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMessage(t *testing.T) {
	s := New("somechannel")

	t.Run("decodes a normal message", func(t *testing.T) {
		raw := pusherData(t, `{
			"content": "hello there",
			"created_at": "2026-08-09T18:04:05Z",
			"sender": {"username": "Someone", "identity": {"color": "#00FF00"}}
		}`)
		m, ok := s.message(raw)
		if !ok {
			t.Fatal("message() returned not-ok")
		}
		if m.User != "Someone" {
			t.Errorf("User = %q, want Someone", m.User)
		}
		if m.Text != "hello there" {
			t.Errorf("Text = %q", m.Text)
		}
		if m.Color != "#00FF00" {
			t.Errorf("Color = %q", m.Color)
		}
		if m.Channel != "somechannel" {
			t.Errorf("Channel = %q, want somechannel", m.Channel)
		}
		if m.Time.UTC().Format("15:04:05") != "18:04:05" {
			t.Errorf("Time = %v, want the created_at value", m.Time)
		}
	})

	t.Run("strips emote markup", func(t *testing.T) {
		raw := pusherData(t, `{
			"content": "nice [emote:37226:emojiPog] play [emote:1:KEKW]",
			"sender": {"username": "a"}
		}`)
		m, ok := s.message(raw)
		if !ok {
			t.Fatal("message() returned not-ok")
		}
		if want := "nice emojiPog play KEKW"; m.Text != want {
			t.Errorf("Text = %q, want %q", m.Text, want)
		}
	})

	t.Run("falls back to now on a bad timestamp", func(t *testing.T) {
		raw := pusherData(t, `{"content":"x","created_at":"not-a-date","sender":{"username":"a"}}`)
		m, ok := s.message(raw)
		if !ok {
			t.Fatal("message() returned not-ok")
		}
		if m.Time.IsZero() {
			t.Error("Time is zero, want a fallback to now")
		}
	})

	t.Run("rejects empty and malformed payloads", func(t *testing.T) {
		cases := []json.RawMessage{
			pusherData(t, `{"content":"","sender":{"username":"a"}}`),
			pusherData(t, `not json at all`),
			json.RawMessage(`{"not":"a string"}`),
		}
		for i, raw := range cases {
			if _, ok := s.message(raw); ok {
				t.Errorf("case %d: accepted, want rejected", i)
			}
		}
	})
}

func TestNewNormalisesSlug(t *testing.T) {
	for _, in := range []string{"TrainwrecksTV", "@trainwreckstv", "@TrainwrecksTV"} {
		if got := New(in).Label(); got != "kick:trainwreckstv" {
			t.Errorf("New(%q).Label() = %q, want kick:trainwreckstv", in, got)
		}
	}
}

func TestEmoteRegexLeavesOtherBracketsAlone(t *testing.T) {
	in := "[not an emote] [emote:5:Pog] [emote:bad:x]"
	want := "[not an emote] Pog [emote:bad:x]"
	if got := emoteRe.ReplaceAllString(in, "$1"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
