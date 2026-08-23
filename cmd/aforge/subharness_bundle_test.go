package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/jsrun"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/substore"
)

// The doors this file pins are the two seams between a bundle on disk and a run
// that actually happens: the adapter that turns one into the other, and the note
// a finished run leaves behind for the next list to draw.
//
// EVERY TEST HERE USES [substore.At] OVER A TEMPORARY DIRECTORY and never
// [substore.Home]. A test that wrote into ~/.aforge would be a test that changes
// what the person's own `/subharness` list says.

// toyBundle mints one small, valid subharness into a store and hands back the
// store and the name.
func toyBundle(t *testing.T, program string) (*substore.Store, string) {
	t.Helper()
	store := substore.At(t.TempDir())
	manifest := exec.Manifest{
		SubharnessInfo: exec.SubharnessInfo{
			Name:    "weekly-brief",
			Purpose: "turn the week's notes into a brief",
		},
	}
	if _, err := store.Mint(substore.Files{
		Manifest: manifest,
		Program:  []byte(program),
		Prompts:  map[string][]byte{"summarise.md": []byte("Summarise the notes.")},
	}, "", ""); err != nil {
		t.Fatalf("minting the bundle: %v", err)
	}
	return store, manifest.Name
}

// THE PIN FOR THE ADAPTER: what comes back out of the store is a runner, and it
// describes itself with the manifest the bundle was written with.
//
// It also pins the one translation the adapter makes. The store keys a prompt by
// its FILE NAME and the runtime keys it by the NAME AN ai() CALL WRITES, and a
// build that handed the file names straight over would put every asset one
// suffix away from every lookup.
func TestABundleFromTheStoreBecomesARunnerThatKeepsItsManifest(t *testing.T) {
	store, name := toyBundle(t, "function run(input) { return { ok: true }; }\n")
	bundle, err := store.Load(name, 0)
	if err != nil {
		t.Fatalf("loading the bundle: %v", err)
	}

	runner, err := subharnessBuild(bundleLook{})(bundle)
	if err != nil {
		t.Fatalf("building a runner out of it: %v", err)
	}
	manifest := runner.Manifest()
	if manifest.Name != bundle.Manifest.Name || manifest.Purpose != bundle.Manifest.Purpose {
		t.Fatalf("the runner describes itself as %+v, and the bundle says %+v", manifest, bundle.Manifest)
	}

	js, ok := runner.(*jsrun.Runner)
	if !ok {
		t.Fatalf("a bundle should have become a JavaScript runner, and it became %T", runner)
	}
	if _, present := js.Prompts()["summarise"]; !present {
		t.Fatalf("prompts/summarise.md should reach the runner as %q; it carries %v", "summarise", js.Prompts())
	}
}

// A guard cannot be checked before there is anything to check it against, and
// jsrun states what that has to mean: it does not pass. This pins both halves —
// false while nobody is watching the belt, and the real answer the moment
// somebody is.
func TestAToolGuardSeesNothingUntilTheBeltIsAttached(t *testing.T) {
	belt := &beltWatch{}
	look := bundleLook{workspace: t.TempDir(), belt: belt.on}

	if look.ToolOnBelt("read") {
		t.Fatal("a tool guard answered yes with no belt attached — the safe answer is the long way")
	}
	belt.watch(func(name string) bool { return name == "read" })
	if !look.ToolOnBelt("read") {
		t.Fatal("the belt is attached and says it has read, and the guard still cannot see it")
	}
	if look.ToolOnBelt("write") {
		t.Fatal("the guard answered yes for a tool the belt does not have")
	}
}

// A file guard asks about the PERSON'S directory, not the bundle's, so a
// relative path is resolved against the workspace and nothing else.
func TestAFileGuardLooksInTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.md"), []byte("the week"), 0o644); err != nil {
		t.Fatalf("writing the file the guard is about: %v", err)
	}
	look := bundleLook{workspace: workspace}
	if !look.FileExists("notes.md") {
		t.Fatal("a file that is there was reported missing")
	}
	if look.FileExists("nothing.md") {
		t.Fatal("a file that is not there was reported present")
	}
	if (bundleLook{}).FileExists("notes.md") {
		t.Fatal("with nowhere to look, the honest answer is no")
	}
}

// THE EMPTINESS LAW ON THE ROW: a subharness nobody has run says nothing under
// its name — never "never run", never "0 runs" — and one that has run says when,
// how it went, and what it cost.
func TestARowSaysNothingWithNoHistoryAndOneLineWithIt(t *testing.T) {
	store := substore.At(t.TempDir())
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	last := subharnessLastRun(store, nil, func() time.Time { return now })

	if line := last("weekly-brief"); line != "" {
		t.Fatalf("a subharness nobody has run should draw nothing, and it drew %q", line)
	}

	finished := subharness.LastRunLine(subharness.LastRun{
		At: now.Add(-26 * time.Hour), Finished: true, CostUSD: 0.1234,
	}, now)
	if finished != "yesterday · finished · $0.12" {
		t.Fatalf("a finished run reads %q", finished)
	}

	// A run that did not finish is INCOMPLETE and is never called a failure, and
	// a cost nobody reported is not drawn at all.
	incomplete := subharness.LastRunLine(subharness.LastRun{
		At: now.Add(-90 * time.Minute),
	}, now)
	if incomplete != "1h · incomplete" {
		t.Fatalf("an unfinished run reads %q", incomplete)
	}
	if strings.Contains(incomplete, "fail") || strings.Contains(incomplete, "$") {
		t.Fatalf("the line broke the vocabulary or the emptiness law: %q", incomplete)
	}
}

// ONE PROGRAM, ONE RECORD, WHICHEVER DOOR RAN IT.
//
// A page run started from `/harness` saves a trace beside the page and leaves no
// home-store note; a run started from `/subharness` leaves the note. The row
// under a name has to say the same thing either way, so the reading asks both
// stores and answers with whichever is newer ([subharnessLastRun] states the
// whole design and why it is a read rather than a second write).
func TestARunFromEitherDoorReachesTheSameRow(t *testing.T) {
	home := substore.At(t.TempDir())
	pages := subharness.At(t.TempDir())
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	last := subharnessLastRun(home, pages, func() time.Time { return now })

	page := subharness.Harness{
		Id: subharness.Id{Name: "triage-flake", Desc: "chase a flaky test"},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "look", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "look"}},
		}},
	}
	if _, err := pages.Save(page); err != nil {
		t.Fatalf("saving the page: %v", err)
	}
	if line := last("triage-flake"); line != "" {
		t.Fatalf("a page nobody has run should draw nothing, and it drew %q", line)
	}

	// THE /harness ROAD: a trace beside the page and no note anywhere else. The
	// row used to stay silent about it.
	if _, err := pages.SaveRun(subharness.Trace{
		Id: page.Id, Started: now.Add(-3 * time.Hour), Status: subharness.StatusOK,
	}); err != nil {
		t.Fatalf("saving the trace: %v", err)
	}
	if line := last("triage-flake"); line != "3h · finished" {
		t.Fatalf("a run from the /harness road reads %q on the /subharness row", line)
	}

	// THE /subharness ROAD: the note is newer, and it carries the cost the trace
	// never had.
	if err := home.RecordRun("triage-flake", substore.RunNote{
		At: now.Add(-10 * time.Minute), Finished: true, CostUSD: 0.42,
	}); err != nil {
		t.Fatalf("recording the note: %v", err)
	}
	if line := last("triage-flake"); line != "10m · finished · $0.42" {
		t.Fatalf("the newer of the two readings did not win: %q", line)
	}

	// And an older note does not shout down a newer trace.
	if _, err := pages.SaveRun(subharness.Trace{
		Id: page.Id, Started: now.Add(-time.Minute), Status: subharness.StatusCancelled,
	}); err != nil {
		t.Fatalf("saving the second trace: %v", err)
	}
	if line := last("triage-flake"); line != "1m · incomplete" {
		t.Fatalf("the newest run is not what the row says: %q", line)
	}
}

// THE PIN FOR THE WRITE HALF: a run recorded through the seam both surfaces use
// is the run the next list reads back, with the version that actually ran on it.
func TestARecordedRunIsWhatTheNextListReadsBack(t *testing.T) {
	store, name := toyBundle(t, "function run(input) { return {}; }\n")
	record := subharnessRunRecorder(store)
	if record == nil {
		t.Fatal("a store that is there should be recordable into")
	}
	at := time.Date(2026, 8, 22, 8, 30, 0, 0, time.UTC)
	record(name, substore.RunNote{At: at, Finished: true, CostUSD: 0.5})

	note, ok := store.LastRun(name)
	if !ok {
		t.Fatal("the run was recorded and the store says the subharness has never run")
	}
	if !note.Finished || note.CostUSD != 0.5 || !note.At.Equal(at) {
		t.Fatalf("what was read back is %+v", note)
	}
	// The version is stamped by the recorder rather than by its caller, so a
	// row can say which program the note is actually about.
	if note.Version != 1 {
		t.Fatalf("the note should name the version that ran; it names v%d", note.Version)
	}

	line := subharnessLastRun(store, nil, func() time.Time { return at.Add(5 * time.Second) })(name)
	if line != "now · finished · $0.50" {
		t.Fatalf("the row under the name reads %q", line)
	}

	// A name with no bundle at all is still recordable — a Go-native program has
	// no directory here until its first run, and its row wants the same note.
	record("linear-ish", substore.RunNote{At: at, Finished: false, Why: "it needed a closer look"})
	if _, ok := store.LastRun("linear-ish"); !ok {
		t.Fatal("a subharness with no bundle on disk left no note")
	}
}
