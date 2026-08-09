package recorder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
)

func at(day, hour int) time.Time {
	return time.Date(2026, 8, day, hour, 4, 5, 0, time.Local)
}

func sample(day, hour int, source, user, text string) chat.Message {
	return chat.Message{
		Platform: "twitch",
		Source:   source,
		Channel:  "test",
		Time:     at(day, hour),
		User:     user,
		Text:     text,
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

func ls(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name()
	}
	return names
}

func TestTextFormat(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	r.Message(sample(9, 20, "twitch:xqc", "Bob", "hello there"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	got := read(t, dir, "2026-08-09.log")
	for _, want := range []string{"20:04:05", "twitch:xqc", "Bob", "hello there"} {
		if !strings.Contains(got, want) {
			t.Errorf("log line %q is missing %q", strings.TrimSpace(got), want)
		}
	}
}

func TestJSONLFormatRoundTrips(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	want := sample(9, 20, "kick:someone", "Bob", "hello")
	want.Highlight = true
	r.Message(want)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(read(t, dir, "2026-08-09.jsonl"))
	var got chat.Message
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("the recorded line is not valid JSON: %v\n%s", err, line)
	}
	if got.User != want.User || got.Text != want.Text || got.Source != want.Source {
		t.Errorf("round trip lost data: %+v", got)
	}
	if !got.Highlight {
		t.Error("Highlight did not survive the round trip")
	}
	if !got.Time.Equal(want.Time) {
		t.Errorf("Time = %v, want %v", got.Time, want.Time)
	}
}

func TestBothFormats(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "both"})
	if err != nil {
		t.Fatal(err)
	}
	r.Message(sample(9, 20, "twitch:xqc", "Bob", "hi"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	names := ls(t, dir)
	if len(names) != 2 {
		t.Fatalf("wrote %v, want both a .log and a .jsonl", names)
	}
}

func TestSplitBySource(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "text", SplitBySource: true})
	if err != nil {
		t.Fatal(err)
	}
	r.Message(sample(9, 20, "twitch:xqc", "A", "one"))
	r.Message(sample(9, 20, "kick:trainwreckstv", "B", "two"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if got := read(t, dir, "2026-08-09-twitch-xqc.log"); !strings.Contains(got, "one") {
		t.Errorf("twitch log = %q", got)
	}
	if got := read(t, dir, "2026-08-09-kick-trainwreckstv.log"); !strings.Contains(got, "two") {
		t.Errorf("kick log = %q", got)
	}
}

func TestFilesRollOverAtMidnight(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	r.Message(sample(9, 23, "twitch:xqc", "A", "before midnight"))
	r.Message(sample(10, 0, "twitch:xqc", "B", "after midnight"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if got := read(t, dir, "2026-08-09.log"); !strings.Contains(got, "before midnight") {
		t.Errorf("day one log = %q", got)
	}
	if got := read(t, dir, "2026-08-10.log"); !strings.Contains(got, "after midnight") {
		t.Errorf("day two log = %q", got)
	}
}

func TestAppendsToAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	for _, text := range []string{"first run", "second run"} {
		r, err := Open(Options{Dir: dir, Format: "text"})
		if err != nil {
			t.Fatal(err)
		}
		r.Message(sample(9, 20, "twitch:xqc", "A", text))
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}

	got := read(t, dir, "2026-08-09.log")
	if !strings.Contains(got, "first run") || !strings.Contains(got, "second run") {
		t.Errorf("reopening truncated the log: %q", got)
	}
}

func TestFlushMakesMessagesReadableBeforeClose(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	r.Message(sample(9, 20, "twitch:xqc", "A", "buffered"))
	r.Flush()

	if got := read(t, dir, "2026-08-09.log"); !strings.Contains(got, "buffered") {
		t.Errorf("Flush did not reach the file: %q", got)
	}
}

func TestOpenRejectsBadOptions(t *testing.T) {
	if _, err := Open(Options{Dir: "", Format: "text"}); err == nil {
		t.Error("an empty directory should be rejected")
	}
	if _, err := Open(Options{Dir: t.TempDir(), Format: "yaml"}); err == nil {
		t.Error("an unknown format should be rejected")
	}
}

func TestOpenCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	r, err := Open(Options{Dir: dir, Format: "text"})
	if err != nil {
		t.Fatalf("Open should create missing directories: %v", err)
	}
	defer r.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestSanitise(t *testing.T) {
	tests := map[string]string{
		"twitch:xqc":   "twitch-xqc",
		"youtube:a/b":  "youtube-a-b",
		"kick:tw":      "kick-tw",
		"":             "unknown",
		"../../etc/pw": "------etc-pw",
	}
	for in, want := range tests {
		if got := sanitise(in); got != want {
			t.Errorf("sanitise(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusIsNotLogged(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(Options{Dir: dir, Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	r.Status(chat.Status{Source: "twitch:xqc", State: chat.StateConnected})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if names := ls(t, dir); len(names) != 0 {
		t.Errorf("status alone created %v, want no files", names)
	}
}
