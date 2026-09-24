package service

import (
	"sync"
)

// keyedMutex serializes operations sharing a string key (one generator's
// annual quota) within this process. Locks are retained because the key set
// (generator codes) is bounded and stable.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[string]*sync.Mutex)}
}

func (k *keyedMutex) Lock(key string) *sync.Mutex {
	k.mu.Lock()
	lock, ok := k.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		k.locks[key] = lock
	}
	k.mu.Unlock()
	lock.Lock()
	return lock
}
