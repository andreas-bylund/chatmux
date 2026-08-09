package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
	"github.com/andreas-bylund/chatmux/filter"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type connState int

const (
	stIdle connState = iota
	stConnecting
	stConnected
	stReconnecting
	stFailed
	stStopped
)

func toConnState(s chat.State) connState {
	switch s {
	case chat.StateConnecting:
		return stConnecting
	case chat.StateConnected:
		return stConnected
	case chat.StateReconnecting:
		return stReconnecting
	case chat.StateFailed:
		return stFailed
	case chat.StateStopped:
		return stStopped
	}
	return stIdle
}

// SourceInfo describes a source to the UI before it has produced anything.
type SourceInfo struct {
	Label    string
	Platform string
}

// Options configures the UI. Filter may be nil, in which case the filter toggle
// is hidden.
type Options struct {
	Sources    []SourceInfo
	Unified    bool
	Timestamps bool
	Compact    bool
	Scrollback int
	Filter     *filter.Filter
}

// minColWidth is the narrowest a split column can usefully be. Below this we
// fall back to the unified view rather than render unreadable slivers.
const minColWidth = 28

// Fallback dimensions for terminals that never report a size.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

type model struct {
	panes   map[string]*pane
	order   []string
	unified *pane

	opts     *renderOpts
	filter   *filter.Filter
	showHelp bool
	forced   bool // unified because the terminal is too narrow, not by choice
	unifiedM bool // the user's chosen mode

	focus  int
	width  int
	height int

	quitting bool
}

func newModel(o Options) *model {
	ro := &renderOpts{timestamps: o.Timestamps, showTag: !o.Compact}
	m := &model{
		panes:    make(map[string]*pane, len(o.Sources)),
		opts:     ro,
		filter:   o.Filter,
		unifiedM: o.Unified,
		unified:  newPane("all", "", o.Scrollback, ro),
		width:    defaultWidth,
		height:   defaultHeight,
	}
	for _, s := range o.Sources {
		m.order = append(m.order, s.Label)
		m.panes[s.Label] = newPane(s.Label, s.Platform, o.Scrollback, ro)
	}
	return m
}

func (m *model) Init() tea.Cmd { return tick() }

// tick drives the status bar's clock-dependent parts (msg/min decay, retry
// countdowns) even when no messages are arriving.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A terminal that cannot report its size (some pty setups, some CI
		// runners) sends zeros. Rendering nothing would leave a blank screen
		// forever, so keep the fallback instead.
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.invalidateAll()
		return m, nil

	case tickMsg:
		return m, tick()

	case *batch:
		for _, s := range msg.statuses {
			m.applyStatus(s)
		}
		for _, c := range msg.msgs {
			m.applyMessage(c)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) applyMessage(c chat.Message) {
	if p, ok := m.panes[c.Source]; ok {
		p.add(c, false)
	}
	m.unified.add(c, false)
}

func (m *model) applyStatus(s chat.Status) {
	p, ok := m.panes[s.Source]
	if !ok {
		return
	}
	prev := p.state
	p.state = toConnState(s.State)
	p.detail = ""

	switch s.State {
	case chat.StateReconnecting:
		p.detail = fmt.Sprintf("retry %s", roundDur(s.Retry))
		if s.Err != nil {
			m.note(p, fmt.Sprintf("%s lost: %v — reconnecting in %s",
				s.Source, s.Err, roundDur(s.Retry)))
		}
	case chat.StateFailed:
		p.detail = "failed"
		if s.Err != nil {
			m.note(p, fmt.Sprintf("%s failed: %v", s.Source, s.Err))
		}
	case chat.StateConnected:
		// Only worth announcing if we had previously fallen over.
		if prev == stReconnecting || prev == stFailed {
			m.note(p, s.Source+" reconnected")
		}
	}
}

// note adds a locally generated line to a pane and to the unified view, so the
// user can see connection trouble in context with the chat.
func (m *model) note(p *pane, text string) {
	msg := chat.Message{Time: time.Now(), Text: text, Source: p.label}
	p.add(msg, true)
	m.unified.add(msg, true)
}

func (m *model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		switch k.String() {
		case "q", "esc", "?", "ctrl+c":
			m.showHelp = false
		}
		if k.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	_, paneH := m.paneDims()

	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "?":
		m.showHelp = true

	case "tab":
		if n := m.visiblePaneCount(); n > 1 {
			m.focus = (m.focus + 1) % n
		}
	case "shift+tab":
		if n := m.visiblePaneCount(); n > 1 {
			m.focus = (m.focus - 1 + n) % n
		}

	case "up", "k":
		m.scrollFocused(-1, paneH)
	case "down", "j":
		m.scrollFocused(1, paneH)
	case "pgup", "b":
		m.scrollFocused(-max(1, paneH-1), paneH)
	case "pgdown", " ":
		m.scrollFocused(max(1, paneH-1), paneH)
	case "home", "g":
		m.focused().toTop()
	case "end", "G":
		m.focused().toBottom()

	case "u":
		m.unifiedM = !m.unifiedM
		m.focus = 0
		m.invalidateAll()
	case "t":
		m.opts.timestamps = !m.opts.timestamps
		m.invalidateAll()
	case "c":
		m.opts.showTag = !m.opts.showTag
		m.invalidateAll()
	case "f":
		if m.filter != nil {
			m.filter.Toggle()
		}
	}
	return m, nil
}

func (m *model) scrollFocused(delta, height int) {
	w, _ := m.paneDims()
	m.focused().scroll(delta, w, height)
}

func (m *model) focused() *pane {
	if m.isUnified() || len(m.order) == 0 {
		return m.unified
	}
	if m.focus >= len(m.order) {
		m.focus = 0
	}
	return m.panes[m.order[m.focus]]
}

func (m *model) visiblePaneCount() int {
	if m.isUnified() {
		return 1
	}
	return len(m.order)
}

// isUnified reports the effective mode, which may differ from the chosen one
// when the terminal is too narrow to split.
func (m *model) isUnified() bool {
	if m.unifiedM {
		return true
	}
	return m.forced
}

func (m *model) invalidateAll() {
	m.unified.invalidate()
	for _, p := range m.panes {
		p.invalidate()
	}
}

// paneDims returns the content width and height of a single pane.
func (m *model) paneDims() (int, int) {
	// One line for the header row, one for the status bar.
	h := max(1, m.height-2)
	if m.isUnified() || len(m.order) <= 1 {
		return max(4, m.width), h
	}
	n := len(m.order)
	// n-1 single-column separators between panes.
	w := (m.width - (n - 1)) / n
	return max(4, w), h
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	if m.showHelp {
		return m.helpView()
	}

	// Decide whether a split still fits before measuring anything.
	m.forced = false
	if !m.unifiedM && len(m.order) > 1 {
		if w := (m.width - (len(m.order) - 1)) / len(m.order); w < minColWidth {
			m.forced = true
		}
	}

	paneW, paneH := m.paneDims()

	var body []string
	if m.isUnified() || len(m.order) <= 1 {
		body = m.unifiedBody(paneW, paneH)
	} else {
		body = m.splitBody(paneW, paneH)
	}

	return strings.Join(append(body, m.statusBar()), "\n")
}

func (m *model) unifiedBody(w, h int) []string {
	p := m.unified
	if len(m.order) == 1 && !m.unifiedM {
		p = m.panes[m.order[0]]
	}
	header := m.paneHeader(p, w, true, m.forced)
	return append([]string{header}, padAll(p.view(w, h), w)...)
}

func (m *model) splitBody(w, h int) []string {
	cols := make([][]string, len(m.order))
	heads := make([]string, len(m.order))
	for i, label := range m.order {
		p := m.panes[label]
		heads[i] = m.paneHeader(p, w, i == m.focus, false)
		cols[i] = padAll(p.view(w, h), w)
	}

	sep := styleSep.Render("│")
	out := make([]string, 0, h+1)
	out = append(out, strings.Join(heads, sep))
	for row := 0; row < h; row++ {
		parts := make([]string, len(cols))
		for i := range cols {
			parts[i] = cols[i][row]
		}
		out = append(out, strings.Join(parts, sep))
	}
	return out
}

// paneHeader is the "─ twitch:xqc ●──────" rule above each column.
func (m *model) paneHeader(p *pane, w int, focused, forced bool) string {
	th := themeFor(p.platform)
	name := p.label
	if p.platform == "" {
		name = "all sources"
	}

	label := th.accent.Render(name)
	if focused && m.visiblePaneCount() > 1 {
		label = styleFocus.Render(name)
	}

	parts := []string{styleSep.Render("─"), label}
	if p.platform != "" {
		parts = append(parts, stateDot(p.state))
		if p.detail != "" {
			parts = append(parts, styleFaint.Render(p.detail))
		}
	}
	if !p.pinned {
		parts = append(parts, styleBar.Render("↑paused"))
	}
	if forced {
		parts = append(parts, styleFaint.Render("(too narrow to split)"))
	}

	head := strings.Join(parts, " ")
	if fill := w - ansi.StringWidth(head) - 1; fill > 0 {
		head += " " + styleSep.Render(strings.Repeat("─", fill))
	}
	return truncPad(head, w)
}

func (m *model) statusBar() string {
	now := time.Now()

	total := 0
	dots := make([]string, 0, len(m.order))
	for _, label := range m.order {
		p := m.panes[label]
		total += p.rate.perMinute(now)
		dots = append(dots, stateDot(p.state)+" "+themeFor(p.platform).accent.Render(short(label)))
	}

	left := strings.Join(dots, "  ")
	right := []string{fmt.Sprintf("%d msg/min", total)}
	if m.filter != nil && m.filter.Active() {
		state := "off"
		if m.filter.Enabled() {
			state = "on"
		}
		right = append(right, "filter "+state)
	}
	if !m.focused().pinned {
		right = append(right, styleBar.Render("scrolled"))
	}
	right = append(right, styleKey.Render("?")+" help")

	sep := styleFaint.Render(" · ")
	l := left
	r := strings.Join(right, sep)

	gap := m.width - ansi.StringWidth(l) - ansi.StringWidth(r) - 2
	if gap < 1 {
		// Not enough room for both halves; the counters matter more than the
		// source list, which is already visible in the headers.
		return styleStatus.Render(truncPad(" "+r, m.width))
	}
	return styleStatus.Render(" " + l + strings.Repeat(" ", gap) + r + " ")
}

func (m *model) helpView() string {
	rows := [][2]string{
		{"q / ctrl+c", "quit"},
		{"↑ ↓ / k j", "scroll one line"},
		{"pgup / pgdn", "scroll one page"},
		{"g / G", "jump to top / bottom"},
		{"tab / shift+tab", "focus next / previous pane"},
		{"u", "toggle split and unified view"},
		{"t", "toggle timestamps"},
		{"c", "toggle the platform tag"},
		{"f", "toggle hide filters"},
		{"?", "close this help"},
	}

	var b strings.Builder
	b.WriteString(styleFocus.Render("chatmux") + "\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %s  %s\n",
			truncPad(styleKey.Render(r[0]), 18), styleDim.Render(r[1]))
	}
	b.WriteString("\n" + styleFaint.Render("  Scrolling up pauses autoscroll; G resumes it."))

	lines := strings.Split(b.String(), "\n")
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	return strings.Join(lines[:max(1, m.height-1)], "\n") + "\n" + m.statusBar()
}

// padAll pads every line to exactly w visible columns so adjacent panes line up.
func padAll(lines []string, w int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = truncPad(l, w)
	}
	return out
}

// truncPad forces a styled string to exactly w visible columns.
func truncPad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n > w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-n)
}

// short trims "twitch:somelongchannelname" for the status bar.
func short(label string) string {
	if len(label) <= 22 {
		return label
	}
	return label[:21] + "…"
}

func roundDur(d time.Duration) time.Duration {
	if d >= time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(100 * time.Millisecond)
}
