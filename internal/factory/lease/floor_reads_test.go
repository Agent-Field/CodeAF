package lease_test

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestIdleFloorReadsOnlyChangedJobs(t *testing.T) {
	for _, route := range []string{"Pull", "OverLimit"} {
		t.Run(route, func(t *testing.T) {
			f := setup(t)
			for range 300 {
				f.add(t, factory.Item{State: factory.StateNew})
			}
			look := func() {
				t.Helper()
				if route == "Pull" {
					if _, err := f.desk.Pull(worker("idle", "sam")); !errors.Is(err, lease.ErrNoJob) {
						t.Fatalf("Pull: %v", err)
					}
				} else if n, err := f.desk.OverLimit(worker("idle", "sam")); err != nil || n != 0 {
					t.Fatalf("OverLimit: %d, %v", n, err)
				}
			}
			look()
			before := f.store.DocumentReads()
			for range 3 {
				look()
			}
			if n := f.store.DocumentReads() - before; n != 0 {
				t.Fatalf("unchanged 300-job floor: read %d documents, want 0", n)
			}
			other, err := store.Open(f.store.Root())
			if err != nil {
				t.Fatal(err)
			}
			if err := other.Update(150, func(it *factory.Item) error { it.Title = "changed elsewhere"; return nil }); err != nil {
				t.Fatal(err)
			}
			before = f.store.DocumentReads()
			look()
			if n := f.store.DocumentReads() - before; n != 1 {
				t.Fatalf("one changed job: read %d documents, want 1", n)
			}
		})
	}
}

func TestPullAndCapacitySeeExternalFloorChanges(t *testing.T) {
	for _, change := range []string{"stop", "start", "budget", "create", "delete", "take", "release", "rename"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			in := factory.Item{State: factory.StateQueued, Taker: "sam", OverLimit: true, Cap: 10,
				Autonomy: factory.AutonomyAuto, Stream: &factory.Stream{Spent: 2}, Recovery: &factory.StepRecovery{State: "queued"}}
			if change == "start" || change == "create" {
				in.State = factory.StateNew
			}
			if change == "release" {
				in.Taker = "other"
			}
			it := f.add(t, in)
			if _, err := f.desk.OverLimit(worker("idle", "sam")); err != nil {
				t.Fatal(err)
			}
			other, err := store.Open(f.store.Root())
			if err != nil {
				t.Fatal(err)
			}
			want := false
			switch change {
			case "create":
				in.State = factory.StateQueued
				it, err = other.Create(in)
				want = true
			case "delete":
				err = os.Remove(other.ItemPath(it.ID))
			case "rename":
				path := other.ItemPath(it.ID)
				info, e := os.Stat(path)
				if e != nil {
					t.Fatal(e)
				}
				data, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if strings.Count(string(data), `"sam"`) != 1 {
					t.Fatal("expected one owner to replace")
				}
				tmp := path + ".replacement"
				if e = os.WriteFile(tmp, []byte(strings.Replace(string(data), `"sam"`, `"pat"`, 1)), 0o600); e != nil {
					t.Fatal(e)
				}
				if e = os.Chtimes(tmp, info.ModTime(), info.ModTime()); e != nil {
					t.Fatal(e)
				}
				err = os.Rename(tmp, path)
			default:
				err = other.Update(it.ID, func(cur *factory.Item) error {
					switch change {
					case "stop":
						factory.MarkPersonStopped(cur)
					case "start":
						factory.ClearPersonStop(cur)
						cur.State = factory.StateQueued
						want = true
					case "budget":
						cur.Cap = 1
					case "take":
						cur.Taker = "other"
					case "release":
						cur.Taker = ""
						want = true
					}
					return nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			w := worker("idle", "sam")
			w.TakesUnassigned = map[string]bool{"acme/api": true}
			n, err := f.desk.OverLimit(w)
			if err != nil || (n == 1) != want || n > 1 {
				t.Fatalf("capacity after %s: %d, %v; eligible=%v", change, n, err, want)
			}
			held, err := f.desk.Pull(w)
			if want {
				if err != nil || held.Job != it.ID {
					t.Fatalf("next Pull missed %s: %+v, %v", change, held, err)
				}
			} else if !errors.Is(err, lease.ErrNoJob) {
				t.Fatalf("next Pull admitted after %s: %+v, %v", change, held, err)
			}
		})
	}
}

// The live output-policy seam runs after List has copied its document, before
// returning the candidate. Commit a second store's change there to force the
// race between candidate selection and the locked reservation without sleeps.
func TestPullRechecksLockedJobAfterListing(t *testing.T) {
	for _, change := range []string{"stop", "stop with reply", "budget", "take"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			in := recoveryJob()
			if change == "budget" {
				in.Recovery = &factory.StepRecovery{State: "queued"}
			}
			if change == "stop with reply" {
				in.Talk = "chat"
				in.Thread = []factory.Message{{State: "pending", Text: "explain"}}
			}
			it := f.add(t, in)
			if _, err := f.store.List(); err != nil {
				t.Fatal(err)
			}
			other, err := store.Open(f.store.Root())
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			f.store.SetOutputPolicy(func() (int, int) {
				once.Do(func() {
					if err := other.Update(it.ID, func(cur *factory.Item) error {
						switch change {
						case "stop", "stop with reply":
							factory.MarkPersonStopped(cur)
						case "budget":
							cur.Cap = 1
						case "take":
							cur.Assignee = "other"
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				})
				return 0, 0
			})
			held, err := f.desk.Pull(worker("idle", "sam"))
			if change == "stop with reply" {
				if err != nil || !held.ThreadOnly {
					t.Fatalf("Stop during selection granted job steps: %+v, %v", held, err)
				}
				f.state(t, it.ID, factory.StateQueued)
			} else if !errors.Is(err, lease.ErrNoJob) {
				t.Fatalf("locked reservation ignored %s: %+v, %v", change, held, err)
			}
		})
	}
}

func TestFloorLooksRaceExternalWrites(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{State: factory.StateQueued, Assignee: "sam", OverLimit: true})
	other, err := store.Open(f.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 30 {
				if _, err := f.desk.Pull(worker("stranger", "other")); !errors.Is(err, lease.ErrNoJob) {
					t.Error(err)
				}
				if _, err := f.desk.OverLimit(worker("idle", "sam")); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	close(start)
	for i := range 30 {
		stopped := i%2 == 0
		if err := other.Update(it.ID, func(cur *factory.Item) error {
			if stopped {
				factory.MarkPersonStopped(cur)
			} else {
				factory.ClearPersonStop(cur)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		n, err := f.desk.OverLimit(worker("idle", "sam"))
		if err != nil || (n == 0) != stopped {
			t.Errorf("next look after write %d: capacity=%d, stopped=%v, err=%v", i, n, stopped, err)
		}
	}
	wg.Wait()
}
