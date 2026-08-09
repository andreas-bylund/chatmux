package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("a missing config should not be an error, got %v", err)
	}
	if cfg.UI.Mode != "split" || !cfg.UI.Timestamps {
		t.Errorf("defaults not applied: %+v", cfg.UI)
	}
}

func TestLoadPartialConfigKeepsOtherDefaults(t *testing.T) {
	cfg, err := Load(write(t, `
[ui]
mode = "unified"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Mode != "unified" {
		t.Errorf("Mode = %q, want unified", cfg.UI.Mode)
	}
	if !cfg.UI.Timestamps {
		t.Error("Timestamps was reset; unset keys should keep their default")
	}
	if cfg.Retry.Initial.Std() != 2*time.Second {
		t.Errorf("Retry.Initial = %v, want the default 2s", cfg.Retry.Initial.Std())
	}
}

func TestLoadDurations(t *testing.T) {
	cfg, err := Load(write(t, `
[retry]
initial = "500ms"
max = "1m30s"
reset_after = "10m"
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Retry.Initial.Std(); got != 500*time.Millisecond {
		t.Errorf("Initial = %v, want 500ms", got)
	}
	if got := cfg.Retry.Max.Std(); got != 90*time.Second {
		t.Errorf("Max = %v, want 1m30s", got)
	}
	if got := cfg.Retry.ResetAfter.Std(); got != 10*time.Minute {
		t.Errorf("ResetAfter = %v, want 10m", got)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	tests := map[string]string{
		"unknown ui mode":      "[ui]\nmode = \"sideways\"\n",
		"unknown log format":   "[log]\nformat = \"xml\"\n",
		"unparseable duration": "[retry]\ninitial = \"soon\"\n",
		"preset with a colon":  "[presets]\n\"twitch:x\" = [\"twitch:a\"]\n",
		"empty preset":         "[presets]\nmine = []\n",
		"unknown key":          "[ui]\ncolour_scheme = \"neon\"\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestUnknownKeyErrorNamesTheKey(t *testing.T) {
	_, err := Load(write(t, "[ui]\ntimestampss = true\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "timestampss") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestPresets(t *testing.T) {
	cfg, err := Load(write(t, `
[presets]
kvall = ["twitch:xqc", "kick:trainwreckstv"]
jobb = ["twitch:theprimeagen"]
`))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := cfg.Preset("kvall")
	if !ok {
		t.Fatal("preset kvall not found")
	}
	if len(got) != 2 || got[0] != "twitch:xqc" {
		t.Errorf("preset kvall = %q", got)
	}
	if _, ok := cfg.Preset("nope"); ok {
		t.Error("an undefined preset was found")
	}
	if len(cfg.PresetNames()) != 2 {
		t.Errorf("PresetNames() = %q, want 2 entries", cfg.PresetNames())
	}
}

func TestScrollbackHasAFloor(t *testing.T) {
	cfg, err := Load(write(t, "[ui]\nscrollback = 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Scrollback < 100 {
		t.Errorf("Scrollback = %d, want it clamped to a usable minimum", cfg.UI.Scrollback)
	}
}

func TestHomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	cfg, err := Load(write(t, "[log]\ndir = \"~/chatlogs\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "chatlogs"); cfg.Log.Dir != want {
		t.Errorf("Log.Dir = %q, want %q", cfg.Log.Dir, want)
	}
}

// The starter config is what users edit first; a typo in it would greet them
// with a parse error on their very first run.
func TestTemplateIsValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the bundled template does not load: %v", err)
	}
	if cfg.UI.Mode != "split" {
		t.Errorf("template ui.mode = %q, want split", cfg.UI.Mode)
	}
	if cfg.Retry.Initial.Std() != 2*time.Second {
		t.Errorf("template retry.initial = %v, want 2s", cfg.Retry.Initial.Std())
	}
}

func TestWriteTemplateRefusesToClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteTemplate(path); err == nil {
		t.Error("WriteTemplate overwrote an existing config")
	}
}

func TestPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	if want := "/tmp/xdg-test/stream-chat/config.toml"; Path() != want {
		t.Errorf("Path() = %q, want %q", Path(), want)
	}
}
