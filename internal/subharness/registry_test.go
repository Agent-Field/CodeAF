package subharness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryStoresEntriesOnThePlainPath(t *testing.T) {
	root := t.TempDir()
	reg := New(root)
	e := hostedEntry()

	if err := reg.Save(e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(root, "triage.hjson")
	if reg.Path("triage") != path {
		t.Fatalf("Path is %q, want the plain %q", reg.Path("triage"), path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the entry is not on its own path: %v", err)
	}

	back, err := reg.Load("triage")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Name != e.Name || back.Revision != e.Revision || len(back.Nodes) != len(e.Nodes) {
		t.Fatalf("loaded %+v, want the entry that was saved", back)
	}
	if back.Nodes[0].Trigger == nil || back.Nodes[0].Trigger.Mode != TriggerHosted {
		t.Fatalf("the trigger did not survive the round trip: %+v", back.Nodes[0])
	}
	if strings.Join(back.Nodes[0].Trigger.Args, ",") != "since,label" {
		t.Errorf("allowed args came back as %v", back.Nodes[0].Trigger.Args)
	}

	names, err := reg.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 1 || names[0] != "triage" {
		t.Fatalf("Names is %v, want [triage]", names)
	}
}

func TestLoadingWhatIsNotThereIsNotFound(t *testing.T) {
	reg := New(t.TempDir())

	if _, err := reg.Load("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load of a missing harness: %v", err)
	}
	if _, err := reg.LoadRevision("absent", 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadRevision of a missing revision: %v", err)
	}
	names, err := reg.Names()
	if err != nil || len(names) != 0 {
		t.Fatalf("Names on an empty root: %v %v", names, err)
	}
}

func TestARevisionIsPinnedAndNeverRewritten(t *testing.T) {
	reg := New(t.TempDir())
	v1 := hostedEntry()
	if err := reg.Save(v1); err != nil {
		t.Fatalf("Save v1: %v", err)
	}

	// Saving the same revision again with the same content is idempotent.
	if err := reg.Save(v1); err != nil {
		t.Fatalf("re-saving identical content: %v", err)
	}

	changed := hostedEntry()
	changed.Desc = "sort the inbox, differently"
	if err := reg.Save(changed); err == nil {
		t.Fatal("revision 1 was rewritten in place")
	}

	v2 := changed
	v2.Revision = 2
	if err := reg.Save(v2); err != nil {
		t.Fatalf("Save v2: %v", err)
	}
	current, err := reg.Load("triage")
	if err != nil || current.Revision != 2 {
		t.Fatalf("current is revision %d (%v), want 2", current.Revision, err)
	}

	pinned, err := reg.LoadRevision("triage", 1)
	if err != nil {
		t.Fatalf("LoadRevision 1: %v", err)
	}
	if pinned.Revision != 1 || pinned.Desc != "sort the inbox" {
		t.Fatalf("the pin resolved to %+v, want revision 1 as it was written", pinned)
	}
	if unpinned, err := reg.LoadRevision("triage", 0); err != nil || unpinned.Revision != 2 {
		t.Fatalf("revision 0 resolved to %d (%v), want the current 2", unpinned.Revision, err)
	}

	revisions, err := reg.Revisions("triage")
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revisions) != 2 || revisions[0] != 1 || revisions[1] != 2 {
		t.Fatalf("Revisions is %v, want [1 2]", revisions)
	}

	behind := hostedEntry()
	behind.Desc = "older"
	behind.Revision = 1
	if err := reg.Save(behind); err == nil {
		t.Fatal("a revision behind the stored one became current")
	}
}

func TestSaveRefusesAnEntryThatWouldNotRun(t *testing.T) {
	reg := New(t.TempDir())
	e := hostedEntry()
	e.Nodes[0].Trigger.Entry = "nowhere"

	if err := reg.Save(e); err == nil {
		t.Fatal("Save stored a harness whose trigger enters nowhere")
	}
	if _, err := os.Stat(reg.Path("triage")); err == nil {
		t.Fatal("the refused entry was written anyway")
	}
}

func TestLoadRefusesAFileThatRenamedItself(t *testing.T) {
	root := t.TempDir()
	reg := New(root)
	if err := reg.Save(hostedEntry()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(reg.Path("triage"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.hjson"), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := reg.Load("other"); err == nil {
		t.Fatal("a file whose name field disagrees with its path loaded anyway")
	}
}

func TestHostAllMountsEveryRegisteredHarness(t *testing.T) {
	reg := New(t.TempDir())
	first := hostedEntry()
	second := hostedEntry()
	second.Name = "sweep"
	second.Desc = "clear the queue"
	for _, e := range []Entry{first, second} {
		if err := reg.Save(e); err != nil {
			t.Fatalf("Save %s: %v", e.Name, err)
		}
	}

	src := newFakeSource()
	mounted, err := reg.HostAll(src)
	if err != nil {
		t.Fatalf("HostAll: %v", err)
	}
	if len(mounted) != 2 {
		t.Fatalf("mounted %d commands, want 2", len(mounted))
	}
	for _, name := range []string{"triage", "sweep"} {
		if _, ok := src.mounted[CommandFor(name)]; !ok {
			t.Errorf("source has %v, missing %q", keysOf(src.mounted), CommandFor(name))
		}
	}
}

func TestRunsAreSavedUnderTheHarnessRunDirectory(t *testing.T) {
	root := t.TempDir()
	reg := New(root)
	if err := reg.Save(hostedEntry()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	started := time.Date(2026, 8, 16, 9, 30, 15, 0, time.UTC)
	run := Run{
		Harness: "triage", Revision: 1,
		Started: started, Ended: started.Add(time.Minute),
		Trigger: "start", Command: CommandFor("triage"), Args: []string{"since=yesterday"},
		Steps: []Step{
			{ID: "read-it", Kind: KindAgentLoop, Started: started, Ended: started.Add(time.Minute), Status: "done"},
		},
		Verdict: VerdictPass,
	}
	path, err := reg.SaveRun(run)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	want := filepath.Join(root, "triage", "run", "20260816T093015Z.json")
	if path != want {
		t.Fatalf("run saved at %q, want %q", path, want)
	}

	// A second run in the same second gets its own file rather than the first's.
	second, err := reg.SaveRun(run)
	if err != nil {
		t.Fatalf("SaveRun again: %v", err)
	}
	if second == path {
		t.Fatal("the second run overwrote the first")
	}

	paths, err := reg.Runs("triage")
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if len(paths) != 2 || paths[0] != path {
		t.Fatalf("Runs is %v, want the two saved runs oldest first", paths)
	}

	back, err := reg.LoadRun(path)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if back.Revision != 1 || back.Verdict != VerdictPass || back.Trigger != "start" {
		t.Fatalf("loaded %+v, want the run that was saved", back)
	}
	if len(back.Steps) != 1 || back.Steps[0].Kind != KindAgentLoop {
		t.Fatalf("steps came back as %+v", back.Steps)
	}
}

func TestARunMustNameItsRevision(t *testing.T) {
	reg := New(t.TempDir())
	started := time.Date(2026, 8, 16, 9, 30, 15, 0, time.UTC)

	if _, err := reg.SaveRun(Run{Harness: "triage", Started: started}); err == nil {
		t.Fatal("a run saved without the revision it ran")
	}
	if _, err := reg.SaveRun(Run{Harness: "triage", Revision: 1}); err == nil {
		t.Fatal("a run saved without a start time")
	}
	if _, err := reg.SaveRun(Run{Harness: "../escape", Revision: 1, Started: started}); err == nil {
		t.Fatal("a run escaped the registry root")
	}
	if names, err := reg.Runs("triage"); err != nil || len(names) != 0 {
		t.Fatalf("Runs after refusals is %v (%v), want none", names, err)
	}
}

func TestNamesStayInsideTheRegistry(t *testing.T) {
	reg := New(t.TempDir())
	for _, name := range []string{"../escape", "with space", "Upper", "sub/dir", ""} {
		if err := ValidName(name); err == nil {
			t.Errorf("ValidName accepted %q", name)
		}
		if _, err := reg.Load(name); err == nil {
			t.Errorf("Load accepted %q", name)
		}
	}
}

func TestDefaultRootIsUnderTheStateRoot(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())

	root := New("").Root()
	if filepath.Base(root) != "harnesses" || !strings.HasPrefix(root, os.Getenv("AFORGE_HOME")) {
		t.Fatalf("default root is %q, want harnesses under the state root", root)
	}
}
