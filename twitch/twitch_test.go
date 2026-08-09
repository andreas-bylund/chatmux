package twitch

import (
	"testing"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
)

func TestParseIRC(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		cmd    string
		nick   string
		params []string
		tags   map[string]string
	}{
		{
			name:   "privmsg with tags",
			line:   "@color=#FF0000;display-name=Bob :bob!bob@bob.tmi.twitch.tv PRIVMSG #chan :hello world",
			cmd:    "PRIVMSG",
			nick:   "bob",
			params: []string{"#chan", "hello world"},
			tags:   map[string]string{"color": "#FF0000", "display-name": "Bob"},
		},
		{
			name:   "no tags",
			line:   ":bob!bob@bob.tmi.twitch.tv PRIVMSG #chan :hi",
			cmd:    "PRIVMSG",
			nick:   "bob",
			params: []string{"#chan", "hi"},
		},
		{
			name:   "ping has no prefix",
			line:   "PING :tmi.twitch.tv",
			cmd:    "PING",
			params: []string{"tmi.twitch.tv"},
		},
		{
			name:   "trailing param keeps colons",
			line:   ":a!a@a PRIVMSG #c :look: http://x.example/y",
			cmd:    "PRIVMSG",
			nick:   "a",
			params: []string{"#c", "look: http://x.example/y"},
		},
		{
			name:   "numeric reply",
			line:   ":tmi.twitch.tv 366 justinfan1 #chan :End of /NAMES list",
			cmd:    "366",
			nick:   "tmi.twitch.tv",
			params: []string{"justinfan1", "#chan", "End of /NAMES list"},
		},
		{
			name:   "empty trailing param",
			line:   ":a!a@a PRIVMSG #c :",
			cmd:    "PRIVMSG",
			nick:   "a",
			params: []string{"#c", ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, ok := parseIRC(tc.line)
			if !ok {
				t.Fatalf("parseIRC(%q) returned not-ok", tc.line)
			}
			if l.cmd != tc.cmd {
				t.Errorf("cmd = %q, want %q", l.cmd, tc.cmd)
			}
			if tc.nick != "" && l.nick() != tc.nick {
				t.Errorf("nick = %q, want %q", l.nick(), tc.nick)
			}
			if len(l.params) != len(tc.params) {
				t.Fatalf("params = %q, want %q", l.params, tc.params)
			}
			for i, p := range tc.params {
				if l.params[i] != p {
					t.Errorf("param %d = %q, want %q", i, l.params[i], p)
				}
			}
			for k, v := range tc.tags {
				if l.tags[k] != v {
					t.Errorf("tag %q = %q, want %q", k, l.tags[k], v)
				}
			}
		})
	}
}

func TestParseIRCRejectsGarbage(t *testing.T) {
	for _, line := range []string{"", "@only-tags", ":only-prefix"} {
		if _, ok := parseIRC(line); ok {
			t.Errorf("parseIRC(%q) = ok, want not-ok", line)
		}
	}
}

func TestUnescapeTag(t *testing.T) {
	tests := map[string]string{
		`plain`:           "plain",
		`two\swords`:      "two words",
		`semi\:colon`:     "semi;colon",
		`back\\slash`:     `back\slash`,
		`trailing\`:       `trailing\`,
		`new\nline`:       "new\nline",
		`unknown\qescape`: "unknownqescape",
	}
	for in, want := range tests {
		if got := unescapeTag(in); got != want {
			t.Errorf("unescapeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMessage(t *testing.T) {
	s := New("chan")

	t.Run("uses display-name and colour", func(t *testing.T) {
		l, _ := parseIRC("@color=#00FF00;display-name=BobTheGreat;tmi-sent-ts=1700000000000 :bob!bob@bob PRIVMSG #chan :hi there")
		m, ok := s.message(l)
		if !ok {
			t.Fatal("message() returned not-ok")
		}
		if m.User != "BobTheGreat" {
			t.Errorf("User = %q, want BobTheGreat", m.User)
		}
		if m.Color != "#00FF00" {
			t.Errorf("Color = %q, want #00FF00", m.Color)
		}
		if m.Text != "hi there" {
			t.Errorf("Text = %q", m.Text)
		}
		if m.Channel != "chan" {
			t.Errorf("Channel = %q, want chan", m.Channel)
		}
		if got := m.Time.UnixMilli(); got != 1700000000000 {
			t.Errorf("Time = %d, want the tmi-sent-ts value", got)
		}
	})

	t.Run("falls back to the prefix nick", func(t *testing.T) {
		l, _ := parseIRC(":bob!bob@bob PRIVMSG #chan :hi")
		m, _ := s.message(l)
		if m.User != "bob" {
			t.Errorf("User = %q, want bob", m.User)
		}
	})

	t.Run("detects /me actions", func(t *testing.T) {
		l, _ := parseIRC(":bob!bob@bob PRIVMSG #chan :\x01ACTION waves\x01")
		m, _ := s.message(l)
		if !m.Action {
			t.Error("Action = false, want true")
		}
		if m.Text != "waves" {
			t.Errorf("Text = %q, want waves", m.Text)
		}
	})

	t.Run("drops empty messages", func(t *testing.T) {
		l, _ := parseIRC(":bob!bob@bob PRIVMSG #chan :")
		if _, ok := s.message(l); ok {
			t.Error("empty message accepted, want rejected")
		}
	})
}

func TestNoticeError(t *testing.T) {
	tests := []struct {
		line      string
		wantErr   bool
		permanent bool
	}{
		{"@msg-id=msg_channel_suspended :tmi.twitch.tv NOTICE #c :This channel is suspended.", true, true},
		{"@msg-id=msg_banned :tmi.twitch.tv NOTICE #c :You are banned.", true, true},
		{"@msg-id=whisper_restricted :tmi.twitch.tv NOTICE #c :nope", false, false},
		{":tmi.twitch.tv NOTICE * :Login authentication failed", true, true},
		{":tmi.twitch.tv NOTICE #c :Some routine notice", false, false},
	}

	for _, tc := range tests {
		l, ok := parseIRC(tc.line)
		if !ok {
			t.Fatalf("could not parse %q", tc.line)
		}
		err := noticeError(l)
		if (err != nil) != tc.wantErr {
			t.Errorf("noticeError(%q) = %v, wantErr %v", tc.line, err, tc.wantErr)
			continue
		}
		if tc.wantErr && chat.IsPermanent(err) != tc.permanent {
			t.Errorf("noticeError(%q): permanent = %v, want %v", tc.line, chat.IsPermanent(err), tc.permanent)
		}
	}
}

func TestNewNormalisesChannel(t *testing.T) {
	for _, in := range []string{"XQC", "#xqc", "#XQC"} {
		if got := New(in).Label(); got != "twitch:xqc" {
			t.Errorf("New(%q).Label() = %q, want twitch:xqc", in, got)
		}
	}
}
