// Package filter decides which messages are worth showing and which deserve to
// stand out.
package filter

import (
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/andreas-bylund/Stream-chat-terminal/chat"
)

// Rules is the compiled-from-config description of what to hide and highlight.
type Rules struct {
	HideCommands bool
	HideUsers    []string
	HidePatterns []string
	MinLength    int

	HighlightWords    []string
	HighlightUsers    []string
	HighlightPatterns []string
}

// Filter applies Rules to messages. It is safe for concurrent use, and the
// enabled flag can be toggled from the UI while sources are running.
type Filter struct {
	enabled atomic.Bool

	hideCommands bool
	hideUsers    map[string]bool
	hidePatterns []*regexp.Regexp
	minLength    int

	highlightWords    []string // pre-lowercased
	highlightUsers    map[string]bool
	highlightPatterns []*regexp.Regexp
}

// New compiles rules into a Filter, reporting the first bad regexp.
func New(r Rules) (*Filter, error) {
	f := &Filter{
		hideCommands:   r.HideCommands,
		hideUsers:      lowerSet(r.HideUsers),
		minLength:      r.MinLength,
		highlightUsers: lowerSet(r.HighlightUsers),
	}
	f.enabled.Store(true)

	var err error
	if f.hidePatterns, err = compile(r.HidePatterns, "filter.hide_patterns"); err != nil {
		return nil, err
	}
	if f.highlightPatterns, err = compile(r.HighlightPatterns, "highlight.patterns"); err != nil {
		return nil, err
	}

	for _, w := range r.HighlightWords {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			f.highlightWords = append(f.highlightWords, w)
		}
	}
	return f, nil
}

// SetEnabled turns hiding on or off. Highlighting is unaffected: it is an
// annotation, not a filter, and there is no reason to lose it.
func (f *Filter) SetEnabled(on bool) { f.enabled.Store(on) }

// Enabled reports whether hiding rules are currently applied.
func (f *Filter) Enabled() bool { return f.enabled.Load() }

// Toggle flips Enabled and returns the new value.
func (f *Filter) Toggle() bool {
	on := !f.enabled.Load()
	f.enabled.Store(on)
	return on
}

// Active reports whether any hiding rule is configured at all, so the UI can
// avoid advertising a toggle that would do nothing.
func (f *Filter) Active() bool {
	return f.hideCommands || len(f.hideUsers) > 0 || len(f.hidePatterns) > 0 || f.minLength > 0
}

// Apply returns m annotated with Highlight, and whether it should be shown.
func (f *Filter) Apply(m chat.Message) (chat.Message, bool) {
	m.Highlight = f.highlights(m)
	if !f.enabled.Load() {
		return m, true
	}
	return m, !f.hides(m)
}

func (f *Filter) hides(m chat.Message) bool {
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return true
	}
	if f.hideCommands && strings.HasPrefix(text, "!") {
		return true
	}
	if f.minLength > 0 && utf8.RuneCountInString(text) < f.minLength {
		return true
	}
	if f.hideUsers[strings.ToLower(m.User)] {
		return true
	}
	for _, re := range f.hidePatterns {
		if re.MatchString(m.Text) {
			return true
		}
	}
	return false
}

func (f *Filter) highlights(m chat.Message) bool {
	if f.highlightUsers[strings.ToLower(m.User)] {
		return true
	}
	if len(f.highlightWords) > 0 {
		lower := strings.ToLower(m.Text)
		for _, w := range f.highlightWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	for _, re := range f.highlightPatterns {
		if re.MatchString(m.Text) {
			return true
		}
	}
	return false
}

func compile(patterns []string, field string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: bad regexp %q: %w", field, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func lowerSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, s := range items {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			m[s] = true
		}
	}
	return m
}
