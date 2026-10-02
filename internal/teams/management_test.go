package teams

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func managementFixture() *File {
	return &File{Version: Version, Teams: []Team{
		{ID: "parent", Name: "Interface cleanup"},
		{ID: "child-a", Name: "Layout", Parent: "parent"},
		{ID: "child-b", Name: "Interactions", Parent: "parent"},
		{ID: "grandchild", Name: "Tables", Parent: "child-a"},
		{ID: "other", Name: "Release notes"},
		{ID: "root", Name: RootName, Root: true},
	}}
}

func TestManagerResponsibilityHasOneAnchorAndAllowsDescendants(t *testing.T) {
	f := managementFixture()
	for _, id := range []string{"parent", "child-a", "child-b", "grandchild"} {
		must(t, f.SetManager(id, "manager"))
	}
	if err := f.SetManager("other", "manager"); err == nil || !strings.Contains(err.Error(), "Release notes") {
		t.Fatalf("unrelated appointment accepted: %v", err)
	}
	other, _ := f.Team("other")
	if other.Manager != "" || other.Holds("manager") {
		t.Fatal("refusal added membership or changed manager")
	}
	must(t, f.AddMember("other", Member{Key: "manager"}))
	if err := f.SetManager("parent", "replacement"); err == nil {
		t.Fatal("removing the anchor left the old manager managing sibling teams")
	}
	p, _ := f.Team("parent")
	if p.Manager != "manager" || p.Holds("replacement") {
		t.Fatal("failed replacement changed state")
	}
	g := managementFixture()
	must(t, g.SetManager("child-a", "worker"))
	if err := g.SetManager("child-b", "worker"); err == nil {
		t.Fatal("siblings without their own managed anchor accepted")
	}
}

func TestGlobalManagerCannotAlsoManageAnOrdinaryTeam(t *testing.T) {
	for _, order := range [][]string{{"parent", "root"}, {"root", "parent"}} {
		f := managementFixture()
		must(t, f.SetManager(order[0], "manager"))
		if err := f.SetManager(order[1], "manager"); err == nil {
			t.Fatal("global manager reused for ordinary team")
		}
	}
}

func TestTeamMoveCannotSplitManagerResponsibilities(t *testing.T) {
	f := managementFixture()
	must(t, f.SetManager("parent", "manager"))
	must(t, f.SetManager("child-a", "manager"))
	if err := f.SetParent("child-a", "other"); err == nil {
		t.Fatal("move split the manager across independent branches")
	}
	child, _ := f.Team("child-a")
	if child.Parent != "parent" {
		t.Fatal("refused move mutated hierarchy")
	}
	must(t, f.SetParent("parent", "other"))
	must(t, f.SetParent("grandchild", "child-b"))
}

func TestLegacyManagerConflictsRemainAndCanBeRepairedExplicitly(t *testing.T) {
	dir := t.TempDir()
	f := managementFixture()
	for i := range f.Teams {
		if f.Teams[i].ID == "parent" || f.Teams[i].ID == "other" {
			f.Teams[i].Manager = "manager"
			f.Teams[i].Members = []Member{{Key: "manager"}}
		}
	}
	raw, err := json.Marshal(disk{Version: Version, Teams: f.Teams})
	must(t, err)
	must(t, os.WriteFile(Path(dir), raw, 0600))
	loaded, err := Load(dir)
	must(t, err)
	if len(loaded.ManagedTeams("manager")) != 2 {
		t.Fatal("load silently repaired legacy leadership")
	}
	must(t, Update(dir, func(f *File) error { return f.AddMember("child-a", Member{Key: "member"}) }))
	// Even direct field writes through the store cannot introduce a new conflict.
	if err := Update(dir, func(f *File) error {
		for i := range f.Teams {
			if f.Teams[i].ID == "child-b" {
				f.Teams[i].Manager = "manager"
				f.Teams[i].Members = append(f.Teams[i].Members, Member{Key: "manager"})
			}
		}
		return nil
	}); err == nil {
		t.Fatal("direct store write bypassed policy")
	}
	must(t, Update(dir, func(f *File) error { return f.SetManager("other", "replacement") }))
	loaded, err = Load(dir)
	must(t, err)
	if len(loaded.ManagedTeams("manager")) != 1 {
		t.Fatal("explicit repair did not persist")
	}
}

func TestManagerAliasesCannotBypassTheRestriction(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	alias := filepath.Join(dir, "alias.jsonl")
	must(t, os.WriteFile(file, nil, 0600))
	must(t, os.Symlink(file, alias))
	f := managementFixture()
	f.localIdentities = true
	must(t, f.SetManager("parent", file))
	if err := f.SetManager("other", alias); err == nil {
		t.Fatal("symlink appointed the same conversation to another team")
	}
	if len(f.ManagedTeams(alias)) != 1 {
		t.Fatal("deletion failed to identify aliased manager")
	}
}

func TestManagerRemovalNamesEveryActiveTeamAndPreservesHistory(t *testing.T) {
	f := managementFixture()
	must(t, f.SetManager("parent", "manager"))
	must(t, f.SetManager("child-a", "manager"))
	message := f.ManagerRemovalMessage("manager")
	if !strings.Contains(message, "Interface cleanup") || !strings.Contains(message, "Layout") || !strings.Contains(message, "in Teams") {
		t.Fatal(message)
	}
	must(t, f.Disband("child-a", time.Now(), ""))
	if strings.Contains(f.ManagerRemovalMessage("manager"), "Layout") {
		t.Fatal("closed team prevented deletion")
	}
	must(t, f.SetManager("other", "replacement"))
}

func TestHostedManagerPathsDoNotResolveOnTheWindowDisk(t *testing.T) {
	dir := t.TempDir()
	file, alias := filepath.Join(dir, "transcript.jsonl"), filepath.Join(dir, "alias.jsonl")
	must(t, os.WriteFile(file, nil, 0600))
	must(t, os.Symlink(file, alias))
	f := managementFixture()
	must(t, f.SetManager("parent", file))
	must(t, f.SetManager("other", alias))
	if len(f.ManagedTeams(alias)) != 1 {
		t.Fatal("hosted identity read the window's symlink")
	}
}

func TestInspectDoesNotRunCleanupWithoutALock(t *testing.T) {
	dir := t.TempDir()
	must(t, os.Mkdir(lockPath(dir), 0700))
	called := false
	err := Inspect(dir, func(*File) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("cleanup ran without a lock: called=%v err=%v", called, err)
	}
}

func TestClearManagerCannotStrandIndependentManagedBranches(t *testing.T) {
	f := managementFixture()
	for _, id := range []string{"parent", "child-a", "child-b"} {
		must(t, f.SetManager(id, "manager"))
	}
	if err := f.ClearManager("parent"); err == nil {
		t.Fatal("clearing the anchor bypassed the responsibility rule")
	}
	p, _ := f.Team("parent")
	if p.Manager != "manager" {
		t.Fatal("failed clearing changed leadership")
	}
}

func TestMoveEffectsKeepsLocalAliasIdentity(t *testing.T) {
	dir := t.TempDir()
	file, alias := filepath.Join(dir, "transcript.jsonl"), filepath.Join(dir, "alias.jsonl")
	must(t, os.WriteFile(file, nil, 0600))
	must(t, os.Symlink(file, alias))
	f := managementFixture()
	f.UseLocalIdentities()
	must(t, f.SetManager("parent", alias))
	must(t, f.SetManager("child-a", file))
	must(t, f.SetManager("child-b", file))
	copy := f.tidyCopy()
	must(t, copy.SetParent("child-a", "child-b"))
}
