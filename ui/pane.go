package ui

import (
	"strings"

	"github.com/andreas-bylund/chatmux/chat"
	"github.com/charmbracelet/x/ansi"
)

// minTextWidth is the narrowest column we will still try to hang-indent under a
// nickname. Below it, continuation lines just get a two-space indent.
const minTextWidth = 24

type entry struct {
	msg chat.Message
	// system marks a locally generated line (connection notices), which is
	// rendered differently and never written to the chat log.
	system bool
	// lines caches the wrapped render at cacheWidth; nil means "not yet built".
	lines []string
}

// pane is one scrollable column of messages.
//
// Rendering is anchored to a message rather than to a line offset from the
// bottom, so the view stays put when new messages arrive while the user is
// scrolled up.
type pane struct {
	label    string
	platform string
	state    connState
	detail   string // e.g. "retry in 8s", shown in the header

	entries []entry
	dropped int // messages evicted from the front, for absolute indexing
	limit   int

	rate rate

	// pinned means "follow the newest message". Any scroll up unpins.
	pinned     bool
	anchorMsg  int // absolute message index at the top of the view
	anchorLine int // line within that message

	cacheWidth int
	opts       *renderOpts
}

// renderOpts is shared by every pane so a toggle applies everywhere at once.
type renderOpts struct {
	timestamps bool
	showTag    bool
}

func newPane(label, platform string, limit int, opts *renderOpts) *pane {
	return &pane{
		label:    label,
		platform: platform,
		limit:    limit,
		pinned:   true,
		opts:     opts,
	}
}

func (p *pane) add(m chat.Message, system bool) {
	p.entries = append(p.entries, entry{msg: m, system: system})
	if !system {
		p.rate.add(m.Time)
	}

	// Trim in batches: shifting the slice on every message would be O(n) per
	// message on a busy channel.
	if overflow := len(p.entries) - p.limit; overflow >= p.limit/4+1 {
		p.entries = append(p.entries[:0], p.entries[overflow:]...)
		p.dropped += overflow
		if p.anchorMsg < p.dropped {
			p.anchorMsg = p.dropped
			p.anchorLine = 0
		}
	}
}

// invalidate drops the wrapped-line cache, e.g. after a resize or a display
// toggle.
func (p *pane) invalidate() {
	for i := range p.entries {
		p.entries[i].lines = nil
	}
}

// linesOf returns the wrapped render of entry i, building it on first use.
func (p *pane) linesOf(i, width int) []string {
	if width != p.cacheWidth {
		p.cacheWidth = width
		p.invalidate()
	}
	if p.entries[i].lines == nil {
		p.entries[i].lines = p.render(p.entries[i], width)
	}
	return p.entries[i].lines
}

func (p *pane) render(e entry, width int) []string {
	if width < 4 {
		width = 4
	}
	if e.system {
		return wrapPlain(styleSystem.Render("— "+e.msg.Text), width)
	}

	m := e.msg
	var prefix strings.Builder
	if p.opts.showTag {
		prefix.WriteString(themeFor(m.Platform).tagCell())
		prefix.WriteByte(' ')
	}
	if p.opts.timestamps {
		prefix.WriteString(styleDim.Render(m.Time.Local().Format("15:04")))
		prefix.WriteByte(' ')
	}

	nick := nickStyle(m.User, m.Color).Render(m.User)
	body := m.Text
	if m.Action {
		prefix.WriteString(styleDim.Render("* "))
		prefix.WriteString(nick)
		prefix.WriteByte(' ')
	} else {
		prefix.WriteString(nick)
		prefix.WriteString(styleDim.Render(": "))
	}

	head := prefix.String()
	if m.Highlight {
		head = styleBar.Render("▎") + head
		body = styleHighlight.Render(body)
	}

	indent := ansi.StringWidth(head)
	textWidth := width - indent

	// Too narrow to hang under the nick: wrap the whole thing and indent
	// continuations slightly instead.
	if textWidth < minTextWidth {
		return wrapPlain(head+body, width)
	}

	wrapped := strings.Split(ansi.Wrap(body, textWidth, " -"), "\n")
	out := make([]string, 0, len(wrapped))
	pad := strings.Repeat(" ", indent)
	for i, l := range wrapped {
		if i == 0 {
			out = append(out, head+l)
		} else {
			out = append(out, pad+l)
		}
	}
	return out
}

func wrapPlain(s string, width int) []string {
	return strings.Split(ansi.Wrap(s, width, " -"), "\n")
}

// view returns exactly height lines of content, padded with blanks if the pane
// holds less than a screenful.
func (p *pane) view(width, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(p.entries) == 0 {
		return blank(height)
	}

	if p.pinned {
		return p.viewFromBottom(width, height)
	}
	return p.viewFromAnchor(width, height, p.anchorMsg, p.anchorLine)
}

func (p *pane) viewFromBottom(width, height int) []string {
	var out []string
	for i := len(p.entries) - 1; i >= 0 && len(out) < height; i-- {
		out = append(p.linesOf(i, width), out...)
	}
	if len(out) > height {
		out = out[len(out)-height:]
	}
	for len(out) < height {
		out = append([]string{""}, out...)
	}
	return out
}

func (p *pane) viewFromAnchor(width, height, msg, line int) []string {
	out := make([]string, 0, height)
	i := msg - p.dropped
	if i < 0 {
		i = 0
		line = 0
	}
	for ; i < len(p.entries) && len(out) < height; i++ {
		lines := p.linesOf(i, width)
		if line > 0 {
			if line >= len(lines) {
				line -= len(lines)
				continue
			}
			lines = lines[line:]
			line = 0
		}
		out = append(out, lines...)
	}
	if len(out) > height {
		out = out[:height]
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// scroll moves the view by delta lines (negative = towards older messages) and
// re-pins when it reaches the bottom.
//
// Scrolling works in absolute line coordinates and converts back to a message
// anchor at the end. Doing the arithmetic on a flat line index is far easier to
// get right than walking message boundaries in both directions.
func (p *pane) scroll(delta, width, height int) {
	if len(p.entries) == 0 || delta == 0 {
		return
	}
	maxTop := max(0, p.totalLines(width)-height)

	cur := maxTop
	if !p.pinned {
		cur = p.absLine(p.anchorMsg, p.anchorLine, width)
	}

	next := cur + delta
	if next >= maxTop {
		p.pinned = true
		return
	}
	if next < 0 {
		next = 0
	}
	p.pinned = false
	p.anchorMsg, p.anchorLine = p.anchorAt(next, width)
}

func (p *pane) totalLines(width int) int {
	total := 0
	for i := range p.entries {
		total += len(p.linesOf(i, width))
	}
	return total
}

// absLine flattens an anchor into a line index from the top of the pane.
func (p *pane) absLine(msg, line, width int) int {
	total := 0
	for i := range p.entries {
		if p.dropped+i == msg {
			return total + line
		}
		total += len(p.linesOf(i, width))
	}
	return total
}

// anchorAt is the inverse of absLine, clamped to the pane's contents.
func (p *pane) anchorAt(abs, width int) (int, int) {
	if abs < 0 {
		abs = 0
	}
	for i := range p.entries {
		n := len(p.linesOf(i, width))
		if abs < n {
			return p.dropped + i, abs
		}
		abs -= n
	}
	i := len(p.entries) - 1
	return p.dropped + i, max(0, len(p.linesOf(i, width))-1)
}

func (p *pane) toBottom() { p.pinned = true }

func (p *pane) toTop() {
	if len(p.entries) == 0 {
		return
	}
	p.pinned = false
	p.anchorMsg, p.anchorLine = p.dropped, 0
}

func blank(n int) []string {
	out := make([]string, n)
	return out
}
