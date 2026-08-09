// Command stream-chat streams live chat from Twitch, YouTube and Kick into the
// terminal, multiplexed into one view.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
	"github.com/andreas-bylund/Stream-chat-terminal/config"
	"github.com/andreas-bylund/Stream-chat-terminal/filter"
	"github.com/andreas-bylund/Stream-chat-terminal/kick"
	"github.com/andreas-bylund/Stream-chat-terminal/recorder"
	"github.com/andreas-bylund/Stream-chat-terminal/twitch"
	"github.com/andreas-bylund/Stream-chat-terminal/ui"
	"github.com/andreas-bylund/Stream-chat-terminal/youtube"
	"github.com/mattn/go-isatty"
)

// shutdownGrace is how long we wait for sources to unwind before exiting
// anyway. A source stuck in a syscall should not keep the process alive.
const shutdownGrace = 3 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "stream-chat:", err)
		os.Exit(1)
	}
}

type opts struct {
	configPath  string
	initConfig  bool
	listPresets bool
	plain       bool
	unified     bool
	noLog       bool
	log         bool
	showVersion bool
}

func run() error {
	var o opts
	flag.StringVar(&o.configPath, "config", config.Path(), "path to the config file")
	flag.BoolVar(&o.initConfig, "init-config", false, "write a starter config file and exit")
	flag.BoolVar(&o.listPresets, "list-presets", false, "list configured presets and exit")
	flag.BoolVar(&o.plain, "plain", false, "stream plain lines to stdout instead of the full-screen interface")
	flag.BoolVar(&o.unified, "unified", false, "merge all sources into one column (overrides ui.mode)")
	flag.BoolVar(&o.noLog, "no-log", false, "disable file logging for this run")
	flag.BoolVar(&o.log, "log", false, "enable file logging for this run")
	flag.BoolVar(&o.showVersion, "version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()

	switch {
	case o.showVersion:
		fmt.Println(versionString())
		return nil
	case o.initConfig:
		if err := config.WriteTemplate(o.configPath); err != nil {
			return err
		}
		fmt.Println("wrote", o.configPath)
		return nil
	}

	cfg, err := config.Load(o.configPath)
	if err != nil {
		return err
	}

	if o.listPresets {
		return listPresets(cfg)
	}
	if flag.NArg() == 0 {
		flag.Usage()
		return errors.New("no sources given")
	}
	if o.noLog && o.log {
		return errors.New("-log and -no-log are mutually exclusive")
	}

	specs, err := resolveArgs(flag.Args(), cfg)
	if err != nil {
		return err
	}
	sources, err := buildSources(specs)
	if err != nil {
		return err
	}

	f, err := filter.New(filter.Rules{
		HideCommands:      cfg.Filter.HideCommands,
		HideUsers:         cfg.Filter.HideUsers,
		HidePatterns:      cfg.Filter.HidePatterns,
		MinLength:         cfg.Filter.MinLength,
		HighlightWords:    cfg.Highlight.Words,
		HighlightUsers:    cfg.Highlight.Users,
		HighlightPatterns: cfg.Highlight.Patterns,
	})
	if err != nil {
		return err
	}

	// A TUI needs a terminal; anything else has to fall back to plain lines.
	plain := o.plain || !isatty.IsTerminal(os.Stdout.Fd())

	var rec *recorder.Recorder
	if wantLog(cfg, o) {
		rec, err = recorder.Open(recorder.Options{
			Dir:           cfg.Log.Dir,
			Format:        cfg.Log.Format,
			SplitBySource: cfg.Log.SplitBySource,
		})
		if err != nil {
			return fmt.Errorf("logging: %w", err)
		}
		defer rec.Close()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if plain {
		return runPlain(ctx, cfg, sources, f, rec)
	}
	return runTUI(ctx, cfg, o, sources, f, rec)
}

func runTUI(ctx context.Context, cfg config.Config, o opts, sources []chat.Source, f *filter.Filter, rec *recorder.Recorder) error {
	infos := make([]ui.SourceInfo, len(sources))
	for i, s := range sources {
		infos[i] = ui.SourceInfo{Label: s.Label(), Platform: s.Platform()}
	}

	prog := ui.New(ui.Options{
		Sources:    infos,
		Unified:    o.unified || cfg.UI.Mode == "unified",
		Timestamps: cfg.UI.Timestamps,
		Compact:    cfg.UI.Compact,
		Scrollback: cfg.UI.Scrollback,
		Filter:     f,
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg := startSources(ctx, sources, sinkFor(f, prog.Sink(), rec), cfg)

	// A signal should close the interface; quitting the interface should stop
	// the sources. Wire both directions.
	go func() {
		<-ctx.Done()
		prog.Quit()
	}()

	err := prog.Run()
	cancel()
	waitFor(wg, shutdownGrace)

	// The terminal is ours again, so anything worth reporting can be printed.
	if dropped := prog.Dropped(); dropped > 0 {
		fmt.Fprintf(os.Stderr, "stream-chat: dropped %d messages the display could not keep up with\n", dropped)
	}
	reportRecorder(rec)
	return err
}

func runPlain(ctx context.Context, cfg config.Config, sources []chat.Source, f *filter.Filter, rec *recorder.Recorder) error {
	out := newPlainSink(os.Stdout, os.Stderr, cfg.UI.Timestamps)

	for _, s := range sources {
		fmt.Fprintln(os.Stderr, plainNote.Render("── "+s.Label()))
	}

	wg := startSources(ctx, sources, sinkFor(f, out, rec), cfg)
	wg.Wait()

	out.Flush()
	reportRecorder(rec)
	return nil
}

// sinkFor assembles the pipeline: filter first, then fan out to the display and
// the log. Hidden messages reach neither, so the log matches what you saw.
func sinkFor(f *filter.Filter, display chat.Sink, rec *recorder.Recorder) chat.Sink {
	out := display
	if rec != nil {
		out = chat.MultiSink{display, rec}
	}
	return filtered{f: f, next: out}
}

type filtered struct {
	f    *filter.Filter
	next chat.Sink
}

func (s filtered) Message(m chat.Message) {
	if m, ok := s.f.Apply(m); ok {
		s.next.Message(m)
	}
}

func (s filtered) Status(st chat.Status) { s.next.Status(st) }

func startSources(ctx context.Context, sources []chat.Source, sink chat.Sink, cfg config.Config) *sync.WaitGroup {
	retry := chat.Retry{
		Initial:     cfg.Retry.Initial.Std(),
		Max:         cfg.Retry.Max.Std(),
		Factor:      cfg.Retry.Factor,
		ResetAfter:  cfg.Retry.ResetAfter.Std(),
		MaxAttempts: cfg.Retry.MaxAttempts,
	}

	var wg sync.WaitGroup
	for _, s := range sources {
		wg.Add(1)
		go func(s chat.Source) {
			defer wg.Done()
			chat.Supervise(ctx, s, sink, retry)
		}(s)
	}
	return &wg
}

// waitFor blocks until wg is done or the grace period expires.
func waitFor(wg *sync.WaitGroup, grace time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
	}
}

func reportRecorder(rec *recorder.Recorder) {
	if rec == nil {
		return
	}
	for _, err := range rec.Errors() {
		fmt.Fprintln(os.Stderr, "stream-chat: log:", err)
	}
}

func wantLog(cfg config.Config, o opts) bool {
	switch {
	case o.noLog:
		return false
	case o.log:
		return true
	}
	return cfg.Log.Enabled
}

// resolveArgs expands preset names into their source lists. An argument
// containing ":" is a source; anything else must name a preset.
func resolveArgs(args []string, cfg config.Config) ([]string, error) {
	var out []string
	for _, a := range args {
		if strings.Contains(a, ":") {
			out = append(out, a)
			continue
		}
		sources, ok := cfg.Preset(a)
		if !ok {
			names := cfg.PresetNames()
			if len(names) == 0 {
				return nil, fmt.Errorf("%q is not a source (want platform:target) and no presets are configured", a)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("unknown preset %q; configured presets: %s", a, strings.Join(names, ", "))
		}
		out = append(out, sources...)
	}
	return out, nil
}

func buildSources(specs []string) ([]chat.Source, error) {
	seen := make(map[string]bool, len(specs))
	sources := make([]chat.Source, 0, len(specs))

	for _, spec := range specs {
		s, err := parseSource(spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec, err)
		}
		// Two connections to the same channel would just double every line.
		if seen[s.Label()] {
			continue
		}
		seen[s.Label()] = true
		sources = append(sources, s)
	}
	if len(sources) == 0 {
		return nil, errors.New("no sources to connect to")
	}
	return sources, nil
}

func parseSource(s string) (chat.Source, error) {
	platform, target, ok := strings.Cut(s, ":")
	if !ok {
		return nil, errors.New("expected platform:target")
	}
	// A YouTube URL target contains "://", so only the first field is the
	// platform and the rest must be kept intact.
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("empty target")
	}

	switch strings.ToLower(platform) {
	case "twitch":
		return twitch.New(target), nil
	case "youtube", "yt":
		return youtube.New(target), nil
	case "kick":
		return kick.New(target), nil
	default:
		return nil, fmt.Errorf("unknown platform %q (want twitch, youtube or kick)", platform)
	}
}

func listPresets(cfg config.Config) error {
	names := cfg.PresetNames()
	if len(names) == 0 {
		fmt.Printf("no presets configured in %s\n", config.Path())
		return nil
	}
	sort.Strings(names)
	for _, n := range names {
		sources, _ := cfg.Preset(n)
		fmt.Printf("%-16s %s\n", n, strings.Join(sources, " "))
	}
	return nil
}

// version is stamped into release builds with
// -ldflags "-X main.version=v1.2.3". Left empty, the version recorded by the Go
// toolchain is used instead, which is what "go install" produces.
var version string

func versionString() string {
	if version != "" {
		return "stream-chat " + version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "stream-chat (unknown version)"
	}
	return "stream-chat " + info.Main.Version
}

func usage() {
	fmt.Fprintf(os.Stderr, `stream-chat — live chat from Twitch, YouTube and Kick in the terminal

usage: %s [flags] <source|preset>...

Each source is "<platform>:<target>":
  twitch:<channel>           twitch:xqc
  youtube:<video-id|url>     youtube:dQw4w9WgXcQ    (needs YOUTUBE_API_KEY)
  kick:<channel>             kick:trainwreckstv

An argument without ":" is looked up as a preset from the config file.

flags:
`, os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nconfig: %s (create one with -init-config)\n", config.Path())
}
