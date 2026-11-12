package store

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// editInPlace changes job id's document the way another process's editor
// would: the same file written over, not a fresh file renamed into place, and
// a modification time no write of this test could have given it.
func editInPlace(t *testing.T, st *Store, id int, at time.Time, change func(it map[string]any)) {
	t.Helper()
	path := st.ItemPath(id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	change(d["item"].(map[string]any))
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(out); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// AN UNCHANGED FLOOR IS LISTED FROM MEMORY, AND AN EDIT ON DISK IS SEEN. The
// settle lists the floor every five seconds while a lease is held; it now reads
// only the jobs whose file changed, and answers what [Store.List] answers.
func TestASharedListingOfAnUnchangedFloorReadsNothingAndSeesAnEdit(t *testing.T) {
	st := open(t)
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	st.clock = func() time.Time { return at }
	var ids []int
	for i := range 30 {
		it, err := st.Create(factory.Item{Title: "backlog", Num: i + 1, Kind: factory.KindIssue})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	first, err := st.ListShared()
	if err != nil || len(first) != 30 {
		t.Fatalf("ListShared = %d items, %v", len(first), err)
	}
	want, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatal("the shared listing is not what List answers")
	}

	// A caller may replace what it was handed; the next listing is unchanged.
	first[0] = factory.Item{}
	before := SharedReads()
	for range 5 {
		again, err := st.ListShared()
		if err != nil || !reflect.DeepEqual(again, want) {
			t.Fatalf("an unchanged floor listed differently: %v", err)
		}
	}
	if got := SharedReads() - before; got != 0 {
		t.Fatalf("five listings of an unchanged floor read %d jobs, want 0", got)
	}

	// Another process edits one job in place.
	editInPlace(t, st, ids[6], at.Add(time.Hour), func(it map[string]any) { it["Title"] = "edited elsewhere" })
	before = SharedReads()
	edited, err := st.ListShared()
	if err != nil {
		t.Fatal(err)
	}
	if got := SharedReads() - before; got != 1 {
		t.Fatalf("one edited job read %d jobs again, want 1", got)
	}
	want, _ = st.List()
	if !reflect.DeepEqual(edited, want) {
		t.Fatal("the edited floor listed differently from List")
	}
	seen := false
	for _, it := range edited {
		seen = seen || (it.ID == ids[6] && it.Title == "edited elsewhere")
	}
	if !seen {
		t.Fatal("the edit made on disk was not seen")
	}

	// A job that leaves the floor leaves the listing.
	if err := os.Remove(st.ItemPath(ids[0])); err != nil {
		t.Fatal(err)
	}
	if gone, _ := st.ListShared(); len(gone) != 29 {
		t.Fatalf("a removed job: %d items, want 29", len(gone))
	}
}

// RETENTION IS APPLIED TO EVERY LISTING, AND NEVER TO WHAT IS SHARED. A shared
// job's output is trimmed on a copy of its stream, so a listing under a tighter
// policy does not shorten the log the next listing is handed.
func TestASharedListingTrimsOutputWithoutChangingTheSharedJob(t *testing.T) {
	st := open(t)
	if _, err := st.Create(factory.Item{Title: "logged", Kind: factory.KindIssue, Stream: &factory.Stream{Log: []factory.LogLine{
		{Text: strings.Repeat("a", 8)}, {Text: strings.Repeat("b", 8)}, {Text: strings.Repeat("c", 8)},
	}}}); err != nil {
		t.Fatal(err)
	}
	days, bytes := 0, 0
	st.SetOutputPolicy(func() (int, int) { return days, bytes })
	if items, _ := st.ListShared(); len(items[0].Stream.Log) != 3 {
		t.Fatalf("no policy kept %d lines, want 3", len(items[0].Stream.Log))
	}
	days, bytes = 1, 16
	trimmed, _ := st.ListShared()
	if got := trimmed[0].Stream.Log; len(got) != 2 || got[0].Text[0] != 'b' {
		t.Fatalf("16 bytes kept %v, want the last two lines", got)
	}
	want, _ := st.List()
	if !reflect.DeepEqual(trimmed, want) {
		t.Fatal("the trimmed listing is not what List answers")
	}
	days, bytes = 0, 0
	if items, _ := st.ListShared(); len(items[0].Stream.Log) != 3 {
		t.Fatalf("after a trimmed listing the shared job kept %d lines, want 3", len(items[0].Stream.Log))
	}
}

// A stamp is the same while the job's file is, and differs once anything
// writes it, in place or by rename.
func TestAStampChangesWithTheJobsFile(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Title: "held", Kind: factory.KindIssue})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Stamp(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Stamp(it.ID); !a.Same(b) {
		t.Fatal("an unchanged job's stamps differ")
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "renamed"; return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := st.Stamp(it.ID)
	if a.Same(b) {
		t.Fatal("a store write left the stamp the same")
	}
	editInPlace(t, st, it.ID, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), func(m map[string]any) { m["Title"] = "by hand" })
	if c, _ := st.Stamp(it.ID); b.Same(c) {
		t.Fatal("an edit in place left the stamp the same")
	}
	if (Stamp{}).Same(Stamp{}) {
		t.Fatal("two empty stamps are the same")
	}
}
