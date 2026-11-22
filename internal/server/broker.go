package server

import (
	"strconv"
	"sync"
)

// broker fans change nudges out to the SSE streams of a vault. Slow
// subscribers never block a write: each has a one-slot channel that
// always holds the latest cursor.
type broker struct {
	mu   sync.Mutex
	subs map[string]map[chan int64]struct{}
	done chan struct{}
	once sync.Once
}

func newBroker() *broker {
	return &broker{subs: map[string]map[chan int64]struct{}{}, done: make(chan struct{})}
}

func brokerKey(userID int64, vault string) string { return strconv.FormatInt(userID, 10) + "/" + vault }

func (b *broker) subscribe(key string) (<-chan int64, func()) {
	ch := make(chan int64, 1)
	b.mu.Lock()
	if b.subs[key] == nil {
		b.subs[key] = map[chan int64]struct{}{}
	}
	b.subs[key][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs[key], ch)
		if len(b.subs[key]) == 0 {
			delete(b.subs, key)
		}
		b.mu.Unlock()
	}
}

func (b *broker) publish(key string, cursor int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[key] {
		c := cursor
		select {
		case old := <-ch: // coalesce: keep only the newest cursor
			c = max(c, old)
		default:
		}
		ch <- c // never blocks: the slot was just emptied and we hold mu
	}
}

func (b *broker) close() { b.once.Do(func() { close(b.done) }) }
