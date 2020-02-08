package thread

import "sync"

type rmutex struct {
	mutex sync.Mutex
	IsLocked bool
}

func newMutex() *rmutex {
	return &rmutex{}
}

func (rm *rmutex) Lock() *rmutex {
	rm.IsLocked = true
	rm.mutex.Lock()
	return rm
}

func (rm *rmutex) Unlock() *rmutex {
	rm.mutex.Unlock()
	rm.IsLocked = false
	return rm
}

func (rm *rmutex) TryLock() bool {
	if rm.IsLocked {
		return false
	}
	rm.Lock()
	return true
}
