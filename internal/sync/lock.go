package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// LockFile is the lock that keeps a second engine off a vault.
const LockFile = "sync.lock"

// DefaultStaleLock is how old a lock file may get before it counts as
// abandoned. A running engine touches its lock much more often.
const DefaultStaleLock = 2 * time.Minute

// LockInfo is what a lock file records about its holder.
type LockInfo struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

// LockedError is returned when another engine holds the vault's lock.
type LockedError struct {
	Path   string
	Holder LockInfo
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("sync: the vault is already being synced (pid %d on %s since %s; lock %s)",
		e.Holder.PID, e.Holder.Host, e.Holder.Started.Format(time.RFC3339), e.Path)
}

// ErrLocked matches a *LockedError with errors.Is.
var ErrLocked = errors.New("sync: vault locked")

func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// lock is a held lock file, kept fresh by a heartbeat.
type lock struct {
	path string
	stop chan struct{}
	done chan struct{}
}

// acquireLock takes the lock file at path. A lock whose holder is a dead
// process on this host, or that has not been touched for stale, is taken
// over.
func acquireLock(path string, stale time.Duration) (*lock, error) {
	host, _ := os.Hostname()
	me := LockInfo{PID: os.Getpid(), Host: host, Started: time.Now().UTC().Truncate(time.Second)}
	data, _ := json.Marshal(me)
	for attempt := 0; ; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, werr := f.Write(data)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				os.Remove(path)
				return nil, werr
			}
			l := &lock{path: path, stop: make(chan struct{}), done: make(chan struct{})}
			go l.heartbeat(stale / 4)
			return l, nil
		}
		if !errors.Is(err, os.ErrExist) || attempt > 0 {
			if errors.Is(err, os.ErrExist) {
				return nil, lockedBy(path)
			}
			return nil, fmt.Errorf("sync: lock: %w", err)
		}
		holder, age, err := readLock(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) && age < stale {
			return nil, &LockedError{Path: path} // unreadable but fresh: someone is writing it
		}
		dead := holder.Host == host && holder.PID > 0 && holder.PID != os.Getpid() && !pidAlive(holder.PID)
		if err == nil && !dead && age < stale {
			return nil, &LockedError{Path: path, Holder: holder}
		}
		os.Remove(path) // stale: take it over
	}
}

func lockedBy(path string) error {
	holder, _, _ := readLock(path)
	return &LockedError{Path: path, Holder: holder}
}

func readLock(path string) (LockInfo, time.Duration, error) {
	st, err := os.Stat(path)
	if err != nil {
		return LockInfo{}, 0, err
	}
	age := time.Since(st.ModTime())
	data, err := os.ReadFile(path)
	if err != nil {
		return LockInfo{}, age, err
	}
	var li LockInfo
	if err := json.Unmarshal(data, &li); err != nil {
		return LockInfo{}, age, err
	}
	return li, age, nil
}

func (l *lock) heartbeat(every time.Duration) {
	defer close(l.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			now := time.Now()
			_ = os.Chtimes(l.path, now, now)
		}
	}
}

func (l *lock) release() error {
	close(l.stop)
	<-l.done
	err := os.Remove(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
