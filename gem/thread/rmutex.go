package thread

import (
	"sync"

	"github.com/oruby/oruby"
)

type rmutex struct {
	mu       sync.Mutex
	isLocked bool
}

func newMutex() *rmutex {
	return &rmutex{}
}

func (rm *rmutex) Lock() (*rmutex, error) {
	rm.mu.Lock()
	if rm.isLocked {
		rm.mu.Unlock()
		return nil, oruby.EThreadError("deadlock; recursive locking")
	}
	rm.isLocked = true
	rm.mu.Unlock()
	return rm, nil
}

func (rm *rmutex) Unlock() (*rmutex, error) {
	rm.mu.Lock()
	if !rm.isLocked {
		rm.mu.Unlock()
		return nil, oruby.EThreadError("Attempt to unlock a mutex which is not locked")
	}
	rm.isLocked = false
	rm.mu.Unlock()
	return rm, nil
}

func (rm *rmutex) TryLock() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.isLocked {
		return false
	}
	rm.isLocked = true
	return true
}

func (rm *rmutex) Locked() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.isLocked
}
