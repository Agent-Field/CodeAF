package cellstore

import (
	"errors"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// An explicit stop joins daemon starts and invalidates the recorders that
// owned them. A new recorder may replay the WAL, but old captures and deferred
// syncs cannot reopen a stopped engine. Refusal is not ErrUnavailable because
// fallback must not start a second writer.
const daemonStopWeight = 1 << 60

var (
	daemonLifecycle  = semaphore.NewWeighted(daemonStopWeight)
	daemonClock      atomic.Uint64
	recorderFence    atomic.Uint64
	stoppedDaemons   sync.Map
	errDaemonStopped = errors.New("saving engine was explicitly stopped; pending records remain for retry")
)

type daemonOwnerKey struct{}

type stoppedDaemon struct {
	generation uint64
	resumed    atomic.Bool
}
