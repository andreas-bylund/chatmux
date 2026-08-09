// Package twitch reads a Twitch channel's chat over anonymous IRC.
//
// No token is needed: Twitch accepts any "justinfan<digits>" nick as a
// read-only anonymous login.
package twitch

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
)

const (
	host = "irc.chat.twitch.tv:6697"

	// Twitch pings every ~5 minutes, which is far too slow to notice a dead
	// connection. We ping first and expect a PONG well inside readTimeout, so a
	// silently dropped TCP connection surfaces in ~2 minutes instead of never.
	keepaliveInterval = 60 * time.Second
	readTimeout       = 150 * time.Second
	dialTimeout       = 20 * time.Second
)

// errReconnect is returned when Twitch asks us to reconnect, or when the
// connection goes quiet. Both are ordinary, retryable conditions.
var errReconnect = errors.New("twitch asked us to reconnect")

// Source is one Twitch channel's chat. It implements chat.Source.
type Source struct {
	channel string
}

// New returns a Source for the named channel, with or without a leading "#".
func New(channel string) *Source {
	return &Source{channel: strings.ToLower(strings.TrimPrefix(channel, "#"))}
}

// Label returns the "twitch:<channel>" identifier for this source.
func (s *Source) Label() string { return "twitch:" + s.channel }

// Platform returns "twitch".
func (s *Source) Platform() string { return "twitch" }

// Run joins the channel and streams messages into sink until the connection
// drops or ctx is done. Reconnecting is the caller's job; see chat.Supervise.
func (s *Source) Run(ctx context.Context, sink chat.Sink) error {
	if s.channel == "" {
		return chat.Permanent(errors.New("empty channel name"))
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	defer cancelDial()

	dialer := &tls.Dialer{Config: &tls.Config{ServerName: "irc.chat.twitch.tv"}}
	conn, err := dialer.DialContext(dialCtx, "tcp", host)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Closing the conn is how we unblock a read that is parked in the kernel;
	// cancelling the context alone would not reach it.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	c := &ircConn{conn: conn}

	nick := fmt.Sprintf("justinfan%d", 10000+rand.Intn(89999))
	for _, line := range []string{
		"PASS SCHMOOPIIE",
		"NICK " + nick,
		"CAP REQ :twitch.tv/tags twitch.tv/commands",
		"JOIN #" + s.channel,
	} {
		if err := c.send(line); err != nil {
			return fmt.Errorf("handshake: %w", err)
		}
	}

	go c.keepalive(ctx)

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	joined := false
	for {
		// Refreshed before every line: any traffic at all proves the link is up.
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("set deadline: %w", err)
		}
		if !sc.Scan() {
			break
		}

		l, ok := parseIRC(sc.Text())
		if !ok {
			continue
		}

		switch l.cmd {
		case "PING":
			if err := c.send("PONG :" + l.param(0)); err != nil {
				return fmt.Errorf("pong: %w", err)
			}

		case "PONG":
			// Keepalive answered; the deadline refresh above is the whole point.

		case "JOIN", "366": // 366 = end of NAMES, sent right after a join
			if !joined {
				joined = true
				sink.Status(chat.Status{State: chat.StateConnected})
			}

		case "RECONNECT":
			return errReconnect

		case "NOTICE":
			if err := noticeError(l); err != nil {
				return err
			}

		case "PRIVMSG":
			if m, ok := s.message(l); ok {
				sink.Message(m)
			}
		}
	}

	err = sc.Err()
	if ctx.Err() != nil {
		return nil
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return fmt.Errorf("no traffic for %s", readTimeout)
	}
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return errors.New("connection closed by server")
}

// noticeError turns the NOTICE messages that mean "this will never work" into
// permanent errors, and lets everything else pass.
func noticeError(l ircLine) error {
	switch l.tags["msg-id"] {
	case "msg_channel_suspended":
		return chat.Permanent(errors.New("channel is suspended"))
	case "msg_banned":
		return chat.Permanent(errors.New("banned from this channel"))
	case "msg_room_not_found":
		return chat.Permanent(errors.New("channel not found"))
	}
	// The tags capability is only active after CAP REQ is acknowledged, so the
	// login failure that arrives before that has no msg-id to match on.
	if strings.Contains(strings.ToLower(l.param(1)), "login authentication failed") {
		return chat.Permanent(errors.New("login rejected"))
	}
	return nil
}

func (s *Source) message(l ircLine) (chat.Message, bool) {
	text := l.param(1)
	if text == "" {
		return chat.Message{}, false
	}

	nick := l.nick()
	if d := l.tags["display-name"]; d != "" {
		nick = d
	}

	action := false
	if strings.HasPrefix(text, "\x01ACTION ") && strings.HasSuffix(text, "\x01") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "\x01ACTION "), "\x01")
		action = true
	}

	ts := time.Now()
	if tmi := l.tags["tmi-sent-ts"]; tmi != "" {
		if ms, err := strconv.ParseInt(tmi, 10, 64); err == nil {
			ts = time.UnixMilli(ms)
		}
	}

	return chat.Message{
		Platform: "twitch",
		Channel:  strings.TrimPrefix(l.param(0), "#"),
		Time:     ts,
		User:     nick,
		Color:    l.tags["color"],
		Text:     text,
		Action:   action,
	}, true
}

// ircConn serialises writes: the keepalive goroutine and the read loop both
// send, and interleaved writes would corrupt the protocol.
type ircConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (c *ircConn) send(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	_, err := c.conn.Write([]byte(line + "\r\n"))
	return err
}

func (c *ircConn) keepalive(ctx context.Context) {
	t := time.NewTicker(keepaliveInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.send("PING :keepalive"); err != nil {
				return // the read loop will notice the same failure
			}
		}
	}
}
