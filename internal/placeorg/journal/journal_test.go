package journal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestReplayOfAJournaledRunReproducesThePlansOpsInOrder(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	ops := []placeorg.Op{
		{Kind: placeorg.OpFile, Stage: "park", Place: "p1", Chat: "c1", Why: "closest", Level: 2},
		{Kind: placeorg.OpBirth, Stage: "stream", Place: "p2", Parent: "p1", Members: []string{"c2", "c3", "c4", "c5"}, Name: "north", Why: "four arrived together", Proposed: true, Scores: []float32{0.5, 0.25}},
		{Kind: placeorg.OpLink, Stage: "link", Place: "p3", Chat: "c1", Why: "also close"},
	}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryRunStarted, Note: "add-on"})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Stage: "park", Op: &ops[0]})
	other := placeorg.Op{Kind: placeorg.OpInbox, Stage: "park", Chat: "c9", Why: "below the floor"}
	mustWrite(t, j, placeorg.Entry{Run: "run-b", Kind: placeorg.EntryOp, Op: &other})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryStage, Stage: "park", Note: "parked"})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &ops[1]})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &ops[2]})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryRunEnded})

	got, err := j.Ops("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ops) {
		t.Fatalf("ops = %#v, want %#v", got, ops)
	}
	again, err := Replay(dir, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, ops) {
		t.Fatalf("replay = %#v, want %#v", again, ops)
	}
	foreign, err := Replay(dir, "run-b")
	if err != nil || !reflect.DeepEqual(foreign, []placeorg.Op{other}) {
		t.Fatalf("other run = %#v, %v", foreign, err)
	}
}

func TestACrashBetweenLinesLeavesEarlierLinesReadable(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	first := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c1", Place: "p1", Why: "home"}
	second := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c2", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &first})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &second})
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"run":"run-a","kind":"op","op":{"kind":"file"`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Replay(dir, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []placeorg.Op{first, second}) {
		t.Fatalf("torn file replayed %#v", got)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasSuffix(body, []byte("\n")) {
		t.Fatal("the torn tail still ended on a newline")
	}

	reopened := mustOpen(t, dir)
	third := placeorg.Op{Kind: placeorg.OpPark, Stage: "park", Chat: "c3", Place: "p1", Why: "one level up", Level: 1}
	mustWrite(t, reopened, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &third})
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"kind":"file"`)) && bytes.Contains(body, []byte(`{"run":"run-a","kind":"op","op":{"kind":"file"`)) {
		t.Fatal("reopen left the torn tail in the file")
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		t.Fatal("the repaired file does not end on a finished line")
	}
	got, err = Replay(dir, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []placeorg.Op{first, second, third}) {
		t.Fatalf("after reopen = %#v", got)
	}
}

func TestAShortWriteDoesNotGlueTheNextLine(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	kept := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c1", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &kept})
	j.writeLine = func(f *os.File, p []byte) (int, error) {
		if len(p) < 4 {
			t.Fatalf("line is %d bytes", len(p))
		}
		n, err := f.Write(p[:4])
		if err != nil {
			return n, err
		}
		return n, io.ErrShortWrite
	}
	dropped := placeorg.Op{Kind: placeorg.OpInbox, Stage: "park", Chat: "c2", Why: "below the floor"}
	if err := j.Write(placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &dropped}); err == nil {
		t.Fatal("short write returned nil")
	}
	j.writeLine = func(f *os.File, p []byte) (int, error) { return f.Write(p) }
	next := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c3", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &next})
	got, err := j.Ops("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []placeorg.Op{kept, next}) {
		t.Fatalf("ops = %#v", got)
	}
}

func TestTheRecordIsSyncedWhenARunEnds(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	syncs := 0
	j.syncFile = func(f *os.File) error {
		syncs++
		return f.Sync()
	}
	op := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c1", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &op})
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryStage, Stage: "park"})
	if syncs != 0 {
		t.Fatalf("synced %d times before the run ended", syncs)
	}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryRunEnded})
	if syncs != 1 {
		t.Fatalf("synced %d times at run end, want 1", syncs)
	}
	mustWrite(t, j, placeorg.Entry{Run: "run-b", Kind: placeorg.EntryRunStarted})
	if syncs != 1 {
		t.Fatalf("a later line synced again: %d", syncs)
	}

	j.syncFile = func(*os.File) error { return errors.New("disk full") }
	err := j.Write(placeorg.Entry{Run: "run-b", Kind: placeorg.EntryRunEnded, Note: "cancelled"})
	if err == nil {
		t.Fatal("a failed sync was treated as durable")
	}
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"kind":"run.ended"`)) || !bytes.Contains(body, []byte(`"note":"cancelled"`)) {
		t.Fatalf("the ended line was rolled back:\n%s", body)
	}
	got, err := Replay(dir, "run-a")
	if err != nil || !reflect.DeepEqual(got, []placeorg.Op{op}) {
		t.Fatalf("replay after a failed sync = %#v, %v", got, err)
	}
}

func TestRotationKeepsARunsOpsInOrder(t *testing.T) {
	dir := t.TempDir()
	j, err := open(dir, 80)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	var ops []placeorg.Op
	for i := 0; i < 6; i++ {
		op := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c" + strconv.Itoa(i), Place: "p1", Why: "filed beside the others"}
		ops = append(ops, op)
		mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &op})
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".1")); err != nil {
		t.Fatal("expected a sealed segment", err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= 80 && info.Size() > 400 {
		t.Fatalf("live segment is %d bytes; rotation did not cut it", info.Size())
	}
	got, err := Replay(dir, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ops) {
		t.Fatalf("rotated replay = %#v, want %#v", got, ops)
	}
}

func TestOpenSealsAFileLeftOverTheLimit(t *testing.T) {
	dir := t.TempDir()
	j, err := open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	op := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c1", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &op})
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := open(dir, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("live file is %d bytes after sealing the oversized segment", info.Size())
	}
	got, err := Replay(dir, "run-a")
	if err != nil || !reflect.DeepEqual(got, []placeorg.Op{op}) {
		t.Fatalf("sealed replay = %#v, %v", got, err)
	}
}

func TestABrokenLineInTheMiddleIsAnError(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	op := placeorg.Op{Kind: placeorg.OpFile, Stage: "park", Chat: "c1", Place: "p1", Why: "home"}
	mustWrite(t, j, placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &op})
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte(nil), body...), []byte("{not json}\n")...)
	broken = append(broken, body...)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(dir, "run-a"); err == nil {
		t.Fatal("a finished line that is not JSON was replayed")
	}
}

func TestConcurrentWritesStayOneJSONLineEach(t *testing.T) {
	dir := t.TempDir()
	j := mustOpen(t, dir)
	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := placeorg.Op{Kind: placeorg.OpInbox, Stage: "park", Chat: strconv.Itoa(i), Why: "below the floor"}
			if err := j.Write(placeorg.Entry{Run: "run-a", Kind: placeorg.EntryOp, Op: &op}); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got, err := j.Ops("run-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("got %d ops", len(got))
	}
	seen := map[string]bool{}
	for _, op := range got {
		if seen[op.Chat] {
			t.Fatalf("duplicate chat %s", op.Chat)
		}
		seen[op.Chat] = true
	}
}

func TestClosedJournalRefusesAnotherLine(t *testing.T) {
	j := mustOpen(t, t.TempDir())
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	err := j.Write(placeorg.Entry{Run: "run-a", Kind: placeorg.EntryRunEnded})
	if !errors.Is(err, errClosed) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestRotateAtIsTheArchitectureCut(t *testing.T) {
	if FileName != "places-journal.jsonl" {
		t.Fatalf("FileName = %q", FileName)
	}
	if RotateAt != 32<<20 {
		t.Fatalf("RotateAt = %d, architecture says 32 MB", RotateAt)
	}
}

func mustOpen(t *testing.T, dir string) *File {
	t.Helper()
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	return j
}

func mustWrite(t *testing.T, j *File, e placeorg.Entry) {
	t.Helper()
	if err := j.Write(e); err != nil {
		t.Fatal(err)
	}
}
