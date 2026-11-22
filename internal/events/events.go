// Package events is Nota's core event bus. Vault operations emit typed
// events (note.created, note.saved, …); the index, hooks and plugins
// subscribe to them, either synchronously or on their own goroutine.
package events

import (
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Type names an event. The names are part of the hook and plugin API.
type Type string

// Event types emitted by the core.
const (
	NoteCreated Type = "note.created"
	NoteSaved   Type = "note.saved"
	NoteMoved   Type = "note.moved"
	NoteDeleted Type = "note.deleted"
	InboxAdded  Type = "inbox.added"
)

// Event is one thing that happened in a vault.
type Event struct {
	Type Type `json:"type"`
	// Path is the vault-relative path of the note: its new path for
	// note.moved, the path it had for note.deleted.
	Path string `json:"path"`
	// OldPath is the previous path, for note.moved.
	OldPath string `json:"oldPath,omitempty"`
	// Trash is where a deleted note went (".trash/…"), for note.deleted.
	Trash string    `json:"trash,omitempty"`
	Time  time.Time `json:"time"`
}

// Handler receives events.
type Handler func(Event)

// Bus delivers events to subscribers. The zero value is not usable; call
// New. A nil *Bus is valid and drops every event, so emitters need no checks.
type Bus struct {
	mu     sync.RWMutex
	subs   []*sub
	closed bool
	wg     sync.WaitGroup
}

type sub struct {
	types []Type // empty means every type
	fn    Handler
	q     *queue // nil for synchronous subscribers
}

func (s *sub) wants(t Type) bool { return len(s.types) == 0 || slices.Contains(s.types, t) }

// New returns an empty bus.
func New() *Bus { return &Bus{} }

// Subscribe registers fn to be called synchronously, on the emitting
// goroutine, before Emit returns. It filters on types (all when empty).
// The returned func removes the subscription.
func (b *Bus) Subscribe(fn Handler, types ...Type) (cancel func()) {
	return b.add(&sub{types: types, fn: fn})
}

// SubscribeAsync registers fn to be called on a goroutine of its own, in
// emit order, without ever blocking the emitter. Close waits for queued
// events to be handled.
func (b *Bus) SubscribeAsync(fn Handler, types ...Type) (cancel func()) {
	s := &sub{types: types, fn: fn, q: newQueue()}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			e, ok := s.q.pop()
			if !ok {
				return
			}
			call(fn, e)
		}
	}()
	return b.add(s)
}

func (b *Bus) add(s *sub) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		if s.q != nil {
			s.q.close()
		}
		return func() {}
	}
	b.subs = append(b.subs, s)
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			b.subs = slices.DeleteFunc(b.subs, func(x *sub) bool { return x == s })
			b.mu.Unlock()
			if s.q != nil {
				s.q.close()
			}
		})
	}
}

// Emit delivers e to every matching subscriber. A zero Time is set to now.
func (b *Bus) Emit(e Event) {
	if b == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	b.mu.RLock()
	subs := slices.Clone(b.subs)
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return
	}
	for _, s := range subs {
		if !s.wants(e.Type) {
			continue
		}
		if s.q != nil {
			s.q.push(e)
		} else {
			call(s.fn, e)
		}
	}
}

// Close stops delivery of new events and waits until async subscribers
// have handled everything queued before the call.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.closed = true
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	for _, s := range subs {
		if s.q != nil {
			s.q.close()
		}
	}
	b.wg.Wait()
}

// call runs a handler and keeps a panicking subscriber from taking the
// emitter (a CLI command, the app) down with it.
func call(fn Handler, e Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("event handler panicked", "type", e.Type, "path", e.Path, "panic", r)
		}
	}()
	fn(e)
}

// queue is an unbounded FIFO of events for one async subscriber.
type queue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []Event
	closed bool
}

func newQueue() *queue {
	q := &queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *queue) push(e Event) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, e)
	}
	q.mu.Unlock()
	q.cond.Signal()
}

// pop blocks for the next event. After close it drains what is left and
// then reports false.
func (q *queue) pop() (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return Event{}, false
	}
	e := q.items[0]
	q.items = q.items[1:]
	return e, true
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
