package cellstore

import (
	"errors"
	"sync"
)

// An explicit stop joins daemon starts, then fences deferred syncs for each
// owned socket. The refusal is not ErrUnavailable: fallback must not spawn a
// second writer. Group commit retains its records until a fresh capture reopens
// the engine and a later durability boundary succeeds.
var (
	daemonLifecycle  sync.RWMutex
	stoppedDaemons   sync.Map
	errDaemonStopped = errors.New("saving engine was explicitly stopped; pending records remain for retry")
)
