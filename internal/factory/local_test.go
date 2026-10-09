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
// tell the direct doors from the mailbox's.
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

// TestTheRunnerWinsOverTheMailbox: a seam handed both keeps the direct doors,
// and a verb this build does not know is refused in words by Carry.
func TestTheRunnerWinsOverTheMailbox(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now(), factory.WithMailbox(st.Mailbox()), factory.WithRunner(directDoors{}))
	if err := seam.Stop(1); !errors.Is(err, errDirect) {
		t.Fatalf("stop went %v, not through the runner", err)
	}
	if reply := factory.Carry(directDoors{}, factory.Ask{Verb: "dance", Seq: 9}); reply.Seq != 9 || reply.Err != `the floor's runner does not know "dance"` {
		t.Fatalf("an unknown verb = %+v", reply)
	}
	if bare := factory.LocalSeam(st, time.Now()); bare.Has("launch") || bare.Has("stop") {
		t.Fatal("a seam with no runner and no mailbox draws runner doors")
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
