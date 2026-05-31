package kick

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
	"github.com/coder/websocket"
)

// Pusher app credentials used by kick.com (extracted from public web client).
// Inofficiellt — kan ändras av Kick när som helst.
const (
	pusherAppKey  = "32cbd69e4b950bf97679"
	pusherCluster = "us2"
)

type Source struct {
	slug string
	http *http.Client
}

func New(slug string) *Source {
	return &Source{
		slug: strings.ToLower(strings.TrimPrefix(slug, "@")),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Source) Label() string { return "kick:" + s.slug }

func (s *Source) Run(ctx context.Context, out chan<- chat.Message) error {
	chatroomID, err := s.resolveChatroomID(ctx)
	if err != nil {
		return fmt.Errorf("resolve chatroom: %w", err)
	}

	wsURL := fmt.Sprintf(
		"wss://ws-%s.pusher.com/app/%s?protocol=7&client=js&version=8.4.0&flash=false",
		pusherCluster, pusherAppKey,
	)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	sub := fmt.Sprintf(
		`{"event":"pusher:subscribe","data":{"auth":"","channel":"chatrooms.%d.v2"}}`,
		chatroomID,
	)
	if err := conn.Write(ctx, websocket.MessageText, []byte(sub)); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		if m, ok := parseEvent(data, s.slug); ok {
			select {
			case out <- m:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (s *Source) resolveChatroomID(ctx context.Context) (int64, error) {
	u := "https://kick.com/api/v2/channels/" + s.slug
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 stream-chat-terminal")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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
		return 0, fmt.Errorf("no chatroom id for %q", s.slug)
	}
	return v.Chatroom.ID, nil
}

func parseEvent(data []byte, slug string) (chat.Message, bool) {
	var env struct {
		Event string `json:"event"`
		Data  string `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return chat.Message{}, false
	}
	if env.Event != "App\\Events\\ChatMessageEvent" {
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
	if err := json.Unmarshal([]byte(env.Data), &msg); err != nil {
		return chat.Message{}, false
	}
	ts, _ := time.Parse(time.RFC3339, msg.CreatedAt)
	if ts.IsZero() {
		ts = time.Now()
	}
	return chat.Message{
		Platform: "kick",
		Channel:  slug,
		Time:     ts,
		User:     msg.Sender.Username,
		Color:    msg.Sender.Identity.Color,
		Text:     msg.Content,
	}, true
}
