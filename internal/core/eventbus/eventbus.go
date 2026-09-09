// Package eventbus is the in-process fan-out behind the console's live
// event stream. Anything that happens - a request logged, an item
// waiting in the intercept queue, a project opened - is published here
// once and every connected console tab hears it.
package eventbus

import (
	"context"
	"sync"
)

// Event is one thing that happened. Type is a dotted name the console
// switches on, Data is the payload rendered as JSON.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitzero"`
}

// Bus fans events out to every subscriber. Safe for concurrent use.
type Bus struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// New builds an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[chan Event]struct{})}
}

// subscriberBuffer is how far a slow subscriber may fall behind before
// events are dropped for it. A console tab that stopped reading must not
// hold the proxy's request path.
const subscriberBuffer = 256

// Subscribe returns a channel that receives every event published until
// ctx is done, after which the channel is closed. A subscriber that
// does not keep up loses events rather than blocking publishers.
func (b *Bus) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	context.AfterFunc(ctx, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	})

	return ch
}

// Publish delivers e to every current subscriber without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Emit is Publish with the two fields spelled out.
func (b *Bus) Emit(typ string, data any) {
	b.Publish(Event{Type: typ, Data: data})
}
