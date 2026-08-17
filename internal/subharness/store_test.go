package subharness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A version is a pointer, and the point of a pointer is that it keeps pointing
// at the same thing. v1 has to read back the page it read before v2 existed —
// otherwise a subharness.call pinned to a version is pinned to nothing.
func TestAVersionPointerKeepsReadingItsOwnPage(t *testing.T) {
	store := At(t.TempDir())

	first, err := store.Save(linear())
	if err != nil {
		t.Fatal(err)
	}
	if first.Id.Version != 1 {
		t.Fatalf("first save minted v%d", first.Id.Version)
	}

	// v2 is a different program under the same name.
	revised := linear()
	revised.Id.Version = 0
	revised.Id.Desc = "one loop, checked twice"
	revised.Program.Nodes = append(revised.Program.Nodes,
		Node{Id: "recheck", Kind: KindVerify, Fields: Fields{"check": "and again"}})
	revised.Program.Edges = append(revised.Program.Edges, Edge{"check", "recheck"})
	second, err := store.Save(revised)
	if err != nil {
		t.Fatal(err)
	}
	if second.Id.Version != 2 {
		t.Fatalf("second save minted v%d", second.Id.Version)
	}

	// The store is reopened, because a pointer that only holds inside one
	// process is not a pointer.
	reopened := At(store.Dir())
	pinned, err := reopened.Load("linear", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned.Program.Nodes) != 3 || pinned.Id.Desc != "one loop, checked" || pinned.Id.Version != 1 {
		t.Fatalf("v1 read back as %+v", pinned.Id)
	}
	head, err := reopened.Load("linear", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Program.Nodes) != 4 || head.Id.Version != 2 {
		t.Fatalf("the head read back as v%d with %d nodes", head.Id.Version, len(head.Program.Nodes))
	}
	if at, err := reopened.Head("linear"); err != nil || at != 2 {
		t.Fatalf("head is v%d (%v)", at, err)
	}
	versions, err := reopened.Versions("linear")
	if err != nil || len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("versions are %v (%v)", versions, err)
	}
}

// The pages are the interface. `ls` in a harness's directory has to read as a
// version list without a tool.
func TestAPageLandsWhereItsNameSaysItDoes(t *testing.T) {
	dir := t.TempDir()
	store := At(dir)
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "linear", "v1.json")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("a page does not end in a newline")
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("the page on disk does not decode: %v", err)
	}
}

// A distiller reads v2, thinks, and comes back to save. If somebody saved v3
// in the meantime, the save it meant to make is not the save it would be
// making, and it has to find out.
func TestSavingAVersionSomebodyElseAlreadyTookIsRefused(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	stale := linear()
	stale.Id.Version = 1
	_, err := store.Save(stale)
	if err == nil || !strings.Contains(err.Error(), "the next version is v2") {
		t.Fatalf("a stale save was allowed: %v", err)
	}
	// The version it did mean is accepted.
	fresh := linear()
	fresh.Id.Version = 2
	if _, err := store.Save(fresh); err != nil {
		t.Fatal(err)
	}
}

// An invalid harness never reaches the disk, so every page a reader finds is
// one the runner can run.
func TestAnInvalidHarnessNeverLands(t *testing.T) {
	dir := t.TempDir()
	store := At(dir)
	broken := linear()
	broken.Program.Edges = append(broken.Program.Edges, Edge{"check", "work"})
	if _, err := store.Save(broken); err == nil {
		t.Fatal("a cyclic program was saved")
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "linear")); err == nil && len(entries) > 0 {
		t.Fatalf("a refused save left %d files behind", len(entries))
	}
	if names, err := store.Names(); err != nil || len(names) != 0 {
		t.Fatalf("a refused save is listed: %v (%v)", names, err)
	}
}

func TestReadingAHarnessThatWasNeverSaved(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Load("absent", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unsaved harness read as %v", err)
	}
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("linear", 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unsaved version read as %v", err)
	}
	// Listing a registry that has never been written is a question, not a
	// mutation: no error, and no directory either.
	empty := At(filepath.Join(t.TempDir(), "never"))
	names, err := empty.Names()
	if err != nil || names != nil {
		t.Fatalf("an empty registry listed as %v (%v)", names, err)
	}
	if _, err := os.Stat(empty.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("listing an empty registry created it")
	}
}

// A page carries its own version and so does its filename. They can only
// disagree if somebody moved a file, and a harness that lies about its own
// pointer would be recorded as that lie in every trace it runs.
func TestAPageThatDisagreesWithItsFilenameIsRefused(t *testing.T) {
	dir := t.TempDir()
	store := At(dir)
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "linear", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "linear", "v4.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("linear", 4); err == nil || !strings.Contains(err.Error(), "the page says v1") {
		t.Fatalf("a moved page loaded as v4: %v", err)
	}
}

func TestNamesListsEveryHarnessThatHasAPage(t *testing.T) {
	dir := t.TempDir()
	store := At(dir)
	for _, name := range []string{"reviewer", "linear"} {
		h := linear()
		h.Id.Name = name
		h.Id.Version = 0
		if _, err := store.Save(h); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with no pages in it is not a harness, and neither is a file
	// somebody left in the registry.
	if err := os.MkdirAll(filepath.Join(dir, "half-written"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := store.Names()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "linear,reviewer" {
		t.Fatalf("names are %v", names)
	}
}

// A run's trace is the evidence the dynamism ladder is honest about, so it is
// kept whole, under the harness it came from, and named so that filename order
// is time order.
func TestRunsAreSavedUnderTheHarnessAndReadBackWhole(t *testing.T) {
	dir := t.TempDir()
	store := At(dir)
	at := time.Date(2026, 8, 16, 10, 11, 12, 0, time.UTC)
	store.Now = func() time.Time { return at }

	saved, err := store.Save(linear())
	if err != nil {
		t.Fatal(err)
	}
	trace := Trace{
		Id:      saved.Id,
		Started: at,
		Elapsed: 3 * time.Second,
		Trail:   []Trail{{Step: 1, Id: "start", Kind: KindTrigger, Out: "idle", Elapsed: time.Second}},
		Edges:   []Edge{{"start", "work"}},
	}
	first, err := store.SaveRun(trace)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "linear", RunDir, "20260816T101112Z.json"); first != want {
		t.Fatalf("run landed at %s, want %s", first, want)
	}
	// Two runs in one second is ordinary. The second earns a suffix rather
	// than overwriting the first.
	second, err := store.SaveRun(trace)
	if err != nil {
		t.Fatal(err)
	}
	if second == first || !strings.Contains(second, "20260816T101112Z_01.json") {
		t.Fatalf("a second run in the same second landed at %s", second)
	}

	runs, err := store.Runs("linear")
	if err != nil || len(runs) != 2 || runs[0] != first {
		t.Fatalf("runs are %v (%v)", runs, err)
	}
	back, err := store.LoadRun(first)
	if err != nil {
		t.Fatal(err)
	}
	if back.Id != trace.Id || len(back.Trail) != 1 || back.Trail[0].Out != "idle" {
		t.Fatalf("the trace read back as %+v", back)
	}
	if back.Elapsed != 3*time.Second || back.Trail[0].Elapsed != time.Second {
		t.Fatalf("elapsed did not survive: %v %v", back.Elapsed, back.Trail[0].Elapsed)
	}
	if _, err := store.Runs("absent"); err != nil {
		t.Fatalf("listing runs of an unsaved harness errored: %v", err)
	}
}

// The registry lives under the one state root aforge owns, and moves wholesale
// with it.
func TestTheDefaultRegistryFollowsTheStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	if got, want := Default().Dir(), filepath.Join(root, Root); got != want {
		t.Fatalf("the default registry is at %s, want %s", got, want)
	}
}

func TestAHarnessNameIsHeldToTheSlugLawOnTheWayToDisk(t *testing.T) {
	store := At(t.TempDir())
	escaping := linear()
	escaping.Id.Name = "../elsewhere"
	if _, err := store.Save(escaping); err == nil {
		t.Fatal("a name that escapes its directory was saved")
	}
	if _, err := store.Load("../elsewhere", 1); err == nil {
		t.Fatal("a name that escapes its directory was read")
	}
	if _, err := store.SaveRun(Trace{Id: Id{Name: "../elsewhere"}}); err == nil {
		t.Fatal("a run under a name that escapes its directory was saved")
	}
}
