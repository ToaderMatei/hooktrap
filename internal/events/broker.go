// Package events implements a tiny fan-out broker for live dashboard updates.
package events

import (
	"sync"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

// Broker fans events out to subscribers. Slow subscribers drop events rather
// than blocking the capture path.
type Broker struct {
	mu   sync.Mutex
	subs map[chan model.Event]struct{}
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[chan model.Event]struct{})}
}

// Subscribe registers a new subscriber and returns its channel and a cancel func.
func (b *Broker) Subscribe() (<-chan model.Event, func()) {
	ch := make(chan model.Event, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish sends ev to every subscriber without blocking.
func (b *Broker) Publish(ev model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Count returns the number of live subscribers.
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
