// Package events is a small in-memory pub/sub: bots publish, SSE clients subscribe.
package events

import (
	"sync"
)

// Event types sent over SSE.
const (
	TypeStatus     = "status"
	TypeTranscript = "transcript"
	TypeSummary    = "summary"
	TypeError      = "error"
)

// Event is one message for a meeting's subscribers.
type Event struct {
	Type string
	ID   int64 // transcript segment id (for Last-Event-ID); 0 otherwise
	Data any   // JSON-encoded by the SSE handler
}

// Hub fans events out per meeting. Slow subscribers are dropped, never waited on,
// so a stuck browser tab can't stall a bot.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*sub]struct{}
}

type sub struct {
	ch   chan Event
	once sync.Once
}

func (s *sub) close() { s.once.Do(func() { close(s.ch) }) }

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[string]map[*sub]struct{}{}} }

// Subscribe returns a channel of events for meetingID and a cancel func.
// The channel is closed when cancel is called or the subscriber falls behind.
func (h *Hub) Subscribe(meetingID string) (<-chan Event, func()) {
	s := &sub{ch: make(chan Event, 64)}
	h.mu.Lock()
	if h.subs[meetingID] == nil {
		h.subs[meetingID] = map[*sub]struct{}{}
	}
	h.subs[meetingID][s] = struct{}{}
	h.mu.Unlock()
	return s.ch, func() { h.remove(meetingID, s) }
}

func (h *Hub) remove(meetingID string, s *sub) {
	h.mu.Lock()
	delete(h.subs[meetingID], s)
	if len(h.subs[meetingID]) == 0 {
		delete(h.subs, meetingID)
	}
	h.mu.Unlock()
	s.close()
}

// Publish sends e to every subscriber of meetingID without blocking.
func (h *Hub) Publish(meetingID string, e Event) {
	h.mu.Lock()
	var slow []*sub
	for s := range h.subs[meetingID] {
		select {
		case s.ch <- e:
		default:
			slow = append(slow, s)
		}
	}
	h.mu.Unlock()
	for _, s := range slow {
		h.remove(meetingID, s) // client reconnects and replays via Last-Event-ID
	}
}

// Subscribers returns how many clients watch meetingID (for tests/metrics).
func (h *Hub) Subscribers(meetingID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[meetingID])
}
