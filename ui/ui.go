// Package ui renders the chat stream as a full-screen terminal interface.
//
// The design constraint is that a busy channel produces messages far faster
// than a person can read them: source goroutines must never block on rendering,
// and rendering must not run once per message. Messages are therefore buffered
// by sink and handed to Bubble Tea in batches.
package ui

import (
	"github.com/andreas-bylund/chatmux/chat"
	tea "github.com/charmbracelet/bubbletea"
)

// Program is a running TUI.
type Program struct {
	prog *tea.Program
	sink *sink
	stop chan struct{}
}

// New builds the interface. Nothing is drawn until Run is called.
func New(o Options) *Program {
	if o.Scrollback <= 0 {
		o.Scrollback = 2000
	}
	m := newModel(o)
	return &Program{
		prog: tea.NewProgram(m, tea.WithAltScreen()),
		sink: &sink{},
		stop: make(chan struct{}),
	}
}

// Sink returns the chat.Sink to feed sources into. It is safe for concurrent
// use and never blocks.
func (p *Program) Sink() chat.Sink { return p.sink }

// Run draws until the user quits or Quit is called. It returns once the
// terminal has been restored.
func (p *Program) Run() error {
	go p.sink.pump(p.prog, p.stop)
	_, err := p.prog.Run()
	close(p.stop)
	return err
}

// Quit asks the interface to shut down, e.g. because a signal arrived.
func (p *Program) Quit() { p.prog.Quit() }

// Dropped reports messages discarded because the UI could not keep up. It is
// meaningful only after Run returns.
func (p *Program) Dropped() int {
	p.sink.mu.Lock()
	defer p.sink.mu.Unlock()
	return p.sink.dropped
}
