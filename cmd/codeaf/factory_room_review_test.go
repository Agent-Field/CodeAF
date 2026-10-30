package main

import (
	"context"
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
