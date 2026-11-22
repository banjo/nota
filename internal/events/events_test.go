package events

import (
	"slices"
	"sync"
	"testing"
	"time"
)

func TestSyncSubscriberFiltersAndRunsBeforeEmitReturns(t *testing.T) {
	b := New()
	var got []Type
	cancel := b.Subscribe(func(e Event) { got = append(got, e.Type) }, NoteCreated, NoteMoved)
	b.Emit(Event{Type: NoteCreated, Path: "a.md"})
	b.Emit(Event{Type: NoteSaved, Path: "a.md"})
	b.Emit(Event{Type: NoteMoved, Path: "b/a.md", OldPath: "a.md"})
	if !slices.Equal(got, []Type{NoteCreated, NoteMoved}) {
		t.Fatalf("got %v", got)
	}
	cancel()
	cancel() // idempotent
	b.Emit(Event{Type: NoteCreated})
	if len(got) != 2 {
		t.Fatalf("delivered after cancel: %v", got)
	}
}

func TestAsyncSubscriberKeepsOrderAndCloseDrains(t *testing.T) {
	b := New()
	var mu sync.Mutex
	var paths []string
	release := make(chan struct{})
	b.SubscribeAsync(func(e Event) {
		<-release
		mu.Lock()
		paths = append(paths, e.Path)
		mu.Unlock()
	})
	done := make(chan struct{})
	go func() {
		for _, p := range []string{"1", "2", "3"} {
			b.Emit(Event{Type: NoteSaved, Path: p})
		}
		close(done)
	}()
	select {
	case <-done: // Emit must not wait for the slow subscriber.
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked on an async subscriber")
	}
	close(release)
	b.Close()
	if !slices.Equal(paths, []string{"1", "2", "3"}) {
		t.Fatalf("paths %v", paths)
	}
	b.Emit(Event{Type: NoteSaved, Path: "after"})
	if len(paths) != 3 {
		t.Fatal("delivered after Close")
	}
}

func TestPanickingHandlerDoesNotStopOthers(t *testing.T) {
	b := New()
	b.Subscribe(func(Event) { panic("boom") })
	n := 0
	b.Subscribe(func(Event) { n++ })
	b.Emit(Event{Type: NoteDeleted})
	if n != 1 {
		t.Fatalf("second handler ran %d times", n)
	}
}

func TestNilBusAndTimestamps(t *testing.T) {
	var nb *Bus
	nb.Emit(Event{Type: NoteSaved}) // must not panic
	nb.Close()

	b := New()
	var e Event
	b.Subscribe(func(x Event) { e = x })
	b.Emit(Event{Type: InboxAdded})
	if e.Time.IsZero() {
		t.Fatal("Time not set")
	}
}

func TestFollow(t *testing.T) {
	b := New()
	where, stop := b.Follow()
	b.Emit(Event{Type: NoteMoved, OldPath: "Inbox/a.md", Path: "Work/a.md"})
	b.Emit(Event{Type: NoteMoved, OldPath: "Work/a.md", Path: "Archive/a.md"})
	b.Emit(Event{Type: NoteSaved, Path: "Inbox/b.md"})
	if got := where("Inbox/a.md"); got != "Archive/a.md" {
		t.Fatalf("where(a) = %q", got)
	}
	if got := where("Inbox/b.md"); got != "Inbox/b.md" {
		t.Fatalf("where(b) = %q", got)
	}
	// A move back and forth must not loop.
	b.Emit(Event{Type: NoteMoved, OldPath: "Archive/a.md", Path: "Inbox/a.md"})
	_ = where("Inbox/a.md")
	stop()
	b.Emit(Event{Type: NoteMoved, OldPath: "Inbox/b.md", Path: "X/b.md"})
	if got := where("Inbox/b.md"); got != "Inbox/b.md" {
		t.Fatalf("after stop: %q", got)
	}
	var nilBus *Bus
	w, s := nilBus.Follow()
	s()
	if w("x") != "x" {
		t.Fatal("nil bus")
	}
}
