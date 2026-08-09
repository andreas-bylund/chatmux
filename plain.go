package main

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"github.com/andreas-bylund/chatmux/chat"
	"github.com/andreas-bylund/chatmux/ui"
	"github.com/charmbracelet/lipgloss"
)

// plainSink writes one line per message to stdout, the way the original
// version of this tool did.
//
// It exists for two reasons: piping into grep/tee stays useful, and a TUI
// cannot run at all when stdout is not a terminal. Status changes go to stderr
// so a redirected stdout contains chat and nothing else.
type plainSink struct {
	mu         sync.Mutex
	out        *bufio.Writer
	errOut     io.Writer
	timestamps bool
}

var (
	plainDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	plainMark = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	plainNote = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	plainErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func newPlainSink(out io.Writer, errOut io.Writer, timestamps bool) *plainSink {
	return &plainSink{
		out:        bufio.NewWriter(out),
		errOut:     errOut,
		timestamps: timestamps,
	}
}

func (s *plainSink) Message(m chat.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.Highlight {
		fmt.Fprint(s.out, plainMark.Render("▎"))
	}
	fmt.Fprint(s.out, ui.Tag(m.Platform), " ")
	if s.timestamps {
		fmt.Fprint(s.out, plainDim.Render(m.Time.Local().Format("15:04")), " ")
	}

	nick := ui.NickStyle(m.User, m.Color).Render(m.User)
	if m.Action {
		fmt.Fprintf(s.out, "%s %s %s\n", plainDim.Render("*"), nick, m.Text)
	} else {
		fmt.Fprintf(s.out, "%s%s %s\n", nick, plainDim.Render(":"), m.Text)
	}
	// Flushed per message: a piped reader should see lines as they arrive, not
	// when a 4KB buffer happens to fill.
	s.out.Flush()
}

func (s *plainSink) Status(st chat.Status) {
	var text string
	switch st.State {
	case chat.StateConnected:
		text = st.Source + " connected"
	case chat.StateReconnecting:
		text = fmt.Sprintf("%s lost (%v) — retrying in %s", st.Source, st.Err, st.Retry.Round(1e8))
	case chat.StateFailed:
		text = fmt.Sprintf("%s failed: %v", st.Source, st.Err)
	default:
		return
	}

	style := plainNote
	if st.State == chat.StateFailed {
		style = plainErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintln(s.errOut, style.Render("── "+text))
}

func (s *plainSink) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out.Flush()
}
