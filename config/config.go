// Package config is the on-disk configuration format: pure data, no behaviour.
// Callers turn a Config into running filters, recorders and retry policies.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration is a time.Duration that reads from TOML as "2s", "1m30s", etc.
type Duration time.Duration

// UnmarshalText parses a Go duration string, so TOML can hold "2s" rather than
// a bare number whose unit the reader has to guess.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Std returns the value as a plain time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Config is a whole config file. The zero value is not useful; start from
// Default or Load.
type Config struct {
	UI        UI                  `toml:"ui"`
	Log       Log                 `toml:"log"`
	Filter    Filter              `toml:"filter"`
	Highlight Highlight           `toml:"highlight"`
	Retry     Retry               `toml:"retry"`
	Presets   map[string][]string `toml:"presets"`
}

// UI is the [ui] table: how the interface is laid out.
type UI struct {
	Mode       string `toml:"mode"`       // "split" or "unified"
	Timestamps bool   `toml:"timestamps"` // show the HH:MM column
	Scrollback int    `toml:"scrollback"` // messages kept per pane
	Compact    bool   `toml:"compact"`    // drop the platform tag in split mode
}

// Log is the [log] table: whether and where chat is written to disk.
type Log struct {
	Enabled       bool   `toml:"enabled"`
	Dir           string `toml:"dir"`
	Format        string `toml:"format"` // "text", "jsonl" or "both"
	SplitBySource bool   `toml:"split_by_source"`
}

// Filter is the [filter] table: which messages are dropped before display.
type Filter struct {
	HideCommands bool     `toml:"hide_commands"` // drop messages starting with "!"
	HideUsers    []string `toml:"hide_users"`    // exact nick match, case-insensitive
	HidePatterns []string `toml:"hide_patterns"` // regexps matched against the text
	MinLength    int      `toml:"min_length"`
}

// Highlight is the [highlight] table: which messages are marked. Unlike Filter
// these rules never hide anything.
type Highlight struct {
	Words    []string `toml:"words"`    // case-insensitive substrings
	Users    []string `toml:"users"`    // exact nick match, case-insensitive
	Patterns []string `toml:"patterns"` // regexps, for when substrings are too blunt
}

// Retry is the [retry] table: the reconnect policy, mirroring chat.Retry.
type Retry struct {
	Initial     Duration `toml:"initial"`
	Max         Duration `toml:"max"`
	Factor      float64  `toml:"factor"`
	ResetAfter  Duration `toml:"reset_after"`
	MaxAttempts int      `toml:"max_attempts"` // 0 = keep trying forever
}

// Default is what you get with no config file at all.
func Default() Config {
	return Config{
		UI: UI{
			Mode:       "split",
			Timestamps: true,
			Scrollback: 2000,
		},
		Log: Log{
			Enabled: false,
			Dir:     filepath.Join(dataHome(), "chatmux"),
			Format:  "text",
		},
		Filter: Filter{
			HideCommands: false,
		},
		Retry: Retry{
			Initial:    Duration(2 * time.Second),
			Max:        Duration(60 * time.Second),
			Factor:     2,
			ResetAfter: Duration(2 * time.Minute),
		},
		Presets: map[string][]string{},
	}
}

// Path is where the config file lives, honouring XDG_CONFIG_HOME.
func Path() string {
	return filepath.Join(configHome(), "chatmux", "config.toml")
}

// Load reads path, falling back to defaults for anything it does not set. A
// missing file is not an error — it just means "use the defaults".
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown setting(s): %s", path, strings.Join(keys, ", "))
	}

	cfg.expand()
	return cfg, cfg.validate()
}

func (c *Config) expand() {
	c.Log.Dir = expandHome(c.Log.Dir)
	c.UI.Mode = strings.ToLower(strings.TrimSpace(c.UI.Mode))
	c.Log.Format = strings.ToLower(strings.TrimSpace(c.Log.Format))
}

func (c *Config) validate() error {
	switch c.UI.Mode {
	case "split", "unified":
	default:
		return fmt.Errorf("ui.mode: want \"split\" or \"unified\", got %q", c.UI.Mode)
	}
	switch c.Log.Format {
	case "text", "jsonl", "both":
	default:
		return fmt.Errorf("log.format: want \"text\", \"jsonl\" or \"both\", got %q", c.Log.Format)
	}
	if c.UI.Scrollback < 100 {
		c.UI.Scrollback = 100
	}
	if c.Log.Enabled && c.Log.Dir == "" {
		return errors.New("log.enabled is true but log.dir is empty")
	}
	for name, sources := range c.Presets {
		if len(sources) == 0 {
			return fmt.Errorf("preset %q is empty", name)
		}
		if strings.Contains(name, ":") {
			return fmt.Errorf("preset %q must not contain ':' (that is the source syntax)", name)
		}
	}
	return nil
}

// Preset resolves a preset name to its source list.
func (c Config) Preset(name string) ([]string, bool) {
	s, ok := c.Presets[name]
	return s, ok
}

// PresetNames lists the configured presets, for error messages and --list.
func (c Config) PresetNames() []string {
	names := make([]string, 0, len(c.Presets))
	for n := range c.Presets {
		names = append(names, n)
	}
	return names
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share")
}
