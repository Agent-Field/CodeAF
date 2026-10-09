package placegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// ---- shared helpers --------------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now advances a second per call so every stamp in a test is distinct and the
// run never reads the real clock.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "places.json")
	clk := &fakeClock{t: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	var n int
	var mu sync.Mutex
	s, err := Open(Options{Path: path, Now: clk.Now, NewID: func(prefix string) string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("%s%d", prefix, n)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func mk(t *testing.T, s *Store, name string, parents ...string) Place {
	t.Helper()
	p, _, err := s.CreatePlace(NewPlace{Name: name, Parents: parents})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return p
}

func snap(t *testing.T, s *Store) *Snapshot {
	t.Helper()
	sn, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return sn
}

// ok adapts a (Receipt, error) call: ok(t)(s.Rename(...)) fails the test on error.
func ok(t *testing.T) func(Receipt, error) Receipt {
	return func(rc Receipt, err error) Receipt {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}
}

func ids(ps []Place) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func eq(a, b []string) bool { return equalStrings(append([]string{}, a...), append([]string{}, b...)) }

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ---- cross-process ---------------------------------------------------------

// TestMain doubles as the helper process for the cross-process tests: the test
// binary re-executes itself with PLACEGRAPH_HELPER set, so the lock is exercised by
// REAL separate processes and not by goroutines sharing one mutex.
func TestMain(m *testing.M) {
	if mode := os.Getenv("PLACEGRAPH_HELPER"); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	s, err := Open(Options{Path: os.Getenv("PLACEGRAPH_PATH"), LockTimeout: 60 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		return 2
	}
	switch mode {
	case "file":
		tag := os.Getenv("PLACEGRAPH_TAG")
		for i := 0; i < 25; i++ {
			if _, _, err := s.AddChat(fmt.Sprintf("chat-%s-%d", tag, i), os.Getenv("PLACEGRAPH_PLACE"), AddedByYou); err != nil {
				fmt.Fprintln(os.Stderr, "add:", err)
				return 3
			}
		}
	case "claim":
		// Check-then-act: only one process may win the name.
		_, _, err := s.CreatePlace(NewPlace{Name: "Shared"})
		switch {
		case err == nil:
			fmt.Println("won")
		case errors.Is(err, ErrNameTaken):
			fmt.Println("lost")
		default:
			fmt.Fprintln(os.Stderr, "claim:", err)
			return 4
		}
	}
	return 0
}

func runHelpers(t *testing.T, mode, path, place string, n int) []string {
	t.Helper()
	type res struct {
		out string
		err error
	}
	results := make([]res, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "PLACEGRAPH_HELPER="+mode, "PLACEGRAPH_PATH="+path,
				"PLACEGRAPH_PLACE="+place, fmt.Sprintf("PLACEGRAPH_TAG=p%d", i))
			out, err := cmd.CombinedOutput()
			results[i] = res{strings.TrimSpace(string(out)), err}
		}()
	}
	wg.Wait()
	var outs []string
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("helper %d failed: %v\n%s", i, r.err, r.out)
		}
		outs = append(outs, r.out)
	}
	return outs
}

func TestCrossProcessWritersLoseNoUpdates(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Inbox work")
	const procs = 4
	runHelpers(t, "file", path, p.ID, procs)

	sn := snap(t, s)
	if got := len(sn.Memberships); got != procs*25 {
		t.Fatalf("memberships = %d, want %d (an update was lost)", got, procs*25)
	}
	// One create plus one commit per membership, each bumping the revision once.
	if sn.Revision != 1+procs*25 {
		t.Fatalf("revision = %d, want %d", sn.Revision, 1+procs*25)
	}
	if _, err := validateState(&sn.State, false); err != nil {
		t.Fatalf("file invalid after contention: %v", err)
	}
}

func TestCrossProcessCheckThenActIsSerialized(t *testing.T) {
	_, path := newStore(t)
	outs := runHelpers(t, "claim", path, "", 6)
	won := 0
	for _, o := range outs {
		if o == "won" {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d processes won the same name; outputs %v", won, outs)
	}
}

func TestLockTimeoutReportsErrLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places.json")
	s, err := Open(Options{Path: path, LockTimeout: 60 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path+".lock", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := filelock.Lock(f, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	if err := filelock.Unlock(f); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

// ---- atomicity -------------------------------------------------------------

func TestFailureBeforeRenameLeavesThePreviousDocument(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "Keep me")
	before := readFile(t, path)

	s.beforeRename = func() error { return errors.New("power cut") }
	if _, _, err := s.CreatePlace(NewPlace{Name: "Never lands"}); err == nil {
		t.Fatal("create succeeded through a failed write")
	}
	s.beforeRename = nil

	if after := readFile(t, path); string(after) != string(before) {
		t.Fatal("a failed write changed the document")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file %s left behind", e.Name())
		}
	}
	if names := placeNames(snap(t, s)); !eq(names, []string{"Keep me"}) {
		t.Fatalf("places = %v", names)
	}
	// And the failed attempt left no undo receipt for a change that never happened.
	if n := len(s.Receipts()); n != 1 {
		t.Fatalf("receipts = %d, want 1", n)
	}
}

func TestStaleTempFileFromACrashIsIgnored(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "A")
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".places-crash.tmp"), []byte(`{"version":1,"places":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	if names := placeNames(snap(t, s)); !eq(names, []string{"A"}) {
		t.Fatalf("places = %v", names)
	}
	if s.LastRecovery() != nil {
		t.Fatal("a stray temp file triggered recovery")
	}
}

func placeNames(sn *Snapshot) []string {
	out := []string{}
	for _, p := range sn.Places {
		out = append(out, p.Name)
	}
	return out
}

// ---- corrupt input ---------------------------------------------------------

func openOver(t *testing.T, content []byte) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	return s, path, dir
}

func sidecars(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "places.json.corrupt-*"))
	return m
}

func TestCorruptInputIsQuarantinedNotDeleted(t *testing.T) {
	cases := map[string]string{
		"garbage":       "this is not json",
		"truncated":     `{"version":1,"revision":4,"places":[{"id":"pl_1","na`,
		"empty":         "",
		"trailing":      `{"version":1,"places":[],"memberships":[],"pinned":[]} {"x":1}`,
		"no version":    `{"places":[],"memberships":[],"pinned":[]}`,
		"duplicate ids": `{"version":1,"places":[{"id":"a","name":"A","parents":[]},{"id":"a","name":"B","parents":[]}]}`,
		"cycle":         `{"version":1,"places":[{"id":"a","name":"A","parents":["b"]},{"id":"b","name":"B","parents":["a"]}]}`,
		"self parent":   `{"version":1,"places":[{"id":"a","name":"A","parents":["a"]}]}`,
		"bad tint":      `{"version":1,"places":[{"id":"a","name":"A","parents":[],"tint":"puce"}]}`,
		"blank name":    `{"version":1,"places":[{"id":"a","name":"  ","parents":[]}]}`,
		"reserved id":   `{"version":1,"places":[{"id":"now","name":"N","parents":[]}]}`,
		"bad addedBy":   `{"version":1,"places":[{"id":"a","name":"A","parents":[]}],"memberships":[{"chatId":"c","placeId":"a","addedBy":"robot"}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			s, path, dir := openOver(t, []byte(content))
			rec := s.LastRecovery()
			if rec == nil || rec.Kind != "quarantined" || rec.Reason == "" {
				t.Fatalf("recovery = %+v", rec)
			}
			if got := readFile(t, rec.MovedTo); string(got) != content {
				t.Fatal("the damaged bytes were not preserved verbatim")
			}
			if len(sidecars(t, dir)) != 1 {
				t.Fatalf("sidecars = %v", sidecars(t, dir))
			}
			// The store is honest-empty and fully usable, and the live file is valid.
			sn := snap(t, s)
			if len(sn.Places) != 0 {
				t.Fatalf("recovered store has places: %v", placeNames(sn))
			}
			mk(t, s, "After recovery")
			var st State
			if err := json.Unmarshal(readFile(t, path), &st); err != nil {
				t.Fatalf("live file unreadable: %v", err)
			}
		})
	}
}

func TestDanglingReferencesAreRepairedAndListed(t *testing.T) {
	content := `{"version":1,"revision":7,"places":[
	  {"id":"a","name":"A","parents":["ghost","root"]},
	  {"id":"b","name":"B","parents":["a","a"]},
	  {"id":"z","name":"Z","parents":[],"archived":true}],
	 "memberships":[
	  {"chatId":"c1","placeId":"a","addedBy":"you"},
	  {"chatId":"c1","placeId":"a","addedBy":"ai"},
	  {"chatId":"c2","placeId":"gone","addedBy":"you"}],
	 "pinned":["a","a","gone","z"]}`
	s, _, dir := openOver(t, []byte(content))
	rec := s.LastRecovery()
	if rec == nil || rec.Kind != "repaired" || len(rec.Repairs) != 8 {
		t.Fatalf("recovery = %+v", rec)
	}
	if got := readFile(t, rec.MovedTo); string(got) != content {
		t.Fatal("original not preserved beside the repair")
	}
	if len(sidecars(t, dir)) != 1 {
		t.Fatal("expected exactly one sidecar")
	}
	sn := snap(t, s)
	a, _ := sn.Place("a")
	b, _ := sn.Place("b")
	if len(a.Parents) != 0 || !eq(b.Parents, []string{"a"}) {
		t.Fatalf("parents a=%v b=%v", a.Parents, b.Parents)
	}
	if len(sn.Memberships) != 1 || !eq(sn.Pinned, []string{"a"}) {
		t.Fatalf("memberships %v pinned %v", sn.Memberships, sn.Pinned)
	}
	if sn.Revision != 8 {
		t.Fatalf("revision = %d, want 8 (the repair is a commit)", sn.Revision)
	}
}

func TestNewerVersionIsRefusedAndNeverRewritten(t *testing.T) {
	content := `{"version":2,"revision":3,"places":[],"future":true}`
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Path: path}); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v", err)
	}
	if string(readFile(t, path)) != content || len(sidecars(t, dir)) != 0 {
		t.Fatal("a newer file was touched")
	}
}

func TestOversizeFileIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, MaxFileBytes+10); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if rec := s.LastRecovery(); rec == nil || rec.Kind != "quarantined" || !strings.Contains(rec.Reason, "larger") {
		t.Fatalf("recovery = %+v", rec)
	}
}

func TestDamageAfterOpenIsCaughtOnTheNextCall(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "Before")
	if err := os.WriteFile(path, []byte("{{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Places) != 0 || s.LastRecovery() == nil {
		t.Fatal("damage written behind the store's back was not recovered")
	}
}

// ---- revisions and undo ----------------------------------------------------

func TestRevisionCountsStructuralCommitsOnly(t *testing.T) {
	s, _ := newStore(t)
	if rev, _ := s.Revision(); rev != 0 {
		t.Fatalf("fresh revision = %d", rev)
	}
	p := mk(t, s, "A")
	if rc, err := s.Rename(p.ID, "A"); err != nil || !rc.Noop() {
		t.Fatalf("same-name rename: %v %+v", err, rc)
	}
	if err := s.TouchOpened(p.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if rev, _ := s.Revision(); rev != 1 {
		t.Fatalf("revision = %d, want 1 after one create, a no-op and a touch", rev)
	}
	if got, _ := snap(t, s).Place(p.ID); got.LastOpenedAt.IsZero() {
		t.Fatal("TouchOpened did not record")
	}
}

func TestUndoRestoresTheGraphAndIsRevisionChecked(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	rc := ok(t)(mustRename(s, a.ID, "Renamed"))
	if got, _ := snap(t, s).Place(a.ID); got.Name != "Renamed" {
		t.Fatal("rename did not apply")
	}
	rev, err := s.Undo(rc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sn := snap(t, s)
	if got, _ := sn.Place(a.ID); got.Name != "A" || sn.Revision != rev || rev != rc.AfterRevision+1 {
		t.Fatalf("after undo: name %q rev %d (receipt after %d)", got.Name, sn.Revision, rc.AfterRevision)
	}
	if _, err := s.Undo(rc.ID); !errors.Is(err, ErrNoReceipt) {
		t.Fatalf("second undo of the same receipt = %v", err)
	}
}

func mustRename(s *Store, id, name string) (Receipt, error) { return s.Rename(id, name) }

func TestUndoChainsBackThroughSeveralActions(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")                    // receipt 1 (create)
	r2 := ok(t)(mustRename(s, a.ID, "B")) // 2
	r3 := ok(t)(mustRename(s, a.ID, "C")) // 3
	for _, r := range []Receipt{r3, r2} {
		if _, err := s.Undo(r.ID); err != nil {
			t.Fatalf("undo %s: %v", r.Action, err)
		}
	}
	if got, _ := snap(t, s).Place(a.ID); got.Name != "A" {
		t.Fatalf("name = %q after two undos", got.Name)
	}
	// The create is still takeable-back: the place disappears.
	rcs := s.Receipts()
	if len(rcs) != 1 || rcs[0].Action != ActionCreate {
		t.Fatalf("receipts = %+v", rcs)
	}
	if _, err := s.Undo(rcs[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Places) != 0 {
		t.Fatal("create was not undone")
	}
}

func TestUndoRefusesAnOutOfOrderOrForeignChange(t *testing.T) {
	s, path := newStore(t)
	a := mk(t, s, "A")
	r2 := ok(t)(mustRename(s, a.ID, "B"))
	r3 := ok(t)(mustRename(s, a.ID, "C"))
	// The older receipt is not the latest change.
	if _, err := s.Undo(r2.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("out-of-order undo = %v", err)
	}
	// Another process moves the graph on: the latest receipt is now stale.
	other, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := other.CreatePlace(NewPlace{Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(r3.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale undo = %v", err)
	}
	if got, _ := snap(t, s).Place(a.ID); got.Name != "C" {
		t.Fatal("a refused undo changed the graph")
	}
}

func TestUndoRingKeepsTwentyAndTouchDoesNotBreakUndo(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "n0")
	var last Receipt
	for i := 1; i <= 25; i++ {
		last = ok(t)(mustRename(s, a.ID, fmt.Sprintf("n%d", i)))
	}
	rcs := s.Receipts()
	if len(rcs) != MaxUndo || rcs[len(rcs)-1].ID != last.ID {
		t.Fatalf("receipts = %d, newest ok = %v", len(rcs), rcs[len(rcs)-1].ID == last.ID)
	}
	if err := s.TouchOpened(a.ID, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(last.ID); err != nil {
		t.Fatalf("undo after a visit: %v", err)
	}
	got, _ := snap(t, s).Place(a.ID)
	if got.Name != "n24" || got.LastOpenedAt.Year() != 2026 || got.LastOpenedAt.Month() != 11 {
		t.Fatalf("name %q lastOpened %v (a visit must survive an undo)", got.Name, got.LastOpenedAt)
	}
}

func TestUndoOfADeleteRestoresPlacesMembershipsAndPins(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	mid := mk(t, s, "Mid", top.ID)
	leaf := mk(t, s, "Leaf", mid.ID)
	ok(t)(mustPin(s, mid.ID))
	if _, _, err := s.AddChat("c1", mid.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	before := snap(t, s)
	_, rc, err := s.DeletePlace(mid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after := snap(t, s); len(after.Places) != 2 || len(after.Memberships) != 0 || len(after.Pinned) != 0 {
		t.Fatalf("delete left %d places %d memberships %v pinned", len(after.Places), len(after.Memberships), after.Pinned)
	}
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	after := snap(t, s)
	b, a := before.State, after.State
	b.Revision, a.Revision = 0, 0
	jb, _ := json.Marshal(b)
	ja, _ := json.Marshal(a)
	if string(jb) != string(ja) {
		t.Fatalf("undo did not restore the exact graph\nbefore %s\nafter  %s", jb, ja)
	}
	if got, _ := after.Place(leaf.ID); !eq(got.Parents, []string{mid.ID}) {
		t.Fatalf("leaf parents = %v", got.Parents)
	}
}

func mustPin(s *Store, id string) (Receipt, error) { return s.Pin(id, -1) }

func TestUndoIsPerStoreAndNotPersisted(t *testing.T) {
	s, path := newStore(t)
	mk(t, s, "A")
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Receipts()) != 0 {
		t.Fatal("receipts survived a reopen")
	}
}

// ---- bounds ----------------------------------------------------------------

func TestSizeBoundsAreRefused(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.CreatePlace(NewPlace{Name: strings.Repeat("é", MaxNameRunes+1)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("long name = %v", err)
	}
	if _, _, err := s.CreatePlace(NewPlace{Name: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank name = %v", err)
	}
	if _, _, err := s.CreatePlace(NewPlace{Name: "tab\there"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("control char name = %v", err)
	}
	a := mk(t, s, "A")
	if _, err := s.SetContext(a.ID, Context{Instructions: strings.Repeat("x", MaxInstructions+1)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("instructions = %v", err)
	}
	src := make([]Source, MaxSources+1)
	for i := range src {
		src[i] = Source{Kind: SourceURL, Ref: fmt.Sprintf("https://example.test/%d", i)}
	}
	if _, err := s.SetContext(a.ID, Context{Sources: src}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("sources = %v", err)
	}
	if _, err := s.SetContext(a.ID, Context{Sources: []Source{{Kind: "carrier-pigeon", Ref: "x"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source kind = %v", err)
	}
	var ps []string
	for i := 0; i <= MaxParents; i++ {
		ps = append(ps, mk(t, s, fmt.Sprintf("P%d", i)).ID)
	}
	if _, err := s.Reparent(a.ID, ps); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("parents = %v", err)
	}
	for i := 0; i < MaxPinned; i++ {
		if i < len(ps) {
			ok(t)(mustPin(s, ps[i]))
		}
	}
	if rev, _ := s.Revision(); rev == 0 {
		t.Fatal("setup did not commit")
	}
	if _, err := s.SetPolicy(a.ID, Policy{Model: strings.Repeat("m", MaxPolicyFieldRune+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("policy = %v", err)
	}
}

func TestReservedManagerSurvivesRoundTripAndMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	doc := `{"version":1,"revision":1,"places":[
	 {"id":"a","name":"A","parents":[],"manager":{"mode":"later","n":[1,2]}},
	 {"id":"b","name":"B","parents":[]}],"memberships":[],"pinned":[]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("a", "A2"); err != nil {
		t.Fatal(err)
	}
	a, _ := snap(t, s).Place("a")
	var got map[string]any
	if err := json.Unmarshal(a.Manager, &got); err != nil || got["mode"] != "later" {
		t.Fatalf("manager = %s (%v)", a.Manager, err)
	}
	if _, _, err := s.MergePlaces("a", "b"); err != nil {
		t.Fatal(err)
	}
	b, _ := snap(t, s).Place("b")
	if !strings.Contains(string(b.Manager), "later") {
		t.Fatalf("merge dropped the reserved manager: %s", b.Manager)
	}
}
