package ui

import (
	"sync"
	"time"

	"github.com/andreas-bylund/chatmux/chat"
	tea "github.com/charmbracelet/bubbletea"
)

// frameInterval bounds how often the model is woken. A busy channel can produce
// hundreds of messages a second; repainting for each one would burn CPU to
// render frames nobody can read. Batching at ~20fps is imperceptible.
const frameInterval = 50 * time.Millisecond

// batch is everything that arrived since the last frame.
type batch struct {
	msgs     []chat.Message
	statuses []chat.Status
}

// sink is the chat.Sink handed to the sources. It buffers into a batch that the
// pump goroutine hands to Bubble Tea, so source goroutines never block on the
// renderer.
type sink struct {
	mu       sync.Mutex
	msgs     []chat.Message
	statuses []chat.Status

	// dropped counts messages discarded because the UI fell far behind. This
	// should never happen in practice; it exists so a pathological burst
	// degrades instead of exhausting memory.
	dropped int
}

// maxPending caps the buffer between frames.
const maxPending = 10000

func (s *sink) Message(m chat.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.msgs) >= maxPending {
		s.dropped++
		return
	}
	s.msgs = append(s.msgs, m)
}

func (s *sink) Status(st chat.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, st)
}

// take swaps out the pending buffer, returning nil when nothing is waiting.
func (s *sink) take() *batch {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.msgs) == 0 && len(s.statuses) == 0 {
		return nil
	}
	b := &batch{msgs: s.msgs, statuses: s.statuses}
	s.msgs, s.statuses = nil, nil
	return b
}

// pump forwards batches to the program until stop is closed.
func (s *sink) pump(p *tea.Program, stop <-chan struct{}) {
	t := time.NewTicker(frameInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			if b := s.take(); b != nil {
				p.Send(b)
			}
			return
		case <-t.C:
			if b := s.take(); b != nil {
				p.Send(b)
			}
		}
	}
}
