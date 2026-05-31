package twitch

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
)

const host = "irc.chat.twitch.tv:6697"

type Source struct {
	channel string
}

func New(channel string) *Source {
	return &Source{channel: strings.ToLower(strings.TrimPrefix(channel, "#"))}
}

func (s *Source) Label() string { return "twitch:" + s.channel }

func (s *Source) Run(ctx context.Context, out chan<- chat.Message) error {
	dialer := &tls.Dialer{Config: &tls.Config{}}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	nick := fmt.Sprintf("justinfan%d", 10000+rand.Intn(89999))
	for _, w := range []string{
		"PASS SCHMOOPIIE",
		"NICK " + nick,
		"CAP REQ :twitch.tv/tags twitch.tv/commands",
		"JOIN #" + s.channel,
	} {
		if _, err := fmt.Fprintf(conn, "%s\r\n", w); err != nil {
			return fmt.Errorf("write %q: %w", w, err)
		}
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "PING ") {
			fmt.Fprintf(conn, "PONG %s\r\n", strings.TrimPrefix(line, "PING "))
			continue
		}
		if m, ok := parsePrivmsg(line, s.channel); ok {
			select {
			case out <- m:
			case <-ctx.Done():
				return nil
			}
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read: %w", err)
	}
	return nil
}

func parsePrivmsg(line, channel string) (chat.Message, bool) {
	tags := map[string]string{}
	if strings.HasPrefix(line, "@") {
		sp := strings.SplitN(line, " ", 2)
		if len(sp) < 2 {
			return chat.Message{}, false
		}
		for _, kv := range strings.Split(sp[0][1:], ";") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				tags[k] = v
			}
		}
		line = sp[1]
	}
	if !strings.HasPrefix(line, ":") {
		return chat.Message{}, false
	}
	prefix, rest, ok := strings.Cut(line[1:], " ")
	if !ok {
		return chat.Message{}, false
	}
	cmd, rest, ok := strings.Cut(rest, " ")
	if !ok || cmd != "PRIVMSG" {
		return chat.Message{}, false
	}
	_, msg, ok := strings.Cut(rest, " :")
	if !ok {
		return chat.Message{}, false
	}

	nick := prefix
	if i := strings.Index(prefix, "!"); i >= 0 {
		nick = prefix[:i]
	}
	if d := tags["display-name"]; d != "" {
		nick = d
	}

	action := false
	if strings.HasPrefix(msg, "\x01ACTION ") && strings.HasSuffix(msg, "\x01") {
		msg = strings.TrimSuffix(strings.TrimPrefix(msg, "\x01ACTION "), "\x01")
		action = true
	}

	return chat.Message{
		Platform: "twitch",
		Channel:  channel,
		Time:     time.Now(),
		User:     nick,
		Color:    tags["color"],
		Text:     msg,
		Action:   action,
	}, true
}
