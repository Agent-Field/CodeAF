package lease_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A Stop can land after Pull has listed the job and before it reserves it.
// The output policy runs inside List, after that job's document was read, so it
// is where the Stop lands here. Only the locked job can authorize running steps.
func TestPullHonoursStopLandingAfterListing(t *testing.T) {
	for _, reply := range []bool{false, true} {
		name := "no pending reply"
		if reply {
			name = "pending reply"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			in := recoveryJob()
			if reply {
				in.Talk = "chat"
				in.Thread = []factory.Message{{State: "pending", Text: "explain"}}
			}
			it := f.add(t, in)
			other, err := store.Open(f.store.Root())
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			f.store.SetOutputPolicy(func() (int, int) {
				once.Do(func() {
					if err := other.Update(it.ID, func(cur *factory.Item) error {
						factory.MarkPersonStopped(cur)
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				})
				return 0, 0
			})
			held, err := f.desk.Pull(worker("idle", "sam"))
			if !reply {
				if !errors.Is(err, lease.ErrNoJob) {
					t.Fatalf("a stopped job was granted: %+v, %v", held, err)
				}
				return
			}
			if err != nil || !held.ThreadOnly {
				t.Fatalf("a stopped job's reply was granted job steps: %+v, %v", held, err)
			}
			f.state(t, it.ID, factory.StateQueued)
		})
	}
}
