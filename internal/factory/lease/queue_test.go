package lease_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// FIFTEEN SLOTS WOKEN BY ONE CHANGE SHARE ONE READING OF IT. Every runner slot
// is a worker with a pull of its own, and each look listed and deep-copied
// every job on the floor and sorted the lot: one change on the seventh live
// run's floor of 86 jobs cost fifteen full listings. The desk keeps the queue
// it read last; a look at an unchanged floor reads no job and sorts nothing,
// and a change reads only the job whose file changed.
func TestSlotsWokenByOneChangeShareOneReadingOfIt(t *testing.T) {
	f := setup(t)
	var jobs []factory.Item
	for range 40 {
		// Somebody else's work: on the floor, never this person's to take.
		jobs = append(jobs, f.add(t, factory.Item{Kind: "issue", Title: "theirs", Assignee: "kim"}))
	}
	slots := make([]lease.Worker, 15)
	for i := range slots {
		slots[i] = lease.Worker{ID: fmt.Sprintf("slot-%d", i), Handle: "sam", Machine: "mac", Slots: 15}
	}
	builds, reads := lease.QueueCounts()
	look := func() {
		t.Helper()
		for _, slot := range slots {
			if _, err := f.desk.Pull(slot); !errors.Is(err, lease.ErrNoJob) {
				t.Fatalf("%s took something from a floor with nothing for it: %v", slot.ID, err)
			}
		}
	}
	look()
	b, r := lease.QueueCounts()
	if b-builds != 1 || r-reads != 40 {
		t.Fatalf("fifteen looks at a new floor of 40 jobs built %d queues and read %d jobs; want 1 and 40", b-builds, r-reads)
	}
	for range 3 {
		look()
	}
	b2, r2 := lease.QueueCounts()
	if b2 != b || r2 != r {
		t.Fatalf("45 looks at an unchanged floor built %d queues and read %d jobs; want none", b2-b, r2-r)
	}

	// One job is handed to this person: the next look reads that one job and
	// finds it, ahead of nothing else.
	if err := f.store.Update(jobs[7].ID, func(it *factory.Item) error { it.Assignee = "sam"; return nil }); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(slots[0])
	if err != nil || held.Job != jobs[7].ID {
		t.Fatalf("the job handed over was not found: %+v %v", held, err)
	}
	b3, r3 := lease.QueueCounts()
	if b3-b2 != 1 || r3-r2 != 1 {
		t.Fatalf("one changed job built %d queues and read %d jobs; want 1 and 1", b3-b2, r3-r2)
	}
	// Taking it rewrote it, which the next look reads once; the rest share it.
	for _, slot := range slots[1:] {
		if _, err := f.desk.Pull(slot); !errors.Is(err, lease.ErrNoJob) {
			t.Fatalf("%s: %v", slot.ID, err)
		}
	}
	b4, r4 := lease.QueueCounts()
	if b4-b3 != 1 || r4-r3 != 1 {
		t.Fatalf("fourteen looks after the take built %d queues and read %d jobs; want 1 and 1", b4-b3, r4-r3)
	}
}

// A JOB ALREADY HELD AT ITS BUDGET IS NOT SAVED AGAIN BY A LOOK. A job that
// has spent its budget with a reply to its manager pending stays eligible, so
// every look finds it and holds it at its budget once more; that hold, already
// in place, saved it unchanged as a fresh file, and a fresh file wakes every
// open pull, each of which found it and saved it again. The first look holds
// it (one save); the next ten leave its document exactly as it was.
func TestALookLeavesAJobAtItsBudgetAsItIsOnDisk(t *testing.T) {
	f := setup(t)
	job := f.add(t, factory.Item{Kind: "issue", Title: "spent", Assignee: "sam", Cap: 1, SpentBefore: 2, Talk: "manager",
		Thread: []factory.Message{{ID: "reply", State: "pending", Text: "status?"}}})
	if _, err := f.desk.Pull(worker("w1", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("a job at its budget was handed out: %v", err)
	}
	f.state(t, job.ID, factory.StateNeedsYou)
	before, err := f.store.Revision(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	info := func() string {
		t.Helper()
		st, err := os.Stat(f.store.ItemPath(job.ID))
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(st.Size(), st.ModTime().UnixNano(), st.Sys())
	}
	was := info()
	for range 10 {
		if _, err := f.desk.Pull(worker("w1", "sam")); !errors.Is(err, lease.ErrNoJob) {
			t.Fatalf("a job at its budget was handed out: %v", err)
		}
	}
	if after, err := f.store.Revision(job.ID); err != nil || after != before {
		t.Fatalf("ten looks moved the held job's revision from %d to %d (%v)", before, after, err)
	}
	if info() != was {
		t.Fatal("ten looks rewrote the file of a job already held at its budget")
	}
}
