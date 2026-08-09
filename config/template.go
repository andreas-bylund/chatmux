package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Template is the commented starter config written by --init-config. It is kept
// in sync with Default() by TestTemplateMatchesDefaults.
const Template = `# stream-chat configuration
# Docs: https://github.com/andreas-bylund/Stream-chat-terminal

[ui]
# "split"   — one column per source
# "unified" — every source merged into a single column
mode = "split"
# Show the HH:MM column.
timestamps = true
# Messages kept per pane for scrollback.
scrollback = 2000
# In split mode the column header already names the platform, so the per-line
# tag is redundant. Set true to drop it.
compact = false

[log]
enabled = false
dir = "~/.local/share/stream-chat"
# "text", "jsonl", or "both".
format = "text"
# One file per source instead of one combined file.
split_by_source = false

[filter]
# Hide messages starting with "!" (bot commands).
hide_commands = false
# Nicks to hide entirely, case-insensitive.
hide_users = ["nightbot", "streamelements", "moobot"]
# Regexps matched against message text; matching messages are hidden.
hide_patterns = []
# Hide messages shorter than this many characters. 0 disables.
min_length = 0

[highlight]
# Case-insensitive substrings. Matching messages get a marked line.
words = []
# Nicks whose every message is highlighted.
users = []
# Regexps, for when a substring is too blunt.
patterns = []

[retry]
# Backoff between reconnect attempts.
initial = "2s"
max = "60s"
factor = 2.0
# A connection that stayed up this long resets the backoff.
reset_after = "2m"
# Consecutive failures before giving up. 0 = never give up.
max_attempts = 0

# Named source sets, so you can run "stream-chat evening" instead of typing
# every source out. Names must not contain ":".
[presets]
# evening = ["twitch:xqc", "kick:trainwreckstv"]
`

// WriteTemplate creates a starter config at path. It refuses to clobber an
// existing file — losing a hand-tuned config to a stray flag would be rude.
func WriteTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists", path)
		}
		return err
	}
	defer f.Close()
	_, err = f.WriteString(Template)
	return err
}
