package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
)

type Source struct {
	videoID string
	apiKey  string
	http    *http.Client
}

func New(target string) *Source {
	return &Source{
		videoID: extractVideoID(target),
		apiKey:  os.Getenv("YOUTUBE_API_KEY"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Source) Label() string { return "youtube:" + s.videoID }

func (s *Source) Run(ctx context.Context, out chan<- chat.Message) error {
	if s.apiKey == "" {
		return errors.New("YOUTUBE_API_KEY not set")
	}
	if s.videoID == "" {
		return errors.New("missing video id")
	}

	liveChatID, err := s.resolveLiveChatID(ctx)
	if err != nil {
		return fmt.Errorf("resolve live chat: %w", err)
	}

	pageToken := ""
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		items, next, wait, err := s.fetchPage(ctx, liveChatID, pageToken)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		if !first {
			for _, it := range items {
				if m, ok := convert(it, s.videoID); ok {
					select {
					case out <- m:
					case <-ctx.Done():
						return nil
					}
				}
			}
		}
		first = false
		pageToken = next
		if wait <= 0 {
			wait = 5 * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Source) resolveLiveChatID(ctx context.Context) (string, error) {
	u := "https://www.googleapis.com/youtube/v3/videos?" + url.Values{
		"part": {"liveStreamingDetails"},
		"id":   {s.videoID},
		"key":  {s.apiKey},
	}.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %s", resp.Status)
	}

	var v struct {
		Items []struct {
			LiveStreamingDetails struct {
				ActiveLiveChatID string `json:"activeLiveChatId"`
			} `json:"liveStreamingDetails"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if len(v.Items) == 0 || v.Items[0].LiveStreamingDetails.ActiveLiveChatID == "" {
		return "", errors.New("video has no active live chat (is it live?)")
	}
	return v.Items[0].LiveStreamingDetails.ActiveLiveChatID, nil
}

type liveChatItem struct {
	Snippet struct {
		Type                string `json:"type"`
		PublishedAt         string `json:"publishedAt"`
		DisplayMessage      string `json:"displayMessage"`
		TextMessageDetails  struct {
			MessageText string `json:"messageText"`
		} `json:"textMessageDetails"`
	} `json:"snippet"`
	AuthorDetails struct {
		DisplayName string `json:"displayName"`
	} `json:"authorDetails"`
}

func (s *Source) fetchPage(ctx context.Context, liveChatID, pageToken string) ([]liveChatItem, string, time.Duration, error) {
	q := url.Values{
		"liveChatId": {liveChatID},
		"part":       {"snippet,authorDetails"},
		"key":        {s.apiKey},
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	u := "https://www.googleapis.com/youtube/v3/liveChat/messages?" + q.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", 0, fmt.Errorf("status %s", resp.Status)
	}

	var v struct {
		PollingIntervalMillis int            `json:"pollingIntervalMillis"`
		NextPageToken         string         `json:"nextPageToken"`
		Items                 []liveChatItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, "", 0, err
	}
	return v.Items, v.NextPageToken, time.Duration(v.PollingIntervalMillis) * time.Millisecond, nil
}

func convert(it liveChatItem, videoID string) (chat.Message, bool) {
	if it.Snippet.Type != "textMessageEvent" && it.Snippet.DisplayMessage == "" {
		return chat.Message{}, false
	}
	text := it.Snippet.TextMessageDetails.MessageText
	if text == "" {
		text = it.Snippet.DisplayMessage
	}
	ts, _ := time.Parse(time.RFC3339, it.Snippet.PublishedAt)
	if ts.IsZero() {
		ts = time.Now()
	}
	return chat.Message{
		Platform: "youtube",
		Channel:  videoID,
		Time:     ts,
		User:     it.AuthorDetails.DisplayName,
		Text:     text,
	}, true
}

func extractVideoID(s string) string {
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
	if u.Host == "youtu.be" {
		return strings.TrimPrefix(u.Path, "/")
	}
	if strings.HasPrefix(u.Path, "/live/") {
		return strings.TrimPrefix(u.Path, "/live/")
	}
	return s
}
