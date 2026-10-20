package factory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func openLocal(t *testing.T) (string, *store.Store) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, st
}

// What the chat adds is on the floor at the next Load, with its repo.
func TestLocalLoadReflectsAdds(t *testing.T) {
	_, st := openLocal(t)
	started := time.Now().Add(-time.Hour)
	seam := factory.LocalSeam(st, started)
	snap, err := seam.Load()
	if err != nil || len(snap.Items) != 0 || len(snap.Repos) != 0 || snap.Daily != 0 || snap.Shift.Arrived != 0 {
		t.Fatalf("an empty store's floor = %+v, %v", snap, err)
	}
	id, err := st.Add(context.Background(), factory.Item{Repo: "agentfield/codeaf", Title: "the write half"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(context.Background(), factory.Item{Repo: "agentfield/agentfield", Title: "older", Created: started.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	snap, err = seam.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Items) != 2 || snap.Items[0].ID != id {
		t.Fatalf("Items = %+v, want the Add newest first", snap.Items)
	}
	if len(snap.Repos) != 2 || snap.Repos[0].Name != "agentfield/agentfield" || snap.Repos[1].Name != "agentfield/codeaf" {
		t.Fatalf("Repos = %+v", snap.Repos)
	}
	if len(snap.Repos[0].Recipe.For(factory.KindIssue)) == 0 {
		t.Fatal("a derived repo carries no recipe")
	}
	if snap.Shift.Arrived != 1 || !snap.Shift.Since.Equal(started) {
		t.Fatalf("Shift = %+v, want one arrival since the window opened", snap.Shift)
	}
	if snap.Benches != 0 {
		t.Fatalf("a real floor drew benches %d", snap.Benches)
	}
	if len(snap.Sources) != 2 || snap.Sources[0].Name != "chat" || snap.Sources[0].Writes || snap.Sources[1].Name != "terminal" {
		t.Fatalf("Sources = %+v", snap.Sources)
	}
}

func TestLocalLoadFillsTheShift(t *testing.T) {
	_, st := openLocal(t)
	started := time.Now().Add(-time.Hour)
	now := time.Now()
	before := started.Add(-time.Minute)
	mk := func(it factory.Item) {
		t.Helper()
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	mk(factory.Item{Repo: "acme/web", Num: 42, State: factory.StateShipped, Created: now, Changed: now,
		Stream: &factory.Stream{Started: now, Ended: now}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateNeedsYou, Question: "which file?", Created: now, Changed: now,
		Asking: &factory.Asked{At: now, Question: "which file?"}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateRunning, Created: now, Changed: now,
		Stream: &factory.Stream{Started: now, Log: []factory.LogLine{{At: now, Tone: "said", Text: "yes · carrying on"}}}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateRunning, Created: now, Changed: now,
		Activity: []factory.Event{{At: now, What: "signed off"}}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateRunning, Created: now, Changed: now,
		Stream: &factory.Stream{Spent: 1.25, Started: now}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateRunning, Created: before, Changed: now,
		Stream: &factory.Stream{Spent: 2, Started: before}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateLanded, Question: "prove it", Created: now, Changed: now,
		Asking: &factory.Asked{At: now, Question: "prove it"}})
	mk(factory.Item{Repo: "acme/web", State: factory.StateNeedsYou, Question: "old", Created: before, Changed: before,
		Asking: &factory.Asked{At: before, Question: "old"}})

	snap, err := factory.LocalSeam(st, started).Load()
	if err != nil {
		t.Fatal(err)
	}
	sh := snap.Shift
	if !sh.Since.Equal(started) || sh.Arrived != 6 || sh.Shipped != 1 || len(sh.Shipping) != 1 || sh.Shipping[0] != "#42" {
		t.Fatalf("shift counts = %+v", sh)
	}
	if sh.Asked != 1 || sh.Handled != 2 || sh.Spent != 1.25 || sh.SpentToday != 3.25 {
		t.Fatalf("shift money = %+v", sh)
	}
	if sh.Hours[now.Hour()] == 0 {
		t.Fatalf("hours = %v", sh.Hours)
	}
}

// The words a person types become a title and chips.
func TestLocalNewLiftsChips(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	id, err := seam.New("agentfield/codeaf", "fix the ledger double count, two rounds, security, $8, plan first")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := seam.Load()
	it := snap.Items[0]
	if it.ID != id || it.Title != "fix the ledger double count" {
		t.Fatalf("title = %q", it.Title)
	}
	// `plan first` is an approve step after plan, which the default already
	// has: one, never two.
	if it.Cap != 8 || len(it.Stages) < 2 || it.Stages[1].Name != factory.ApproveName || factory.StageIndex(it.Stages, "approve2") >= 0 {
		t.Fatalf("stages %+v cap %v; want approve after plan, 8", it.Stages, it.Cap)
	}
	if it.Ask == nil || it.Ask.Text != "fix the ledger double count" || it.Ask.Source != factory.AskTUI || it.Ask.At.IsZero() {
		t.Fatalf("ask = %+v", it.Ask)
	}
	if it.Origin != factory.OriginTerminal || it.State != factory.StateNew || it.Tier != factory.TierOwner || it.Kind != factory.KindIssue || it.Num != 0 {
		t.Fatalf("New = %+v", it)
	}
	if it.Triage.Type != "bug" {
		t.Fatalf("type = %q, want bug from the word fix", it.Triage.Type)
	}
	sec := factory.StageIndex(it.Stages, "security")
	if sec < 0 || !it.Stages[sec].On {
		t.Fatalf("security stage = %+v, want on", it.Stages)
	}
	if rv := factory.StageIndex(it.Stages, "review"); rv < 0 || it.Stages[rv].Max != 2 {
		t.Fatalf("review rounds = %+v, want 2", it.Stages)
	}
	if _, err := seam.New("agentfield/codeaf", "$8, plan first"); err == nil {
		t.Fatal("New made an item of chips alone")
	}
	if _, err := seam.New("", "fix it"); err == nil {
		t.Fatal("New made an item on no repo")
	}
}

// Every door that changes an item writes it to disk: a fresh store on the
// same root reads the change back.
func TestLocalDoorsPersistAcrossOpen(t *testing.T) {
	root, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	id, err := seam.New("agentfield/codeaf", "meter crashes at midnight")
	if err != nil {
		t.Fatal(err)
	}
	keep, err := seam.New("agentfield/codeaf", "probes fire once")
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(seam.SetCap(keep, 12.5))
	must(seam.SetStage(keep, 0, false))
	must(seam.AddStage(keep, "after review, make it neater"))
	must(seam.SetEffort(keep, 2, "strong"))
	must(seam.Dismiss(id))
	if seam.SetCap(keep, -1) == nil || seam.SetStage(keep, 99, true) == nil ||
		seam.AddStage(keep, "after review") == nil || seam.SetEffort(keep, 0, "furious") == nil {
		t.Fatal("a door took a value it cannot mean")
	}

	again, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	gone, _ := again.Get(id)
	if gone.State != factory.StateDismissed {
		t.Fatalf("dismissed item reads back %q", gone.State)
	}
	it, _ := again.Get(keep)
	if it.Cap != 12.5 || it.Stages[0].On || it.Stages[2].Effort != "strong" {
		t.Fatalf("chips did not persist: %+v", it)
	}
	at := factory.StageIndex(it.Stages, "make")
	if at != factory.StageIndex(it.Stages, "review")+1 || it.Stages[at].Kind != factory.StageChat || it.Stages[at].Ask != "make it neater" || !it.Stages[at].On || it.Stages[at].By != factory.ByYou {
		t.Fatalf("added stage = %+v", it.Stages)
	}
}

// No store is no floor, and a typed nil store is no store.
func TestLocalSeamOnNilStoreIsZero(t *testing.T) {
	var none *store.Store
	for _, s := range []factory.Seam{factory.LocalSeam(nil, time.Now()), factory.LocalSeam(none, time.Now())} {
		if s.Has("load") || s.Has("new") || s.Has("dismiss") {
			t.Fatal("a nil store drew a door")
		}
	}
}

// The engine's doors are absent, so the page draws no key for them.
func TestLocalSeamEngineDoorsAbsent(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	for _, door := range []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sendback", "reverify", "bank", "sync", "askauthor", "bankstages"} {
		if seam.Has(door) {
			t.Errorf("door %s is present on the local seam", door)
		}
	}
	for _, door := range []string{"load", "new", "dismiss", "setcap", "setstage", "addstage", "seteffort"} {
		if !seam.Has(door) {
			t.Errorf("door %s is missing from the local seam", door)
		}
	}
}

// BENCHES ARE THE ITEMS RUNNING, and a floor with none draws nothing.
func TestLoadFillsBenchesAndDaily(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now())
	snap, err := seam.Load()
	if err != nil || snap.Benches != 0 || snap.Daily != 0 {
		t.Fatalf("an empty floor drew %d, %v, %v", snap.Benches, snap.Daily, err)
	}
	now := time.Now()
	for _, it := range []factory.Item{
		{Repo: "a/b", State: factory.StateRunning, Changed: now, Stream: &factory.Stream{Spent: 1.5}},
		{Repo: "a/b", State: factory.StateRunning, Changed: now, Stream: &factory.Stream{Spent: 0.5}},
		{Repo: "a/b", State: factory.StateLanded, Changed: now.Add(-72 * time.Hour), Stream: &factory.Stream{Spent: 7}},
	} {
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	if snap, _ = seam.Load(); snap.Benches != 2 || snap.Daily != 2 {
		t.Fatalf("Benches %d Daily %v", snap.Benches, snap.Daily)
	}
}

// directDoors is a runner whose every door answers "direct", so a test can
// tell its doors were the ones called.
type directDoors struct{}

func (directDoors) Launch(int) error                { return errDirect }
func (directDoors) Stop(int) error                  { return errDirect }
func (directDoors) Pause(int) error                 { return errDirect }
func (directDoors) Answer(int, bool, string) error  { return errDirect }
func (directDoors) Steer(int, string) error         { return errDirect }
func (directDoors) SignOff(int, bool) (bool, error) { return false, errDirect }
func (directDoors) SendBack(int, string) error      { return errDirect }
func (directDoors) Reverify(int) error              { return errDirect }

var errDirect = errors.New("direct")

// TestTheRunnerHangsItsDoors: a seam handed a runner calls it, and one with
// none draws no runner door.
func TestTheRunnerHangsItsDoors(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(directDoors{}))
	if err := seam.Stop(1); !errors.Is(err, errDirect) {
		t.Fatalf("stop went %v, not through the runner", err)
	}
	if bare := factory.LocalSeam(st, time.Now()); bare.Has("launch") || bare.Has("stop") {
		t.Fatal("a seam with no runner draws runner doors")
	}
}

// THE CLONE DOOR IS A DOOR LIKE ANY OTHER: Has names it, and nil is absent.
// AND THE FLOOR SAYS WHERE EACH REPOSITORY IS CHECKED OUT only when the seam
// was told: nil without WithRepoDirs, and the known folders with it.
func TestSeamHasCloneAndLoadCarriesCheckouts(t *testing.T) {
	if (factory.Seam{}).Has("clone") {
		t.Fatal("the zero seam has a clone door")
	}
	s := factory.Seam{Clone: func(context.Context, string) (string, error) { return "", nil }}
	if !s.Has("clone") {
		t.Fatal("a seam with Clone does not say it has a clone door")
	}

	_, st := openLocal(t)
	if _, err := st.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(context.Background(), factory.Item{Repo: "acme/web", Title: "two"}); err != nil {
		t.Fatal(err)
	}
	snap, err := factory.LocalSeam(st, time.Now()).Load()
	if err != nil || snap.Checkouts != nil {
		t.Fatalf("a seam with no dirs said checkouts %v (%v)", snap.Checkouts, err)
	}
	dirs := func(repo string) string {
		if repo == "acme/api" {
			return "/work/api"
		}
		return ""
	}
	snap, err = factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(dirs)).Load()
	if err != nil || len(snap.Checkouts) != 1 || snap.Checkouts["acme/api"] != "/work/api" {
		t.Fatalf("checkouts = %v (%v), want only acme/api", snap.Checkouts, err)
	}
}

// A DROPPED JOB REFUSES EVERY MANAGER DOOR WITH ITS WAY BACK: `factory_run`'s
// edit door changes none of its steps, and the Talk door gives it no new
// agent, because the drop deleted its conversations and nothing of it runs.
// Each refusal is the start's own sentence, and the job is left as it was.
func TestADroppedJobRefusesTheEditAndTalkDoors(t *testing.T) {
	_, st := openLocal(t)
	recipe := factory.DefaultRecipe()
	it, err := st.Create(factory.Item{Num: 12, Repo: "acme/api", Title: "dropped", Kind: factory.KindIssue,
		State: factory.StateDismissed, DoneHow: "dropped", Stages: factory.CopyStages(recipe.For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	made := false
	seam := factory.LocalSeam(st, time.Now(), factory.WithTalk(func(context.Context, factory.Item) (string, error) {
		made = true
		return "/places/new/transcript.jsonl", nil
	}))
	want := "#12 " + factory.DroppedRefusal
	_, lines, err := seam.Edit(context.Background(), it.ID, factory.RunEdit{By: factory.ByManager, Skip: []string{"review"}, Why: "not needed"})
	var refusal factory.EditRefusal
	if err == nil || err.Error() != want || !errors.As(err, &refusal) || len(lines) != 0 {
		t.Fatalf("an edit of a dropped job = %v %v, want the refusal %q", lines, err, want)
	}
	if _, err := seam.Talk(context.Background(), it.ID); err == nil || err.Error() != want || made {
		t.Fatalf("the Talk door on a dropped job = %v (made %v), want the refusal %q", err, made, want)
	}
	cur, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Talk != "" || len(cur.Adapted) != 0 || !cur.Stages[factory.StageIndex(cur.Stages, "review")].On {
		t.Fatalf("a refused door changed the dropped job: talk %q adapted %v stages %+v", cur.Talk, cur.Adapted, cur.Stages)
	}
	// The same job brought back is edited as any other.
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.State, cur.DoneHow = factory.StateNew, ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, lines, err := seam.Edit(context.Background(), it.ID, factory.RunEdit{By: factory.ByManager, Skip: []string{"review"}}); err != nil || len(lines) == 0 {
		t.Fatalf("an edit of a job in Backlog = %v %v", lines, err)
	}
}
