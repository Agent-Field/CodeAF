package cellsync

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// overlap imports whole frames while the next ones still download (contract
// §22.12). A partial import stores what the head can reach so far and deletes
// nothing, so the primed import that follows the last frame still sees every
// frame-mate and is the one place extras are deleted. A partial import that
// fails only costs the overlap: the final import does all of its work.
type overlap struct {
	kick   chan struct{}
	done   sync.WaitGroup
	frames atomic.Int64
}

// startOverlap starts the importer. The caller reports each landed frame with
// landed and calls finish once, after the last one.
func startOverlap(ctx context.Context, eng Engine, c cell.Cell, head, inbox string) *overlap {
	o := &overlap{kick: make(chan struct{}, 1)}
	o.done.Add(1)
	go func() {
		defer o.done.Done()
		for range o.kick {
			if _, err := eng.ImportPartial(ctx, c, head, inbox); err != nil {
				o.drain()
				return
			}
		}
	}()
	return o
}

// landed counts a frame and asks for an import every ImportEvery of them. The
// kick channel holds one request, so frames that land during an import share
// the next one.
func (o *overlap) landed() {
	if o.frames.Add(1)%ImportEvery != 0 {
		return
	}
	select {
	case o.kick <- struct{}{}:
	default:
	}
}

// drain ignores the requests that follow a failed import.
func (o *overlap) drain() {
	for range o.kick {
	}
}

// finish waits for the import in flight; no import starts after it.
func (o *overlap) finish() {
	close(o.kick)
	o.done.Wait()
}
