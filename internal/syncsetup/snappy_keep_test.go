package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// refusingBranchDir is A's directory client while the relay will not record a
// kept branch: every Create that names a parent is refused, as a relay that is
// down for that one call would. Everything else goes through.
type refusingBranchDir struct {
	directory.Client
	refuse *atomic.Bool
}

func (d refusingBranchDir) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	if in.ParentCell != "" && d.refuse.Load() {
		return directory.CellView{}, blobstore.ErrUnreachable
	}
	return d.Client.Create(ctx, id, in)
}

// keptRig is the receiving computer's side of every "the copy here has a change
// and nothing vouches for it" scenario: A took the chat first, B moved it on
// with a new turn, and A's copy now has a local change that its stat index
// cannot vouch for (the index is gone).
type keptRig struct {
	*twoHomes
	seen     *directory.LeaseExpectation
	edited   string
	editBody string
	rows     func() []chatlist.Row
}

// newKeptRig builds that situation. editRel is the path A edits locally, relative
// to its work folder, and body is what it holds there.
func newKeptRig(t *testing.T, editRel, body string) *keptRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("kept rig initial turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
	mustNil(t, os.WriteFile(filepath.Join(bWork, "src", "part0.go"), []byte("package src\nconst Part0 = \"remote-from-B\"\n"), 0o600))
	b.mustSay("kept rig remote turn on B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	// A's stat index is gone, and A has a change of its own.
	_ = os.Remove(h.engine.StatIndexPath(h.cell))
	edited := filepath.Join(h.work, filepath.FromSlash(editRel))
	mustNil(t, os.MkdirAll(filepath.Dir(edited), 0o755))
	mustNil(t, os.WriteFile(edited, []byte(body), 0o600))

	rows, _, err := h.a.RowsAt(t.Context())
	mustNil(t, err)
	return &keptRig{
		twoHomes: h,
		seen:     &directory.LeaseExpectation{Device: rows[0].DeviceID, Fence: rows[0].Fence},
		edited:   edited,
		editBody: body,
		rows:     func() []chatlist.Row { r, _, _ := h.a.RowsAt(t.Context()); return r },
	}
}

// branchRows are the directory's rows that say a kept branch of the chat exists.
func (r *keptRig) branchRows() []chatlist.Row {
	var out []chatlist.Row
	for _, row := range r.rows() {
		if row.Status == chatlist.Branch && row.Parent == r.cell.ID {
			out = append(out, row)
		}
	}
	return out
}

// Contract F2 (what the person is told, and where the change is kept): the
// change a copy here has that no stat index vouches for is sealed, uploaded and
// RECORDED as a kept branch of the chat in the directory (the row the person
// opens or discards), the person is told in dev's own words, and only after
// both of those is the folder replaced by the arriving files. The change that
// the dirty check names as needing a choice is `.git/COMMIT_EDITMSG`, which is
// what a real Mac run carried.
func TestF2ChangeNoStatIndexVouchesForIsRecordedAndSaidBeforeFilesAreReplaced(t *testing.T) {
	const body = "wip: unsaved commit message typed on A\n"
	r := newKeptRig(t, ".git/COMMIT_EDITMSG", body)

	// The notice is the person's statement. When it is said the record must
	// already exist and the folder must not have been replaced yet: a person
	// told that something was kept must be able to find it, and a folder
	// replaced before the statement is a change lost without a word.
	type moment struct {
		said        string
		branchRows  int
		editOnDisk  bool
		editContent string
	}
	var told []moment
	cont := r.continuerA()
	cont.opt.Notify = func(line string) {
		raw, err := os.ReadFile(r.edited)
		told = append(told, moment{said: line, branchRows: len(r.branchRows()), editOnDisk: err == nil, editContent: string(raw)})
	}
	cont.opt.OnShown = func(Continued) {}

	got, err := cont.TakeSeen(t.Context(), r.cell.ID, r.seen)
	mustNil(t, err)
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}

	keptID, keptTurns := got.Taken.Kept, got.KeptTurns
	if keptID == "" && got.KeptBranch != nil {
		keptID, keptTurns = got.KeptBranch()
	}
	if keptID == "" || keptTurns == 0 {
		t.Fatalf("the change was not kept: keptID=%q keptTurns=%d", keptID, keptTurns)
	}
	want := chatlist.KeptEdits(keptTurns, nameA)
	if len(told) != 1 || told[0].said != want {
		t.Fatalf("the person was told %v, want exactly %q", told, want)
	}
	if told[0].branchRows != 1 {
		t.Errorf("when the person was told, the directory listed %d kept branches of the chat, want 1: a statement with nothing to open", told[0].branchRows)
	}
	if !told[0].editOnDisk || told[0].editContent != body {
		t.Errorf("when the person was told, the change on disk was %q (present=%v), want it still in place: the folder was replaced before the statement", told[0].editContent, told[0].editOnDisk)
	}
	if rows := r.branchRows(); len(rows) != 1 || rows[0].Cell != keptID {
		t.Fatalf("directory kept branches after the move = %+v, want one named %s", rows, keptID)
	}
	if _, err := os.Stat(r.edited); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the arriving files left the local change in place (stat err %v): the folder should match the incoming tree", err)
	}

	// The kept branch holds the changed file, byte for byte.
	branchRoot := filepath.Join(t.TempDir(), keptID)
	opener := r.a.Continuer(r.engine, TakeOptions{RootFor: func(string) string { return branchRoot }})
	opened, err := opener.OpenBranch(t.Context(), keptID)
	mustNil(t, err)
	if opened.Wait != nil {
		mustNil(t, opened.Wait(t.Context()))
	}
	raw, err := os.ReadFile(filepath.Join(workspaceOf(opened.Taken.Cell.Root), ".git", "COMMIT_EDITMSG"))
	mustNil(t, err)
	if string(raw) != body {
		t.Fatalf("kept branch .git/COMMIT_EDITMSG = %q, want %q", raw, body)
	}
}

// Contract F2 (never lost, never silently overwritten): when the directory will
// not record the kept branch, the change a copy here has is NOT replaced by the
// arriving files. The move says so, the files stay as they are, and a retry
// that the relay accepts keeps the change, tells the person once, and only then
// puts the incoming files in place.
func TestF2ChangeThatCannotBeKeptIsNotReplacedUntilItIs(t *testing.T) {
	const body = "wip: unsaved commit message typed on A\n"
	r := newKeptRig(t, ".git/COMMIT_EDITMSG", body)

	var refuse atomic.Bool
	refuse.Store(true)
	r.a.Dir = refusingBranchDir{Client: r.a.Dir, refuse: &refuse}

	cont := r.continuerA()
	cont.opt.OnShown = func(Continued) {}
	var progressMu sync.Mutex
	var failedWith []string
	cont.opt.OnMove = func(p MoveProgress) {
		if p.Err != "" {
			progressMu.Lock()
			failedWith = append(failedWith, p.Err)
			progressMu.Unlock()
		}
	}
	got, err := cont.TakeSeen(t.Context(), r.cell.ID, r.seen)
	mustNil(t, err)

	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	waitErr := got.Wait(waitCtx)
	if waitErr == nil {
		t.Fatalf("the files arrived although the change could not be kept; local change on disk = %v", fileHolds(r.edited, body))
	}
	if !fileHolds(r.edited, body) {
		t.Fatalf("the change was replaced although it could not be kept (wait said %v)", waitErr)
	}
	if told := r.wall.all(); len(told) != 0 && strings.Contains(strings.Join(told, "\n"), "were kept") {
		t.Fatalf("the person was told the edits were kept when they were not: %q", told)
	}
	// What the person reads: the status line's reason and the notice both say what
	// could not be kept, and the notice says the folder was left alone.
	wantReason := "your unsaved edits here could not be kept yet (sync could not be reached)"
	progressMu.Lock()
	reasons := append([]string(nil), failedWith...)
	progressMu.Unlock()
	if len(reasons) == 0 || reasons[len(reasons)-1] != wantReason {
		t.Errorf("the status line's reason = %q, want %q", reasons, wantReason)
	}
	if !r.wall.has(wantReason + ", so the files here are left as they are") {
		t.Errorf("the person was not told the change could not be kept and the files were left alone: %q", r.wall.all())
	}
	if len(r.branchRows()) != 0 {
		t.Fatalf("a kept branch exists although the directory refused it: %+v", r.branchRows())
	}

	// The relay accepts it again; retrying keeps the change and lets the files in.
	refuse.Store(false)
	if !got.Retry() {
		t.Fatal("the failed arrival offers no retry")
	}
	mustNil(t, got.Wait(waitCtx))
	keptID, keptTurns := got.KeptBranch()
	if keptID == "" || keptTurns == 0 {
		t.Fatalf("the retry kept nothing: keptID=%q turns=%d", keptID, keptTurns)
	}
	if rows := r.branchRows(); len(rows) != 1 || rows[0].Cell != keptID {
		t.Fatalf("directory kept branches after the retry = %+v, want one named %s", rows, keptID)
	}
	want := chatlist.KeptEdits(keptTurns, nameA)
	said := 0
	for _, line := range r.wall.all() {
		if line == want {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the person was told %q %d times, want once; all notices %q", want, said, r.wall.all())
	}
	if fileHolds(r.edited, body) {
		t.Fatalf("the retry kept the change but left the folder as it was")
	}
}

func fileHolds(path, body string) bool {
	raw, err := os.ReadFile(path)
	return err == nil && string(raw) == body
}

// Contract F2 (fails closed): when this computer cannot set its older copy of
// the chat aside, so there would be nothing left to check its edits against, the
// move stops before anything is replaced. The change is still on disk, the
// person is told why in plain words, nothing says it was kept, and moving again
// once the disk allows it keeps the change.
func TestF2OlderCopyThatCannotBeSetAsideIsNotReplaced(t *testing.T) {
	const body = "wip: unsaved commit message typed on A\n"
	r := newKeptRig(t, ".git/COMMIT_EDITMSG", body)

	origTemp := preTakeTemp
	preTakeTemp = func(string, string) (string, error) { return "", errors.New("no space left on device") }
	t.Cleanup(func() { preTakeTemp = origTemp })

	cont := r.continuerA()
	cont.opt.OnShown = func(Continued) {}
	_, err := cont.TakeSeen(t.Context(), r.cell.ID, r.seen)
	if err == nil {
		t.Fatalf("the move went on although the older copy could not be set aside; local change on disk = %v", fileHolds(r.edited, body))
	}
	if !strings.Contains(err.Error(), "could not set this computer's older copy of the chat aside") || !strings.Contains(err.Error(), "nothing was replaced") {
		t.Errorf("the failure does not say what happened in plain words: %v", err)
	}
	if !fileHolds(r.edited, body) {
		t.Fatalf("the change was replaced although the move was refused (error %v)", err)
	}
	if said := strings.Join(r.wall.all(), "\n"); strings.Contains(said, "were kept") {
		t.Fatalf("the person was told the edits were kept when nothing was done: %q", said)
	}
	if len(r.branchRows()) != 0 {
		t.Fatalf("a kept branch exists although the move was refused: %+v", r.branchRows())
	}

	// With room again, the same move keeps the change and then lets the files in.
	preTakeTemp = origTemp
	again := r.continuerA()
	again.opt.OnShown = func(Continued) {}
	got, err := again.Take(t.Context(), r.cell.ID)
	mustNil(t, err)
	mustNil(t, got.Wait(t.Context()))
	if keptID, turns := got.KeptBranch(); keptID == "" || turns == 0 {
		t.Fatalf("the second move kept nothing: keptID=%q turns=%d", keptID, turns)
	}
	if fileHolds(r.edited, body) {
		t.Fatalf("the second move kept the change but left the folder as it was")
	}
}
