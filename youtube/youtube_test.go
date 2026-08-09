package youtube

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/andreas-bylund/chatmux/chat"
)

func TestExtractVideoID(t *testing.T) {
	tests := map[string]string{
		"dQw4w9WgXcQ": "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":       "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s": "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                      "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?si=abc":               "dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ":          "dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ":         "dQw4w9WgXcQ",
		"  dQw4w9WgXcQ  ":                                   "dQw4w9WgXcQ",
	}
	for in, want := range tests {
		if got := extractVideoID(in); got != want {
			t.Errorf("extractVideoID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPIErrorClassification(t *testing.T) {
	body := func(reason string) []byte {
		return []byte(`{"error":{"code":403,"message":"boom","errors":[{"reason":"` + reason + `"}]}}`)
	}

	tests := []struct {
		name      string
		status    int
		body      []byte
		permanent bool
		transient bool
	}{
		{"quota exhausted is permanent", 403, body("quotaExceeded"), true, false},
		{"bad key is permanent", 400, body("keyInvalid"), true, false},
		{"blocked referer is permanent", 403, body("ipRefererBlocked"), true, false},
		{"chat disabled is permanent", 403, body("liveChatDisabled"), true, false},
		{"rate limit is transient", 403, body("rateLimitExceeded"), false, true},
		{"backend error is transient", 503, body("backendError"), false, true},
		{"bare 500 is transient", 500, []byte(`{}`), false, true},
		{"bare 429 is transient", 429, []byte(`{}`), false, true},
		{"chat ended is retryable but not transient", 404, body("liveChatEnded"), false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := apiError(tc.status, tc.body)
			if err == nil {
				t.Fatal("apiError returned nil")
			}
			if got := chat.IsPermanent(err); got != tc.permanent {
				t.Errorf("IsPermanent = %v, want %v (err: %v)", got, tc.permanent, err)
			}
			if got := isTransient(err); got != tc.transient {
				t.Errorf("isTransient = %v, want %v (err: %v)", got, tc.transient, err)
			}
		})
	}
}

func TestIsTransientNetworkErrors(t *testing.T) {
	// Anything http.Client.Do fails on arrives wrapped in *url.Error.
	err := &url.Error{Op: "Get", URL: "https://example.invalid", Err: errors.New("dial tcp: no such host")}
	if !isTransient(err) {
		t.Error("network failure should be transient")
	}
	if isTransient(errors.New("plain error")) {
		t.Error("an unclassified error should not be treated as transient")
	}
}

func TestConvert(t *testing.T) {
	s := New("vid123")

	t.Run("text message", func(t *testing.T) {
		var it liveChatItem
		it.Snippet.Type = "textMessageEvent"
		it.Snippet.PublishedAt = "2026-08-09T12:00:00Z"
		it.Snippet.TextMessageDetails.MessageText = "hello"
		it.AuthorDetails.DisplayName = "Someone"

		m, ok := s.convert(it)
		if !ok {
			t.Fatal("convert returned not-ok")
		}
		if m.User != "Someone" || m.Text != "hello" {
			t.Errorf("got %+v", m)
		}
		if m.Channel != "vid123" {
			t.Errorf("Channel = %q, want vid123", m.Channel)
		}
		if m.Time.UTC().Format("15:04") != "12:00" {
			t.Errorf("Time = %v, want the publishedAt value", m.Time)
		}
	})

	t.Run("non-text events are skipped", func(t *testing.T) {
		var it liveChatItem
		it.Snippet.Type = "superChatEvent"
		it.Snippet.DisplayMessage = "gave money"
		if _, ok := s.convert(it); ok {
			t.Error("superChatEvent accepted, want skipped")
		}
	})

	t.Run("empty text is skipped", func(t *testing.T) {
		var it liveChatItem
		it.Snippet.Type = "textMessageEvent"
		if _, ok := s.convert(it); ok {
			t.Error("empty message accepted, want skipped")
		}
	})
}

func TestRunRequiresConfiguration(t *testing.T) {
	t.Setenv("YOUTUBE_API_KEY", "")
	s := New("abc")
	err := s.Run(t.Context(), chat.SinkFunc{})
	if !chat.IsPermanent(err) {
		t.Errorf("missing API key should be a permanent error, got %v", err)
	}
}

func TestStatusTextFallback(t *testing.T) {
	// An error body Google did not fill in should still produce something
	// readable rather than an empty message.
	err := apiError(http.StatusTeapot, []byte("not json"))
	if err == nil || err.Error() == "" {
		t.Fatalf("apiError produced %v", err)
	}
}
