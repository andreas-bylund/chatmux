package ui

import (
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colours are given as 256-colour indices so the palette survives on terminals
// without truecolor; lipgloss degrades further if it has to.
var (
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleFaint     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleHighlight = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	styleBar       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	styleSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleStatus    = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236"))
	styleKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	styleFocus     = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	styleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
)

type platformTheme struct {
	tag    lipgloss.Style
	accent lipgloss.Style
	letter string
}

var platforms = map[string]platformTheme{
	"twitch": {
		tag:    lipgloss.NewStyle().Background(lipgloss.Color("55")).Foreground(lipgloss.Color("231")).Bold(true),
		accent: lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		letter: "T",
	},
	"youtube": {
		tag:    lipgloss.NewStyle().Background(lipgloss.Color("160")).Foreground(lipgloss.Color("231")).Bold(true),
		accent: lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		letter: "Y",
	},
	"kick": {
		tag:    lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("16")).Bold(true),
		accent: lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		letter: "K",
	},
}

func themeFor(platform string) platformTheme {
	if t, ok := platforms[platform]; ok {
		return t
	}
	letter := "?"
	if platform != "" {
		letter = strings.ToUpper(platform[:1])
	}
	return platformTheme{
		tag:    lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("231")).Bold(true),
		accent: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		letter: letter,
	}
}

func (t platformTheme) tagCell() string { return t.tag.Render(" " + t.letter + " ") }

// Tag renders the coloured platform badge. Exported so the plain-text renderer
// can share one definition of what a platform looks like.
func Tag(platform string) string { return themeFor(platform).tagCell() }

// NickStyle is the colour a nickname is drawn in, shared with the plain
// renderer for the same reason as Tag.
func NickStyle(nick, color string) lipgloss.Style { return nickStyle(nick, color) }

// nickPalette is used when a platform gives us no colour for a user. Hashing
// the nick means the same person keeps the same colour across a session.
var nickPalette = []lipgloss.Style{
	newNick("203"), newNick("208"), newNick("220"), newNick("82"),
	newNick("45"), newNick("75"), newNick("141"), newNick("213"),
	newNick("199"), newNick("156"), newNick("117"), newNick("186"),
}

func newNick(c string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true)
}

// nickStyle prefers the platform-supplied colour, falling back to a hashed
// palette entry. Very dark colours are ignored: Twitch lets users pick #000000,
// which is unreadable on a dark terminal.
func nickStyle(nick, color string) lipgloss.Style {
	if isUsableHex(color) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(nick)))
	return nickPalette[int(h.Sum32()%uint32(len(nickPalette)))]
}

func isUsableHex(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	var v [3]int
	for i := 0; i < 3; i++ {
		hi, ok1 := hexVal(c[1+i*2])
		lo, ok2 := hexVal(c[2+i*2])
		if !ok1 || !ok2 {
			return false
		}
		v[i] = hi<<4 | lo
	}
	// Rec. 601 luma; below this the text disappears into the background.
	luma := (299*v[0] + 587*v[1] + 114*v[2]) / 1000
	return luma >= 60
}

func hexVal(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, true
	}
	return 0, false
}

// stateDot renders the connection indicator shown in the status bar and pane
// headers.
func stateDot(s connState) string {
	switch s {
	case stConnected:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("●")
	case stConnecting:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◐")
	case stReconnecting:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("◌")
	case stFailed:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("✕")
	default:
		return styleFaint.Render("·")
	}
}
