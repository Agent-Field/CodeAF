package teams

import (
	"errors"
	"os"
	"testing"
	"time"
)

// A STAMP MOVES WITH EVERY WRITE, EVEN INSIDE ONE CLOCK TICK. Two writes of the
// same size, with the file's time pinned in between as a coarse filesystem
// would leave it, still give two stamps; a file that is not there has the
// missing stamp, never "".
func TestStampMovesWithEveryWrite(t *testing.T) {
	dir := t.TempDir()
	if got := Stamp(dir); got != MissingStamp {
		t.Fatalf("a missing file's stamp is %q", got)
	}
	one := []Team{{ID: "0a0a0a0a0a0a", Name: "aaaa"}}
	two := []Team{{ID: "0a0a0a0a0a0a", Name: "bbbb"}}
	if err := Save(dir, one); err != nil {
		t.Fatal(err)
	}
	pinned := time.Now().Add(time.Hour)
	if err := os.Chtimes(Path(dir), pinned, pinned); err != nil {
		t.Fatal(err)
	}
	first := Stamp(dir)
	if err := Save(dir, two); err != nil {
		t.Fatal(err)
	}
	if second := Stamp(dir); second == first || second == "" {
		t.Fatalf("a write of the same size did not move the stamp: %q then %q", first, second)
	}
	if !modTime(Path(dir)).After(pinned) {
		t.Fatal("the write left the file's time behind the one it replaced")
	}
}

// CHANGEIF IS A COMPARE-AND-SWAP. At the stamp it was given it writes and
// answers the new stamp and the list it wrote; after another writer it is
// ErrStale, writes nothing and does not call its change.
func TestChangeIfRefusesAFileThatMoved(t *testing.T) {
	dir := t.TempDir()
	base := Stamp(dir)
	f, stamp, err := ChangeIf(dir, base, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: "0a0a0a0a0a0a", Name: "port"})
		return nil
	})
	if err != nil || len(f.Teams) != 1 || stamp == base || stamp != Stamp(dir) {
		t.Fatalf("the first swap: %+v, %q (base %q), %v", f, stamp, base, err)
	}
	if err := Update(dir, func(f *File) error { f.Teams[0].Name = "moved"; return nil }); err != nil {
		t.Fatal(err)
	}
	called := false
	_, _, err = ChangeIf(dir, stamp, func(f *File) error { called = true; return nil })
	if !errors.Is(err, ErrStale) || called {
		t.Fatalf("a swap on a moved file answered %v (change called %v)", err, called)
	}
	got, _ := Load(dir)
	if got.Teams[0].Name != "moved" {
		t.Fatalf("the refused swap wrote: %+v", got.Teams)
	}
	// "" is a reader that never looked, and is always stale.
	if _, _, err := ChangeIf(dir, "", func(*File) error { return nil }); !errors.Is(err, ErrStale) {
		t.Fatalf("an empty base was taken: %v", err)
	}
}

// THE WATCH ANSWERS A QUIET LOG FROM A STAT. Asked again from the cursor its
// last read left, with nothing appended, it reads nothing; a line appended is
// found on the next ask; a full page is not remembered, so the rest is read.
func TestWatchReadsOnlyWhatMoved(t *testing.T) {
	dir := t.TempDir()
	const id = "harbor"
	for i := 0; i < 3; i++ {
		if err := AppendTraffic(dir, id, Entry{Kind: KindNote, From: "a", To: "b", Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	var w Watch
	got, _, err := w.Traffic(dir, id, "", 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("the tail: %d entries, %v", len(got), err)
	}
	cursor := got[2].ID
	reads := w.reads
	for i := 0; i < 5; i++ {
		if more, _, _ := w.Traffic(dir, id, cursor, 10); len(more) != 0 {
			t.Fatalf("a quiet log answered %d entries", len(more))
		}
	}
	if w.reads != reads {
		t.Fatalf("five quiet asks made %d reads", w.reads-reads)
	}
	if err := AppendTraffic(dir, id, Entry{Kind: KindNote, From: "a", To: "b", Text: "y"}); err != nil {
		t.Fatal(err)
	}
	more, _, _ := w.Traffic(dir, id, cursor, 10)
	if len(more) != 1 || more[0].Text != "y" {
		t.Fatalf("the appended line: %+v", more)
	}
	// A page of exactly the limit is not a cursor to rest on.
	page, _, _ := w.Traffic(dir, id, "000000000000", 2)
	reads = w.reads
	if _, _, _ = w.Traffic(dir, id, page[1].ID, 2); len(page) != 2 || w.reads != reads+1 {
		t.Fatalf("a full page was remembered as the end (%d entries, %d reads)", len(page), w.reads-reads)
	}
}
