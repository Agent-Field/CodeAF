package syncsetup

import (
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

// Scope is one attributed use of the relay's store. Everything moved through
// its Store is counted, and the counts are written under the scope's name: a
// cell's id for what serves that chat, cellstats.VaultScope for what carries
// the person's secrets. Because every use of the wire is made through a Scope,
// the stats files of a machine add up to what the relay counted, with no
// request outside all of them.
type Scope struct {
	Store blobstore.Store
	rec   *cellstats.Recorder
}

// scope is the one place the wire is wrapped: wire, then Counting, then
// Resuming. Counting sits below Resuming on purpose. Resuming answers a frame it
// already delivered without asking the relay, and probes the relay with has
// requests after a timeout; the relay counts exactly the requests that reach it,
// so counting beneath Resuming is what keeps the stats equal to the relay's
// view. Each scope gets its own Resuming, whose memory of delivered frames is
// then the memory of one chat's publishes or one vault run, and never leaks
// frames between scopes. Sync.Store may be swapped by a test for a fault
// injector, and is counted the same way as the real client.
func (s *Sync) scope(name string) *Scope {
	c := &blobstore.Counters{}
	return &Scope{
		Store: blobstore.NewResuming(blobstore.Counting{Inner: s.Store, C: c}),
		rec:   cellstats.NewRecorder(s.Home, name, meter(c)),
	}
}

// Flushed writes the line of one publish: what the flush carried and what it
// cost. It is the Batcher's OnFlush hook.
func (sc *Scope) Flushed(f cellsync.Flush) { sc.rec.OnFlush(f) }

// Settle writes whatever the scope has counted that no flush carried. It is
// safe to call at any point and writes nothing when there is nothing to write.
func (sc *Scope) Settle() { sc.rec.Settle() }

// meter reads a tally as the recorder's snapshot.
func meter(c *blobstore.Counters) cellstats.Meter {
	return cellstats.MeterFunc(func() cellstats.Counts {
		return cellstats.Counts{
			Puts: c.Puts.Load(), Gets: c.Gets.Load(), Has: c.Has.Load(),
			BytesUp: c.BytesUp.Load(), BytesDown: c.BytesDown.Load(),
		}
	})
}
