package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// roomFollowRig is a factory floor and a profile whose crews it writes, with
// one job and its room, the room holding a stage chat.
type roomFollowRig struct {
	t       *testing.T
	root    string
	profile string
	st      *store.Store
}

func newRoomFollowRig(t *testing.T) *roomFollowRig {
	t.Helper()
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	t.Setenv(config.ProfileDirEnv, profile)
	st, err := store.Open(filepath.Join(root, "floor"))
	if err != nil {
		t.Fatal(err)
	}
	return &roomFollowRig{t: t, root: root, profile: profile, st: st}
}

// job makes a job and its room, with one stage in it, and answers the job.
func (r *roomFollowRig) job(room string, it factory.Item) factory.Item {
	r.t.Helper()
	it.Repo, it.Stream = "work", &factory.Stream{Room: room}
	made, err := r.st.Create(it)
	if err != nil {
		r.t.Fatal(err)
	}
	if got, err := factoryEnsureRoom(r.profile, made, room); err != nil || got != room {
		r.t.Fatalf("room %q %v", got, err)
	}
	chat := filepath.Join(r.root, room, "transcript.jsonl")
	if err := stageJoinTeam(r.profile, room, chat, r.root, "review", "review", "Look it over."); err != nil {
		r.t.Fatal(err)
	}
	return made
}

func (r *roomFollowRig) closed(room string) bool {
	r.t.Helper()
	f, err := teams.LoadTeams(r.profile, room)
	if err != nil || len(f.Teams) != 1 {
		r.t.Fatalf("room %s: %+v %v", room, f, err)
	}
	return f.Teams[0].Closed()
}

func (r *roomFollowRig) update(id int, change func(*factory.Item)) {
	r.t.Helper()
	if err := r.st.Update(id, func(it *factory.Item) error { change(it); return nil }); err != nil {
		r.t.Fatal(err)
	}
}

// A JOB'S ROOM CLOSES WHEN THE JOB ENDS, quietly: no closing report and no
// traffic line. When the job comes back, by itself (a pull request reopened
// on the forge) or by a person's Reopen, its room opens again and the next
// round joins its crew.
func TestAJobsRoomFollowsTheJob(t *testing.T) {
	r := newRoomFollowRig(t)
	merged := r.job("0a0a0a0a0a0a", factory.Item{Title: "merged", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen}})
	local := r.job("0b0b0b0b0b0b", factory.Item{Title: "closed here", State: factory.StateRunning})
	live := r.job("0c0c0c0c0c0c", factory.Item{Title: "still running", State: factory.StateRunning})

	r.update(merged.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	r.update(local.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "closed_locally", time.Now() })
	r.update(live.ID, func(it *factory.Item) { it.Title = "still running, renamed" })
	if !r.closed("0a0a0a0a0a0a") || !r.closed("0b0b0b0b0b0b") || r.closed("0c0c0c0c0c0c") {
		t.Fatal("rooms did not follow their jobs")
	}
	for _, room := range []string{"0a0a0a0a0a0a", "0b0b0b0b0b0b"} {
		f, _ := teams.LoadTeams(r.profile, room)
		if f.Teams[0].Report != "" || !f.Teams[0].JobEnded {
			t.Fatalf("%s closed with a report or unmarked: %+v", room, f.Teams[0])
		}
		if lines, err := teams.ReadTraffic(r.profile, room, "", 0); err != nil || len(lines) != 0 {
			t.Fatalf("%s's close said something: %+v %v", room, lines, err)
		}
	}

	// A pull request reopened on the forge brings the job back by itself, and
	// its room with it.
	r.update(merged.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	if r.closed("0a0a0a0a0a0a") {
		t.Fatal("a job that came back kept its room closed")
	}
	// Its next round joins the crew, as before the job ended.
	back, err := r.st.Get(merged.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(r.root, "next-round", "transcript.jsonl")
	exec := factoryChatStage(r.st, r.profile, factoryrun.ExecutorFunc(func(_ context.Context, j factoryrun.Job) (factory.StageResult, error) {
		if j.Item.Stream.Room != "0a0a0a0a0a0a" {
			t.Fatalf("the next round ran in %q", j.Item.Stream.Room)
		}
		return factory.StageResult{}, stageJoinTeam(r.profile, j.Item.Stream.Room, next, r.root, "fix", "fix", "Answer the review.")
	}))
	if _, err := exec.Run(context.Background(), factoryrun.Job{Item: back}); err != nil {
		t.Fatal(err)
	}
	room, _ := teams.LoadTeams(r.profile, "0a0a0a0a0a0a")
	if len(room.Teams) != 1 || room.Teams[0].Closed() || room.Teams[0].JobEnded || len(room.Teams[0].Members) != 2 {
		t.Fatalf("the next round's crew: %+v", room.Teams)
	}

	// A person's Reopen brings the job back too.
	r.update(local.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "", time.Time{} })
	if r.closed("0b0b0b0b0b0b") {
		t.Fatal("a job a person reopened kept its room closed")
	}
}

// A CREW A PERSON CLOSED OR ARCHIVED STAYS AS THEY LEFT IT when its job comes
// back: only a room its job's end closed is reopened.
func TestAJobsReturnLeavesAPersonsCloseAndArchive(t *testing.T) {
	r := newRoomFollowRig(t)
	shut := r.job("0d0d0d0d0d0d", factory.Item{Title: "shut by hand", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 8, State: factory.PROpen}})
	kept := r.job("0e0e0e0e0e0e", factory.Item{Title: "archived by hand", State: factory.StateShipped, PullRequest: &factory.PullRequest{Number: 9, State: factory.PROpen}})
	// The person closes one crew while its job still runs, and archives the
	// other once its job's end closed it.
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Close("0d0d0d0d0d0d", time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	r.update(shut.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRClosed })
	r.update(kept.ID, func(it *factory.Item) { it.PullRequest.State = factory.PRMerged })
	if err := teams.Update(r.profile, func(f *teams.File) error { return f.Archive("0e0e0e0e0e0e", time.Now()) }); err != nil {
		t.Fatal(err)
	}
	r.update(shut.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	r.update(kept.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	if !r.closed("0d0d0d0d0d0d") || !r.closed("0e0e0e0e0e0e") {
		t.Fatal("a job's return reopened a crew a person closed or archived")
	}
}

// THE SWEEP CLOSES THE ROOMS OF JOBS THAT ENDED BEFORE ROOMS FOLLOWED JOBS,
// and only those: a live job's room, the factory's parent, the root and a
// person's crew stay open, and running it again changes nothing.
func TestTheSweepClosesOnlyEndedJobsRooms(t *testing.T) {
	r := newRoomFollowRig(t)
	ended := r.job("0d0d0d0d0d0d", factory.Item{Title: "ended", State: factory.StateRunning})
	live := r.job("0e0e0e0e0e0e", factory.Item{Title: "live", State: factory.StateRunning})
	// The job ended while nothing followed it: its document says so, its room
	// is still open.
	store.WhenWritten(func(*store.Store, factory.Item, factory.Item) {})
	r.update(ended.ID, func(it *factory.Item) { it.DoneHow = "answered" })
	store.WhenWritten(factoryRoomHook)
	if r.closed("0d0d0d0d0d0d") {
		t.Fatal("the room closed without the hook")
	}
	if err := teams.UpdateOpen(r.profile, func(f *teams.File) error {
		f.Teams = append(f.Teams, teams.Team{ID: "0f0f0f0f0f0f", Name: "a person's crew", Made: time.Now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := factorySweepEndedRooms(r.profile, r.st.Root()); n != 1 {
		t.Fatalf("looked at %d ended jobs", n)
	}
	if !r.closed("0d0d0d0d0d0d") || r.closed("0e0e0e0e0e0e") || r.closed("0f0f0f0f0f0f") {
		t.Fatal("the sweep closed the wrong rooms")
	}
	open, err := teams.LoadOpen(r.profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(open.Teams) != 3 { // the factory parent, the live room, the person's crew
		t.Fatalf("open after the sweep: %d", len(open.Teams))
	}
	stamp := teams.Stamp(r.profile)
	if n := factorySweepEndedRooms(r.profile, r.st.Root()); n != 0 || teams.Stamp(r.profile) != stamp {
		t.Fatalf("a second sweep ran (%d) or wrote", n)
	}
	_ = live
}
