package factory_test

import (
	"context"
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
	if snap.Benches != 0 || snap.Speed != 0 {
		t.Fatalf("a real floor drew benches %d, speed %v", snap.Benches, snap.Speed)
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
	if it.Gate != factory.GatePlan || it.Cap != 8 {
		t.Fatalf("gate %q cap %v; want plan, 8", it.Gate, it.Cap)
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
	must(seam.SetGate(keep, factory.GateNone))
	must(seam.SetCap(keep, 12.5))
	must(seam.SetStage(keep, 0, false))
	must(seam.AddStage(keep, "after review, make it neater"))
	must(seam.SetEffort(keep, 1, "strong"))
	must(seam.Dismiss(id))
	if seam.SetGate(keep, "sideways") == nil || seam.SetCap(keep, -1) == nil || seam.SetStage(keep, 99, true) == nil ||
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
	if it.Gate != factory.GateNone || it.Cap != 12.5 || it.Stages[0].On || it.Stages[1].Effort != "strong" {
		t.Fatalf("chips did not persist: %+v", it)
	}
	at := factory.StageIndex(it.Stages, "make it")
	if at <= factory.StageIndex(it.Stages, "review") || at+1 != factory.StageIndex(it.Stages, "proof") || it.Stages[at].Kind != factory.StageChat || it.Stages[at].Ask != "make it neater" || !it.Stages[at].On {
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
	for _, door := range []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sendback", "reverify", "bank", "sync", "askauthor", "bankstages", "tick", "sleep"} {
		if seam.Has(door) {
			t.Errorf("door %s is present on the local seam", door)
		}
	}
	for _, door := range []string{"load", "new", "dismiss", "setgate", "setcap", "setstage", "addstage", "seteffort"} {
		if !seam.Has(door) {
			t.Errorf("door %s is missing from the local seam", door)
		}
	}
}
