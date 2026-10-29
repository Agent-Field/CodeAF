package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
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
// traffic line. It stays closed when the job comes back by itself, and a
// person bringing the job back reopens it.
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
		if f.Teams[0].Report != "" {
			t.Fatalf("%s closed with a report", room)
		}
		if lines, err := teams.ReadTraffic(r.profile, room, "", 0); err != nil || len(lines) != 0 {
			t.Fatalf("%s's close said something: %+v %v", room, lines, err)
		}
	}

	// A pull request reopened on the forge brings the job back by itself.
	r.update(merged.ID, func(it *factory.Item) { it.PullRequest.State = factory.PROpen })
	if !r.closed("0a0a0a0a0a0a") {
		t.Fatal("a job that came back by itself reopened its room")
	}
	// A round of it runs teamless: nothing reopens the room.
	if got, err := factoryEnsureRoom(r.profile, merged, "0a0a0a0a0a0a"); err != nil || got != "" || !r.closed("0a0a0a0a0a0a") {
		t.Fatalf("a round on the closed room: %q %v", got, err)
	}

	// A person's Reopen clears DoneHow: its room opens with it.
	r.update(local.ID, func(it *factory.Item) { it.DoneHow, it.DoneAt = "", time.Time{} })
	if r.closed("0b0b0b0b0b0b") {
		t.Fatal("a job a person reopened kept its room closed")
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
	store.WhenWritten(func(string, factory.Item, factory.Item) {})
	r.update(ended.ID, func(it *factory.Item) { it.DoneHow = "answered" })
	store.WhenWritten(func(_ string, before, after factory.Item) {
		factoryRoomFollowsJob(factoryProfile(config.ProfileDir()), before, after)
	})
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
	factorySweepEndedRooms(r.profile, r.st.Root())
	if teams.Stamp(r.profile) != stamp {
		t.Fatal("a second sweep wrote")
	}
	_ = live
}
