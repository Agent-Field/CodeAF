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

// scope counts the wire for one name. Counting sits between the wire and the
// caller, so a test that swaps Sync.Store for a fault injector is counted the
// same way the real client is.
func (s *Sync) scope(name string) *Scope {
	c := &blobstore.Counters{}
	return &Scope{
		Store: blobstore.Counting{Inner: s.Store, C: c},
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
