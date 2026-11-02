package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// AN APPROVED JOB IS NOT OVER WHILE IT PUBLISHES. Approving writes Shipped
// before any pull request exists, and publishing writes Publishing; neither
// closes the room. Its pull request merging does.
func TestApprovingAJobKeepsItsRoomOpenWhilePublishing(t *testing.T) {
	r := newRoomFollowRig(t)
	job := r.job("1a1a1a1a1a1a", factory.Item{Title: "landed", State: factory.StateLanded})
	r.update(job.ID, func(it *factory.Item) { it.State = factory.StateShipped })
	if r.closed("1a1a1a1a1a1a") {
		t.Fatal("approving closed the room")
	}
	r.update(job.ID, func(it *factory.Item) { it.Publishing = true })
	if r.closed("1a1a1a1a1a1a") {
		t.Fatal("publishing closed the room")
	}
	r.update(job.ID, func(it *factory.Item) {
		it.Publishing = false
		it.PullRequest = &factory.PullRequest{Number: 3, State: factory.PROpen}
	})
	if r.closed("1a1a1a1a1a1a") {
		t.Fatal("the open pull request closed the room")
	}
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	if !r.closed("1a1a1a1a1a1a") {
		t.Fatal("the merge did not close the room")
	}
}

// THE ROOM'S REPORTING SURVIVES ITS JOB'S END AND RETURN. The job's own
// conversation manages its room and the stage chats report to it; the quiet
// close makes nobody independent, and the reopen gives every stage its home
// in the room again.
func TestAJobsReturnKeepsItsStageChatsReporting(t *testing.T) {
	r := newRoomFollowRig(t)
	job := r.job("3c3c3c3c3c3c", factory.Item{Title: "managed", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 4, State: factory.PROpen}})
	talk := filepath.Join(r.root, "talk", "transcript.jsonl")
	if err := talkJoinTeam(r.profile, job, talk, r.root, "job"); err != nil {
		t.Fatal(err)
	}
	reporting := func(when string) {
		t.Helper()
		f, _ := teams.LoadTeams(r.profile, "3c3c3c3c3c3c")
		room := f.Teams[0]
		if room.Manager == "" {
			t.Fatalf("%s: the room has no manager", when)
		}
		for _, m := range room.Members {
			if m.Key != room.Manager && (m.Independent || !m.Home) {
				t.Fatalf("%s: a stage chat does not report to the room (home=%v independent=%v)", when, m.Home, m.Independent)
			}
		}
	}
	reporting("before")
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	f, _ := teams.LoadTeams(r.profile, "3c3c3c3c3c3c")
	for _, m := range f.Teams[0].Members {
		if m.Independent {
			t.Fatalf("the quiet close made %s independent", m.Handle)
		}
	}
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	reporting("after the job came back")
}

// THE SWEEP RUNS ONCE PER PROFILE, AND NEVER CLOSES A ROOM A PERSON REOPENED.
func TestTheSweepRunsOnceAndLeavesAPersonsReopen(t *testing.T) {
	r := newRoomFollowRig(t)
	reopenedByHand := r.job("2b2b2b2b2b2b", factory.Item{Title: "reopened by hand", State: factory.StateRunning})
	// Its job ended while nothing followed it, and the person reopened the
	// crew to keep talking there: the door writes a reopen line.
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(reopenedByHand.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "answered", time.Now() })
	store.WhenWritten(factoryRoomHook)
	if err := teams.AppendTraffic(r.profile, "2b2b2b2b2b2b", teams.Entry{Kind: teams.KindReopen, From: teams.FromYou, To: teams.ToRoom, Text: "reopened"}); err != nil {
		t.Fatal(err)
	}
	if factorySweepEndedRooms(r.profile, r.st.Root()); r.closed("2b2b2b2b2b2b") {
		t.Fatal("the sweep closed a room a person reopened")
	}

	// Once it has run, it never runs again in this profile.
	later := r.job("2c2c2c2c2c2c", factory.Item{Title: "ended unseen", State: factory.StateRunning})
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(later.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "answered", time.Now() })
	store.WhenWritten(factoryRoomHook)
	if n := factorySweepEndedRooms(r.profile, r.st.Root()); n != 0 || r.closed("2c2c2c2c2c2c") {
		t.Fatalf("the sweep ran again (%d)", n)
	}
}

// THE ROOM FOLLOWS THE JOB'S STATE, NOT THE WRITE IT WAS TOLD OF: a telling
// that arrives late, after the job came back, reopens; one the crews store
// refused while busy is tried again; and a round of a live job whose reopen
// was lost reopens the room and joins it.
func TestTheRoomFollowsTheJobsStateWhateverItIsTold(t *testing.T) {
	r := newRoomFollowRig(t)
	job := r.job("4d4d4d4d4d4d", factory.Item{Title: "flip", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 5, State: factory.PROpen}})
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	over, _ := r.st.Get(job.ID)
	if !r.closed("4d4d4d4d4d4d") {
		t.Fatal("the merge did not close the room")
	}

	// Late: the job came back, and the close's telling arrives after.
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	store.WhenWritten(factoryRoomHook)
	factoryRoomFollow(r.profile, r.st, over)
	if r.closed("4d4d4d4d4d4d") {
		t.Fatal("a late telling left a live job's room closed")
	}

	// Busy: the job ends while another writer holds the crews store past the
	// wait; the close is tried again once it lets go.
	old := roomRetries
	roomRetries = []time.Duration{50 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second, 4 * time.Second}
	t.Cleanup(func() { roomRetries = old })
	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = teams.Inspect(r.profile, func(*teams.File) error { close(held); <-release; return nil })
	}()
	<-held
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRClosed })
	close(release)
	deadline := time.Now().Add(15 * time.Second)
	for !r.closed("4d4d4d4d4d4d") {
		if time.Now().After(deadline) {
			t.Fatal("a close the busy store refused was never tried again")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Lost: the job came back but nothing reopened its room; its next round
	// does, and joins it.
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(job.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	store.WhenWritten(factoryRoomHook)
	back, _ := r.st.Get(job.ID)
	next := filepath.Join(r.root, "lost-round", "transcript.jsonl")
	exec := factoryChatStage(r.st, r.profile, factoryrun.ExecutorFunc(func(_ context.Context, j factoryrun.Job) (factory.StageResult, error) {
		if j.Item.Stream.Room != "4d4d4d4d4d4d" {
			t.Fatalf("the round ran in %q", j.Item.Stream.Room)
		}
		return factory.StageResult{}, stageJoinTeam(r.profile, j.Item.Stream.Room, next, r.root, "fix", "fix", "Fix it.")
	}))
	if _, err := exec.Run(context.Background(), factoryrun.Job{Item: back}); err != nil {
		t.Fatal(err)
	}
	if r.closed("4d4d4d4d4d4d") {
		t.Fatal("a live job's round left its room closed")
	}
}

// A STAGE STILL JOINS A ROOM CLOSED UNDER ITS ROUND. A person may close a live
// job's room, or the `factory` crew above it, while a round is going, and a
// job may end with a round still opening stages; the stage joins the closed
// room, as it always has, and the room stays as the person or the job left it.
func TestAStageJoinsARoomClosedUnderItsRound(t *testing.T) {
	r := newRoomFollowRig(t)
	byHand := r.job("5e5e5e5e5e5e", factory.Item{Title: "closed by hand", State: factory.StateRunning})
	parent := r.job("6f6f6f6f6f6f", factory.Item{Title: "parent closed", State: factory.StateRunning})
	ended := r.job("7a7a7a7a7a7a", factory.Item{Title: "ended", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 8, State: factory.PROpen}})
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Close("5e5e5e5e5e5e", time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	room, _ := teams.LoadTeams(r.profile, "6f6f6f6f6f6f")
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Close(room.Teams[0].Parent, time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	r.update(ended.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	for _, it := range []factory.Item{byHand, parent, ended} {
		room := it.Stream.Room
		if !r.closed(room) {
			t.Fatalf("%s: the rig did not close the room", it.Title)
		}
		next := filepath.Join(r.root, "later-"+room, "transcript.jsonl")
		if err := stageJoinTeam(r.profile, room, next, r.root, "fix", "fix", "Fix it."); err != nil {
			t.Fatalf("%s: the stage could not join: %v", it.Title, err)
		}
		f, _ := teams.LoadTeams(r.profile, room)
		if !f.Teams[0].Closed() || len(f.Teams[0].Members) != 2 {
			t.Fatalf("%s: after the join: closed=%v members=%d", it.Title, f.Teams[0].Closed(), len(f.Teams[0].Members))
		}
	}
}

// THE SWEEP LEAVES A ROOM A PERSON MAY HAVE REOPENED, whatever door it came
// through: a reopen of a crew above it, which writes its line there, and a
// reopen whose line has since rotated out of the room's log. A room made
// after every such line is still closed.
func TestTheSweepLeavesRoomsReopenedAboveOrLongAgo(t *testing.T) {
	r := newRoomFollowRig(t)
	unseen := func(it factory.Item) {
		store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
		r.update(it.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "answered", time.Now() })
		store.WhenWritten(factoryRoomHook)
	}
	above := r.job("8a8a8a8a8a8a", factory.Item{Title: "reopened above", State: factory.StateRunning})
	unseen(above)
	room, _ := teams.LoadTeams(r.profile, "8a8a8a8a8a8a")
	parent := room.Teams[0].Parent
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Close(parent, time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Reopen(parent) }); err != nil {
		t.Fatal(err)
	}
	if err := teams.AppendTraffic(r.profile, parent, teams.Entry{Kind: teams.KindReopen, From: teams.FromYou, To: teams.ToRoom, Text: "reopened"}); err != nil {
		t.Fatal(err)
	}

	rotated := r.job("8b8b8b8b8b8b", factory.Item{Title: "reopen rotated out", State: factory.StateRunning})
	unseen(rotated)
	// Its log's first lines, the reopen among them, are gone: what is left
	// begins well past the first id.
	log := teams.TrafficPath(r.profile, "8b8b8b8b8b8b")
	if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(teams.Entry{ID: "000000000050", At: time.Now(), Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "later"})
	if err := os.WriteFile(log, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)
	later := r.job("8c8c8c8c8c8c", factory.Item{Title: "made after the reopen", State: factory.StateRunning})
	unseen(later)

	factorySweepEndedRooms(r.profile, r.st.Root())
	if r.closed("8a8a8a8a8a8a") {
		t.Fatal("the sweep closed a room a person reopened through the crew above it")
	}
	if r.closed("8b8b8b8b8b8b") {
		t.Fatal("the sweep closed a room whose reopen line rotated out")
	}
	if !r.closed("8c8c8c8c8c8c") {
		t.Fatal("the sweep left open a room made after every reopen")
	}
}

// TWO QUICK WRITES TO ONE JOB LEAVE ITS ROOM AS THE JOB IS. The job ends, and
// its close reads it over and waits on the crews store; meanwhile the job
// comes back, and its reopen finds the room not yet closed. The close landing
// after must not leave a live job's room closed: each telling follows the job
// in turn, so the one told last reads the job last.
func TestTwoQuickWritesLeaveTheRoomAsTheJobIs(t *testing.T) {
	r := newRoomFollowRig(t)
	job := r.job("5e5e5e5e5e5e", factory.Item{Title: "quick", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 6, State: factory.PROpen}})
	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = teams.Inspect(r.profile, func(*teams.File) error { close(held); <-release; return nil })
	}()
	<-held
	ended, back := make(chan error, 1), make(chan error, 1)
	go func() {
		ended <- r.st.Update(job.ID, func(it *factory.Item) error { it.PullRequest.State = factory.PRMerged; return nil })
	}()
	time.Sleep(200 * time.Millisecond)
	go func() {
		back <- r.st.Update(job.ID, func(it *factory.Item) error { it.PullRequest.State = factory.PROpen; return nil })
	}()
	time.Sleep(300 * time.Millisecond)
	close(release)
	for _, done := range []chan error{ended, back} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if r.closed("5e5e5e5e5e5e") {
		t.Fatal("a close read before the job came back left its room closed")
	}
}

// A JOB'S TALK JOINS ITS RECORDED ROOM, OPEN OR NOT. An ended job's room is
// closed; opening its page makes its Talk, which joins that room closed
// instead of making a new open one nothing would close. A live job whose
// reopen was lost has its room opened again, so its Talk and its stages share
// one room.
func TestATalkJoinsItsRecordedRoomEvenClosed(t *testing.T) {
	r := newRoomFollowRig(t)
	open := func() int {
		t.Helper()
		f, err := teams.LoadOpen(r.profile)
		if err != nil {
			t.Fatal(err)
		}
		return len(f.Teams)
	}
	holds := func(room, key string) bool {
		t.Helper()
		f, _ := teams.LoadTeams(r.profile, room)
		return len(f.Teams) == 1 && f.Teams[0].Holds(key)
	}

	ended := r.job("0f0f0f0f0f0f", factory.Item{Title: "ended", State: factory.StateRunning})
	r.update(ended.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "closed_locally", time.Now() })
	if !r.closed("0f0f0f0f0f0f") {
		t.Fatal("the job's end did not close its room")
	}
	before := open()
	it, _ := r.st.Get(ended.ID)
	talk := filepath.Join(r.root, "talk-ended", "transcript.jsonl")
	if err := talkJoinTeam(r.profile, it, talk, r.root, talkTeamName(it)); err != nil {
		t.Fatal(err)
	}
	if got := open(); got != before {
		t.Fatalf("an ended job's Talk made %d open crews", got-before)
	}
	if !r.closed("0f0f0f0f0f0f") || !holds("0f0f0f0f0f0f", talk) {
		t.Fatal("an ended job's Talk did not join its closed room")
	}

	// Lost: the job came back but nothing reopened its room.
	live := r.job("1e1e1e1e1e1e", factory.Item{Title: "live", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen}})
	r.update(live.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(live.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	store.WhenWritten(factoryRoomHook)
	before = open()
	it, _ = r.st.Get(live.ID)
	talk = filepath.Join(r.root, "talk-live", "transcript.jsonl")
	if err := talkJoinTeam(r.profile, it, talk, r.root, talkTeamName(it)); err != nil {
		t.Fatal(err)
	}
	if r.closed("1e1e1e1e1e1e") || !holds("1e1e1e1e1e1e", talk) {
		t.Fatal("a live job's Talk did not reopen and join its room")
	}
	if got := open(); got != before+1 {
		t.Fatalf("open crews went from %d to %d, want its room alone back", before, got)
	}
}
