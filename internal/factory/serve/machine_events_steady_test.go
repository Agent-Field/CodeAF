package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A MACHINE RUNNING TWO JOBS IS NOT NEWS ON EVERY HEARTBEAT (live e2e round 9,
// idle CPU): one computer's two runner slots each held a job and beat in turn,
// the roster's row named whichever job beat last, and every beat announced
// "busy on 5", then "busy on 6", with a rewrite of the live record that woke
// every watcher of the factory's folder. The machine is busy on both the
// whole time; its row keeps the job it was announced with while it holds it.
func TestAMachineBusyOnTwoJobsAnnouncesNothingAsTheyBeatInTurn(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := func() time.Time { return now }
	desk, err := lease.Open(st, lease.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		it, err := st.Create(factory.Item{Title: "work", Repo: "owner/repo", State: factory.StateQueued})
		if err != nil {
			t.Fatal(err)
		}
		if err := desk.Assign(it.ID, "priya"); err != nil {
			t.Fatal(err)
		}
	}
	slots := []lease.Worker{
		{ID: "slot-1", Handle: "priya", Machine: "spark", Slots: 2},
		{ID: "slot-2", Handle: "priya", Machine: "spark", Slots: 2},
	}
	creds := map[[32]byte]registeredWorker{}
	var held []lease.Lease
	for i, w := range slots {
		creds[[32]byte{byte(i + 1)}] = registeredWorker{Name: "spark", Last: now, Worker: w}
		l, err := desk.Pull(w)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, l)
	}
	h := &Handler{config: Config{Root: root, Now: clock}, workers: &workerService{now: clock, desk: desk, credentials: creds}}
	defer h.Close()
	h.machineEvents()
	announced, _, err := live.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	for beat := range 10 {
		now = now.Add(time.Second)
		l := held[beat%2]
		if _, err := desk.Heartbeat(l.Job, l.Worker); err != nil {
			t.Fatal(err)
		}
		h.machineEvents()
	}
	events, _, err := live.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(announced) {
		t.Fatalf("ten heartbeats of a machine busy on both its jobs announced %d events: %+v", len(events)-len(announced), events[len(announced):])
	}
}
