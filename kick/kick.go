// Package kick reads a Kick channel's chat over the Pusher WebSocket that
// kick.com's own web client uses.
//
// This is unofficial: Kick publishes no chat API, so the app key and event
// names below are lifted from the public web client and can change without
// notice.
package kick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
	"github.com/coder/websocket"
)

const (
	pusherAppKey  = "32cbd69e4b950bf97679"
	pusherCluster = "us2"

	chatMessageEvent = `App\Events\ChatMessageEvent`

	// Pusher closes idle connections. We ping well inside that window and treat
	// a long silence as a dead link.
	pingInterval = 60 * time.Second
	readTimeout  = 150 * time.Second

	// Kick sits behind Cloudflare and rejects obviously scripted clients.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// emoteRe matches Kick's inline emote markup, e.g. "[emote:37226:emojiPog]".
var emoteRe = regexp.MustCompile(`\[emote:\d+:([^\]]*)\]`)

// Source is one Kick channel's chat. It implements chat.Source.
type Source struct {
	slug string
	http *http.Client
}

// New returns a Source for the given channel slug, with or without a leading
// "@".
func New(slug string) *Source {
	return &Source{
		slug: strings.ToLower(strings.TrimPrefix(slug, "@")),
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// Label returns the "kick:<slug>" identifier for this source.
func (s *Source) Label() string { return "kick:" + s.slug }

// Platform returns "kick".
func (s *Source) Platform() string { return "kick" }

// Run subscribes to the channel's chatroom and streams messages into sink until
// the connection drops or ctx is done. Reconnecting is the caller's job; see
// chat.Supervise.
func (s *Source) Run(ctx context.Context, sink chat.Sink) error {
	if s.slug == "" {
		return chat.Permanent(errors.New("empty channel slug"))
	}

	chatroomID, err := s.resolveChatroomID(ctx)
	if err != nil {
		return fmt.Errorf("resolve chatroom: %w", err)
	}

	wsURL := fmt.Sprintf(
		"wss://ws-%s.pusher.com/app/%s?protocol=7&client=js&version=8.4.0&flash=false",
		pusherCluster, pusherAppKey,
	)
	// The handshake response body is owned by the library ("You never need to
	// close resp.Body yourself" — websocket.Dial), so it is discarded here.
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{ //nolint:bodyclose
		HTTPHeader: http.Header{"User-Agent": {userAgent}},
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 20)

	c := &pusherConn{conn: conn}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	channel := fmt.Sprintf("chatrooms.%d.v2", chatroomID)
	if err := c.send(ctx, fmt.Sprintf(
		`{"event":"pusher:subscribe","data":{"auth":"","channel":%q}}`, channel,
	)); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	go c.keepalive(ctx)

	for {
		data, err := c.read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("no traffic for %s", readTimeout)
			}
			return fmt.Errorf("read: %w", err)
		}

		var env struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		switch env.Event {
		case "pusher:ping":
			if err := c.send(ctx, `{"event":"pusher:pong","data":{}}`); err != nil {
				return fmt.Errorf("pong: %w", err)
			}

		case "pusher_internal:subscription_succeeded":
			sink.Status(chat.Status{State: chat.StateConnected})

		case "pusher:error":
			return fmt.Errorf("pusher error: %s", string(env.Data))

		case chatMessageEvent:
			if m, ok := s.message(env.Data); ok {
				sink.Message(m)
			}
		}
	}
}

func (s *Source) resolveChatroomID(ctx context.Context) (int64, error) {
	u := "https://kick.com/api/v2/channels/" + s.slug
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return 0, chat.Permanent(fmt.Errorf("no such channel %q", s.slug))
	case resp.StatusCode == http.StatusForbidden:
		// Cloudflare challenge — not something a retry loop fixes quickly, but
		// it does pass on its own sometimes, so keep it retryable.
		return 0, fmt.Errorf("blocked by Cloudflare (status %s)", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("status %s", resp.Status)
	}

	var v struct {
		Chatroom struct {
			ID int64 `json:"id"`
		} `json:"chatroom"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return 0, err
	}
	if v.Chatroom.ID == 0 {
		return 0, chat.Permanent(fmt.Errorf("channel %q has no chatroom", s.slug))
	}
	return v.Chatroom.ID, nil
}

// message decodes a ChatMessageEvent payload. Pusher double-encodes it: the
// envelope's "data" field is itself a JSON string containing the real object.
func (s *Source) message(raw json.RawMessage) (chat.Message, bool) {
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		return chat.Message{}, false
	}

	var msg struct {
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
		Sender    struct {
			Username string `json:"username"`
			Identity struct {
				Color string `json:"color"`
			} `json:"identity"`
		} `json:"sender"`
	}
	if err := json.Unmarshal([]byte(inner), &msg); err != nil {
		return chat.Message{}, false
	}
	if msg.Content == "" {
		return chat.Message{}, false
	}

	ts, err := time.Parse(time.RFC3339, msg.CreatedAt)
	if err != nil {
		ts = time.Now()
	}

	return chat.Message{
		Platform: "kick",
		Channel:  s.slug,
		Time:     ts,
		User:     msg.Sender.Username,
		Color:    msg.Sender.Identity.Color,
		Text:     emoteRe.ReplaceAllString(msg.Content, "$1"),
	}, true
}

// pusherConn serialises writes; coder/websocket allows only one writer at a
// time and the keepalive runs concurrently with the read loop.
type pusherConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (c *pusherConn) send(ctx context.Context, s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, []byte(s))
}

func (c *pusherConn) read(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	_, data, err := c.conn.Read(ctx)
	return data, err
}

func (c *pusherConn) keepalive(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.send(ctx, `{"event":"pusher:ping","data":{}}`); err != nil {
				return // the read loop will surface the same failure
			}
		}
	}
}
