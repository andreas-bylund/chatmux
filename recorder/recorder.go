// Package recorder writes the chat stream to disk alongside the live display.
package recorder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
)

// flushInterval bounds how much is lost if the process is killed. Writes are
// buffered because a busy channel produces hundreds of lines a minute and one
// syscall each is wasteful.
const flushInterval = 2 * time.Second

// Options configures a Recorder. Dir must be set; Format defaults to nothing
// valid, so Open rejects a zero Options rather than silently writing nowhere.
type Options struct {
	Dir           string
	Format        string // "text", "jsonl" or "both"
	SplitBySource bool   // one file per source instead of one combined file
}

// Recorder implements chat.Sink by appending each message to a dated file.
// Files roll over at midnight local time.
type Recorder struct {
	dir       string
	text      bool
	jsonl     bool
	perSource bool

	mu    sync.Mutex
	files map[string]*logFile
	day   string
	errs  []error

	stop chan struct{}
	done chan struct{}
}

type logFile struct {
	f *os.File
	w *bufio.Writer
}

// Open creates the log directory if needed and starts the background flusher.
// The caller must Close the result to flush and release the open files.
func Open(o Options) (*Recorder, error) {
	if o.Dir == "" {
		return nil, fmt.Errorf("no log directory configured")
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", o.Dir, err)
	}

	r := &Recorder{
		dir:       o.Dir,
		text:      o.Format == "text" || o.Format == "both",
		jsonl:     o.Format == "jsonl" || o.Format == "both",
		perSource: o.SplitBySource,
		files:     map[string]*logFile{},
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	if !r.text && !r.jsonl {
		return nil, fmt.Errorf("unknown log format %q", o.Format)
	}

	go r.flushLoop()
	return r, nil
}

// Dir reports where the recorder is writing, for the startup banner.
func (r *Recorder) Dir() string { return r.dir }

// Message appends m to the file for its day, opening or rolling over files as
// needed. Write failures are collected rather than returned, so a full disk
// cannot stop the chat from being displayed; see Errors.
func (r *Recorder) Message(m chat.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	day := m.Time.Local().Format("2006-01-02")
	if day != r.day {
		r.closeAllLocked()
		r.day = day
	}

	base := day
	if r.perSource {
		base = day + "-" + sanitise(m.Source)
	}

	if r.text {
		if w := r.writerLocked(base + ".log"); w != nil {
			fmt.Fprintf(w, "%s [%s] %s: %s\n",
				m.Time.Local().Format("15:04:05"), m.Source, m.User, m.Text)
		}
	}
	if r.jsonl {
		if w := r.writerLocked(base + ".jsonl"); w != nil {
			if b, err := json.Marshal(m); err == nil {
				// bufio.Writer latches its first error, and Flush reports it, so
				// checking every write would report the same failure repeatedly.
				_, _ = w.Write(b)
				_ = w.WriteByte('\n')
			}
		}
	}
}

// Status is ignored: connection churn belongs in the UI, not in a chat log.
func (r *Recorder) Status(chat.Status) {}

func (r *Recorder) writerLocked(name string) *bufio.Writer {
	if lf, ok := r.files[name]; ok {
		return lf.w
	}
	path := filepath.Join(r.dir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		r.recordErrLocked(fmt.Errorf("open %s: %w", path, err))
		// Cache the failure as a nil entry so a broken disk does not mean one
		// failed syscall per message.
		r.files[name] = &logFile{}
		return nil
	}
	lf := &logFile{f: f, w: bufio.NewWriterSize(f, 32<<10)}
	r.files[name] = lf
	return lf.w
}

func (r *Recorder) recordErrLocked(err error) {
	if len(r.errs) < 8 {
		r.errs = append(r.errs, err)
	}
}

// Errors returns any write failures seen so far, so the caller can surface them
// after the TUI has released the terminal.
func (r *Recorder) Errors() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.errs...)
}

func (r *Recorder) flushLoop() {
	defer close(r.done)
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			r.Flush()
		case <-r.stop:
			return
		}
	}
}

// Flush writes buffered lines out to the open files. It runs on a timer; call
// it directly only when you need the log to be current right now.
func (r *Recorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, lf := range r.files {
		if lf.w != nil {
			if err := lf.w.Flush(); err != nil {
				r.recordErrLocked(err)
			}
		}
	}
}

// Close stops the flusher, flushes and closes every open file, and returns the
// first write error seen during the run, if any.
func (r *Recorder) Close() error {
	close(r.stop)
	<-r.done

	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeAllLocked()
	if len(r.errs) > 0 {
		return r.errs[0]
	}
	return nil
}

func (r *Recorder) closeAllLocked() {
	for name, lf := range r.files {
		if lf.w != nil {
			if err := lf.w.Flush(); err != nil {
				r.recordErrLocked(err)
			}
		}
		if lf.f != nil {
			if err := lf.f.Close(); err != nil {
				r.recordErrLocked(err)
			}
		}
		delete(r.files, name)
	}
}

// sanitise turns a source label like "twitch:xqc" into something safe for a
// filename on any filesystem.
func sanitise(s string) string {
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
