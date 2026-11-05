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

// A job held at its budget takes no lease, whichever of the Stop and the budget
// came first. A stopped job gets no cap question, and with a pending reply it
// waits on a person and keeps the reply.
func TestPullHoldsAJobAtItsBudgetInAnyOrderWithAStop(t *testing.T) {
	for _, reply := range []bool{false, true} {
		for _, stopFirst := range []bool{false, true} {
			name := "budget then stop"
			if stopFirst {
				name = "stop then budget"
			}
			if reply {
				name += " with pending reply"
			}
			t.Run(name, func(t *testing.T) {
				f := setup(t)
				in := recoveryJob()
				in.Cap = 1
				if reply {
					in.Talk = "chat"
					in.Thread = []factory.Message{{State: "pending", Text: "explain"}}
				}
				if stopFirst {
					factory.MarkPersonStopped(&in)
				}
				in.SpentBefore = 2
				it := f.add(t, in)
				other, err := store.Open(f.store.Root())
				if err != nil {
					t.Fatal(err)
				}
				var once sync.Once
				f.store.SetOutputPolicy(func() (int, int) {
					once.Do(func() {
						if stopFirst {
							return
						}
						if err := other.Update(it.ID, func(cur *factory.Item) error {
							factory.MarkPersonStopped(cur)
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					})
					return 0, 0
				})
				before, err := f.store.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
				held, err := f.desk.Pull(worker("idle", "sam"))
				if !errors.Is(err, lease.ErrNoJob) {
					t.Fatalf("a job at its budget was granted: %+v, %v", held, err)
				}
				after, err := f.store.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !factory.StoppedByPerson(after) {
					t.Fatal("the Stop was lost")
				}
				if after.QKind == "cap" || after.Question != before.Question {
					t.Fatalf("a stopped job was asked to raise its budget: %q", after.Question)
				}
				if reply && (after.State != factory.StateNeedsYou || len(after.Thread) != 1 || after.Thread[0].State != "pending") {
					t.Fatalf("a stopped job's wait or reply was lost: state %q thread %+v", after.State, after.Thread)
				}
			})
		}
	}
}
