// Package youtube reads a live stream's chat through the YouTube Data API v3.
//
// Unlike Twitch and Kick this is polling, not a push stream, and every call
// costs API quota — see pollPage for the arithmetic.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
)

const (
	apiBase = "https://www.googleapis.com/youtube/v3"

	// A transient failure should not cost us our page token — losing it means
	// re-resolving the chat and skipping whatever arrived in between.
	transientRetries = 4
	minPollInterval  = 2 * time.Second
	defaultPoll      = 5 * time.Second
)

// Source is one live stream's chat. It implements chat.Source.
type Source struct {
	videoID string
	apiKey  string
	http    *http.Client
}

// New returns a Source for target, which may be a video ID or a YouTube URL.
// The API key is read from YOUTUBE_API_KEY at construction time.
func New(target string) *Source {
	return &Source{
		videoID: extractVideoID(target),
		apiKey:  os.Getenv("YOUTUBE_API_KEY"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Label returns the "youtube:<video-id>" identifier for this source.
func (s *Source) Label() string { return "youtube:" + s.videoID }

// Platform returns "youtube".
func (s *Source) Platform() string { return "youtube" }

// Run polls the live chat into sink until it fails or ctx is done. Quota
// exhaustion, a rejected key and disabled chat are returned as permanent errors;
// a stream that has not started yet is not, so the caller can wait for it.
func (s *Source) Run(ctx context.Context, sink chat.Sink) error {
	if s.apiKey == "" {
		return chat.Permanent(errors.New("YOUTUBE_API_KEY not set"))
	}
	if s.videoID == "" {
		return chat.Permanent(errors.New("could not parse a video id from the target"))
	}

	liveChatID, err := s.resolveLiveChatID(ctx)
	if err != nil {
		return err
	}
	sink.Status(chat.Status{State: chat.StateConnected})

	var (
		pageToken string
		// The first page is the entire backlog. Replaying hours of chat on
		// startup is noise, so we take the page for its token and drop its
		// contents.
		skipBacklog = true
		fails       int
	)

	for {
		page, err := s.pollPage(ctx, liveChatID, pageToken)
		if err != nil {
			// A cancelled context is a clean shutdown, not a failure: the poll
			// error is just the cancellation surfacing through the HTTP client.
			if ctx.Err() != nil {
				return nil //nolint:nilerr
			}
			if chat.IsPermanent(err) || !isTransient(err) {
				return err
			}
			fails++
			if fails > transientRetries {
				return fmt.Errorf("%d consecutive failures: %w", fails, err)
			}
			// Back off in place so pageToken survives the blip.
			if !sleep(ctx, time.Duration(fails)*2*time.Second) {
				return nil
			}
			continue
		}
		fails = 0

		if !skipBacklog {
			for _, it := range page.Items {
				if m, ok := s.convert(it); ok {
					sink.Message(m)
				}
			}
		}
		skipBacklog = false
		pageToken = page.NextPageToken

		// The API tells us how long to wait; ignoring it wastes quota or gets
		// us rate limited.
		wait := time.Duration(page.PollingIntervalMillis) * time.Millisecond
		if wait <= 0 {
			wait = defaultPoll
		}
		if wait < minPollInterval {
			wait = minPollInterval
		}
		if !sleep(ctx, wait) {
			return nil
		}
	}
}

func (s *Source) resolveLiveChatID(ctx context.Context) (string, error) {
	var v struct {
		Items []struct {
			LiveStreamingDetails struct {
				ActiveLiveChatID string `json:"activeLiveChatId"`
			} `json:"liveStreamingDetails"`
		} `json:"items"`
	}
	q := url.Values{
		"part": {"liveStreamingDetails"},
		"id":   {s.videoID},
		"key":  {s.apiKey},
	}
	if err := s.get(ctx, apiBase+"/videos?"+q.Encode(), &v); err != nil {
		return "", fmt.Errorf("resolve live chat: %w", err)
	}
	if len(v.Items) == 0 {
		return "", chat.Permanent(fmt.Errorf("no such video %q", s.videoID))
	}
	if id := v.Items[0].LiveStreamingDetails.ActiveLiveChatID; id != "" {
		return id, nil
	}
	// Retryable on purpose: leaving the reader running before a scheduled
	// stream starts is a normal thing to want.
	return "", errors.New("video has no active live chat (not live yet?)")
}

type liveChatPage struct {
	PollingIntervalMillis int            `json:"pollingIntervalMillis"`
	NextPageToken         string         `json:"nextPageToken"`
	Items                 []liveChatItem `json:"items"`
}

type liveChatItem struct {
	Snippet struct {
		Type               string `json:"type"`
		PublishedAt        string `json:"publishedAt"`
		DisplayMessage     string `json:"displayMessage"`
		TextMessageDetails struct {
			MessageText string `json:"messageText"`
		} `json:"textMessageDetails"`
	} `json:"snippet"`
	AuthorDetails struct {
		DisplayName string `json:"displayName"`
		IsChatOwner bool   `json:"isChatOwner"`
		IsModerator bool   `json:"isChatModerator"`
	} `json:"authorDetails"`
}

// pollPage fetches one page of chat. It costs 5 quota units; against the
// default 10,000/day allowance that is roughly 2-3 hours of continuous polling.
func (s *Source) pollPage(ctx context.Context, liveChatID, pageToken string) (*liveChatPage, error) {
	q := url.Values{
		"liveChatId": {liveChatID},
		"part":       {"snippet,authorDetails"},
		"maxResults": {"500"},
		"key":        {s.apiKey},
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	var page liveChatPage
	if err := s.get(ctx, apiBase+"/liveChat/messages?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *Source) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return apiError(resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// apiError turns Google's error envelope into something actionable, and
// classifies whether retrying could ever help.
func apiError(status int, body []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)

	reason := ""
	if len(e.Error.Errors) > 0 {
		reason = e.Error.Errors[0].Reason
	}
	msg := e.Error.Message
	if msg == "" {
		msg = http.StatusText(status)
	}

	switch reason {
	case "quotaExceeded", "dailyLimitExceeded":
		return chat.Permanent(fmt.Errorf("API quota exhausted (resets at midnight Pacific): %s", msg))
	case "keyInvalid", "badRequest":
		return chat.Permanent(fmt.Errorf("invalid API key: %s", msg))
	case "ipRefererBlocked", "accessNotConfigured", "forbidden":
		return chat.Permanent(fmt.Errorf("API key rejected (%s): %s", reason, msg))
	case "liveChatDisabled":
		return chat.Permanent(fmt.Errorf("live chat is disabled for this stream"))
	case "rateLimitExceeded", "backendError", "internalError":
		return transientError{fmt.Errorf("%s: %s", reason, msg)}
	}

	if status >= 500 || status == http.StatusTooManyRequests {
		return transientError{fmt.Errorf("status %d: %s", status, msg)}
	}
	if reason != "" {
		return fmt.Errorf("%s: %s", reason, msg)
	}
	return fmt.Errorf("status %d: %s", status, msg)
}

// transientError marks a failure worth retrying in place, without discarding
// the page token.
type transientError struct{ error }

func (e transientError) Unwrap() error { return e.error }

func isTransient(err error) bool {
	var t transientError
	if errors.As(err, &t) {
		return true
	}
	// Anything http.Client.Do failed on — DNS blips, resets, timeouts — arrives
	// wrapped in *url.Error, and is worth another go. HTTP-level failures never
	// reach here; apiError has already classified those.
	var ue *url.Error
	if errors.As(err, &ue) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func (s *Source) convert(it liveChatItem) (chat.Message, bool) {
	text := it.Snippet.TextMessageDetails.MessageText
	if text == "" {
		text = it.Snippet.DisplayMessage
	}
	if text == "" {
		return chat.Message{}, false
	}
	if it.Snippet.Type != "" && it.Snippet.Type != "textMessageEvent" {
		return chat.Message{}, false
	}

	ts, err := time.Parse(time.RFC3339, it.Snippet.PublishedAt)
	if err != nil {
		ts = time.Now()
	}

	return chat.Message{
		Platform: "youtube",
		Channel:  s.videoID,
		Time:     ts,
		User:     it.AuthorDetails.DisplayName,
		Text:     text,
	}, true
}

// sleep waits for d, reporting false if the context was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func extractVideoID(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	if v := u.Query().Get("v"); v != "" {
		return v
	}
	switch {
	case u.Host == "youtu.be":
		return strings.Trim(u.Path, "/")
	case strings.HasPrefix(u.Path, "/live/"):
		return strings.Trim(strings.TrimPrefix(u.Path, "/live/"), "/")
	case strings.HasPrefix(u.Path, "/embed/"):
		return strings.Trim(strings.TrimPrefix(u.Path, "/embed/"), "/")
	}
	return s
}
