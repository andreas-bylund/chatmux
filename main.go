package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
	"github.com/andreas-bylund/Stream-chat-terminal/kick"
	"github.com/andreas-bylund/Stream-chat-terminal/twitch"
	"github.com/andreas-bylund/Stream-chat-terminal/youtube"
)

const (
	ansiReset = "\033[0m"
	ansiDim   = "\033[2m"
	ansiBold  = "\033[1m"
)

var fallbackColors = []string{
	"\033[38;5;203m", "\033[38;5;208m", "\033[38;5;220m",
	"\033[38;5;82m", "\033[38;5;45m", "\033[38;5;75m",
	"\033[38;5;141m", "\033[38;5;213m", "\033[38;5;199m",
	"\033[38;5;156m", "\033[38;5;117m", "\033[38;5;186m",
}

var platformTags = map[string]string{
	"twitch":  "\033[48;5;55m\033[97m T \033[0m",
	"youtube": "\033[48;5;160m\033[97m Y \033[0m",
	"kick":    "\033[48;5;34m\033[30m K \033[0m",
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: %s <source> [<source>...]

Each source is "<platform>:<target>":
  twitch:<channel>          e.g. twitch:xqc
  youtube:<video-id|url>    e.g. youtube:dQw4w9WgXcQ  (needs YOUTUBE_API_KEY)
  kick:<channel>            e.g. kick:trainwreckstv
`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	sources := make([]chat.Source, 0, flag.NArg())
	for _, a := range flag.Args() {
		s, err := parseSource(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", a, err)
			os.Exit(2)
		}
		sources = append(sources, s)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	out := make(chan chat.Message, 256)
	var wg sync.WaitGroup
	for _, s := range sources {
		wg.Add(1)
		go func(s chat.Source) {
			defer wg.Done()
			fmt.Printf("%s── %s ──%s\n", ansiDim, s.Label(), ansiReset)
			if err := s.Run(ctx, out); err != nil && ctx.Err() == nil {
				log.Printf("[%s] %v", s.Label(), err)
			}
		}(s)
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	for m := range out {
		printMessage(m)
	}
}

func parseSource(s string) (chat.Source, error) {
	platform, target, ok := strings.Cut(s, ":")
	if !ok || target == "" {
		return nil, fmt.Errorf("expected platform:target")
	}
	switch platform {
	case "twitch":
		return twitch.New(target), nil
	case "youtube":
		return youtube.New(target), nil
	case "kick":
		return kick.New(target), nil
	default:
		return nil, fmt.Errorf("unknown platform %q", platform)
	}
}

func printMessage(m chat.Message) {
	tag := platformTags[m.Platform]
	if tag == "" {
		tag = "[" + m.Platform + "]"
	}
	ts := m.Time.Local().Format("15:04")
	nickColor := userColor(m.User, m.Color)
	if m.Action {
		fmt.Printf("%s %s%s%s %s* %s %s%s\n", tag, ansiDim, ts, ansiReset, nickColor, m.User, m.Text, ansiReset)
	} else {
		fmt.Printf("%s %s%s%s %s%s%s: %s\n", tag, ansiDim, ts, ansiReset, nickColor, m.User, ansiReset, m.Text)
	}
}

func userColor(nick, color string) string {
	if len(color) == 7 && color[0] == '#' {
		var r, g, b int
		if _, err := fmt.Sscanf(color, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return fmt.Sprintf("%s\033[38;2;%d;%d;%dm", ansiBold, r, g, b)
		}
	}
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(nick)))
	return ansiBold + fallbackColors[int(h.Sum32())%len(fallbackColors)]
}
