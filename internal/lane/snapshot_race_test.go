package lane

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// ── THE ENGINE THAT DIED MID-TURN (2026-11-18) ──────────────────────────────
//
// A hand test of the chat-move work killed a running engine with
// `fatal error: concurrent map iteration and map write`, from
// encoding/json.Marshal inside [writeState], called by the belief writer
// ([ledger.Persist]) at the moment a model call started and the ledger folded
// its first sighting in.
//
// THE CAUSE IS THE SHAPE OF THE SNAPSHOT AND NOT A MISSING LOCK. [ledger.snapshot]
// is what a compaction hands to [writeState], and it copied the hierarchies
// ([chains], [tallies]) by value: the structs were copies and their maps were the
// ledger's own. The ledger's mutex is released before [writeState] encodes, so the
// encoder walked maps a sighting was writing to. Taking the mutex around the
// encode would only have hidden that — the file's lock, the journal and a rename
// are held across it — so the snapshot is made a snapshot: a value that nothing
// else can reach.

// TestACompactionEncodesASnapshotNoSightingCanReach is contract C4.1 and C4.2.
//
// Sightings for many lanes and models are folded in, which is what grows the
// hierarchies' maps, while the writer compacts over and over. Under -race the
// shared maps are reported as a data race at the first compaction that overlaps
// a sighting, and without -race the process dies the way the engine did.
func TestACompactionEncodesASnapshotNoSightingCanReach(t *testing.T) {
	path := sharedFile(t)
	beliefs := newLedger()
	beliefs.keepIn(newStore().at(path))

	stop := make(chan struct{})
	var folding sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		folding.Add(1)
		go func() {
			defer folding.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				at := noon.Add(time.Duration(i) * time.Second)
				beliefs.Note(Sighting{
					ID:     ID{Model: fmt.Sprintf("vendor/model-%d", i%7), Lane: fmt.Sprintf("lane-%d-%d", worker, i%40)},
					TTFT:   400 * time.Millisecond,
					Gen:    time.Second,
					Tokens: 64,
					At:     at,
				})
				beliefs.NoteOutcome(Outcome{ID: ID{Model: fmt.Sprintf("vendor/model-%d", i%7), Lane: fmt.Sprintf("lane-%d-%d", worker, i%40)}, Accepted: i%3 != 0, At: at})
			}
		}()
	}
	defer func() {
		close(stop)
		folding.Wait()
	}()

	for compaction := 0; compaction < 150; compaction++ {
		if err := beliefs.write(); err != nil {
			t.Fatalf("compaction %d: %v", compaction, err)
		}
	}
}

// TestASnapshotIsUnchangedByWhatIsFoldedInAfterIt is contract C4.2 stated
// without a race detector: what a snapshot holds is fixed at the moment it is
// taken, so an observation that arrives later cannot reach the bytes that are
// about to be written.
func TestASnapshotIsUnchangedByWhatIsFoldedInAfterIt(t *testing.T) {
	beliefs := newLedger()
	beliefs.Note(aSighting("A", noon))

	beliefs.mu.Lock()
	taken := beliefs.snapshot()
	beliefs.mu.Unlock()
	lanes, models := len(taken.Wait.Lane), len(taken.Wait.Model)
	if lanes == 0 || models == 0 {
		t.Fatalf("one sighting left the first-token hierarchy empty (%d lanes, %d models); the snapshot proves nothing", lanes, models)
	}

	beliefs.Note(Sighting{ID: ID{Model: "vendor/another", Lane: "B"}, TTFT: time.Second, Gen: time.Second, Tokens: 64, At: noon.Add(time.Minute)})

	if got := len(taken.Wait.Lane); got != lanes {
		t.Errorf("a sighting folded in after the snapshot changed its lanes from %d to %d", lanes, got)
	}
	if got := len(taken.Wait.Model); got != models {
		t.Errorf("a sighting folded in after the snapshot changed its models from %d to %d", models, got)
	}
}
