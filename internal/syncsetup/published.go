package syncsetup

import (
	"sync"
	"sync/atomic"
)

var publishListeners sync.Map
var publishListenerID atomic.Uint64

// WatchPublishes subscribes a window to completed sync publishes. Listeners
// must only schedule work: a client's network cannot delay a chat's publish.
func WatchPublishes(fn func()) func() {
	id := publishListenerID.Add(1)
	publishListeners.Store(id, fn)
	return func() { publishListeners.Delete(id) }
}

func published() {
	publishListeners.Range(func(_, value any) bool { value.(func())(); return true })
}
