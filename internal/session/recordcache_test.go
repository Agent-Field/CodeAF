package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// recordJournal is a journal of turns, each a question, a call, its result and
// an answer, with a compaction above the last few when compacted is set.
func recordJournal(turns int, compacted bool) string {
	var b strings.Builder
	b.WriteString(`{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}` + "\n")
	turn := func(n int, answer string) {
		fmt.Fprintf(&b, `{"type":"message","role":"user","content":"question %d about the loader"}`+"\n", n)
		fmt.Fprintf(&b, `{"type":"message","role":"assistant","content":"Reading it.","toolCalls":[{"id":"c%d","function":{"name":"bash","arguments":"{\"command\":\"go test ./loader/... -run Test%d\"}"}}]}`+"\n", n, n)
		fmt.Fprintf(&b, `{"type":"message","role":"tool","toolCallId":"c%d","content":"ok  loader 0.%03ds"}`+"\n", n, n%1000)
		fmt.Fprintf(&b, `{"type":"message","role":"assistant","content":"%s %d"}`+"\n", answer, n)
	}
	for n := range turns {
		turn(n, "the loader passes")
	}
	if compacted && turns > 0 {
		b.WriteString(`{"type":"compaction","stubbed":2,"window":2,"tokensBefore":84000,"timestamp":"t"}` + "\n")
		b.WriteString(`{"type":"message","role":"user","content":"question folded"}` + "\n")
		b.WriteString(`{"type":"message","role":"assistant","content":"[folded]"}` + "\n")
		turn(turns, "after the pass")
	}
	return b.String()
}

func writeRecord(t testing.TB, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// settledCache is a cache whose clock stands an hour ahead, so a file the test
// just wrote is old enough to keep.
func settledCache(limits RecordCacheLimits) *RecordCache {
	limits.Now = func() time.Time { return time.Now().Add(time.Hour) }
	return NewRecordCache(limits)
}

// read is one reading through the cache, failing the test on a refusal.
func read(t testing.TB, cache *RecordCache, path string) Record {
	t.Helper()
	var got Record
	if err := cache.WithRecord(path, func(record Record) error { got = record; return nil }); err != nil {
		t.Fatalf("WithRecord(%s): %v", path, err)
	}
	return got
}

// A KEPT READING IS THE READING: entries, the compacted region, its floor and
// the request books are what the one parser makes of the file, and the second
// reading parses nothing.
func TestAKeptRecordIsExactlyThePlainReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeRecord(t, path, recordJournal(30, true))
	want := ReadTranscript(path)
	if len(want.Earlier) == 0 || want.Floor == 0 {
		t.Fatalf("the fixture did not compact: earlier %d floor %d", len(want.Earlier), want.Floor)
	}
	cache := settledCache(RecordCacheLimits{})
	for range 3 {
		if got := read(t, cache, path); !reflect.DeepEqual(got, want) {
			t.Fatal("the cached reading differs from the plain one")
		}
	}
	if s := cache.Stats(); s.Parses != 1 || s.Hits != 2 || s.Entries != 1 || s.Reserved != 0 || s.Operations != 0 {
		t.Fatalf("stats = %+v, want one parse, two hits and nothing held", s)
	}
}

// ANY WRITE IS READ AGAIN: an append, a truncation, a file replaced by rename,
// and an in-place rewrite to the same size with its modification time put back.
func TestEveryChangeToTheJournalIsReadAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.jsonl")
	writeRecord(t, path, recordJournal(4, false))
	cache := settledCache(RecordCacheLimits{})
	read(t, cache, path)

	check := func(why string) {
		t.Helper()
		if got, want := read(t, cache, path), ReadTranscript(path); !reflect.DeepEqual(got, want) {
			t.Fatalf("after %s the cache answered a stale record", why)
		}
	}
	appended := recordJournal(5, false)
	writeRecord(t, path, appended)
	check("an append")

	writeRecord(t, path, recordJournal(2, false))
	check("a truncation")

	other := filepath.Join(dir, "next.jsonl")
	writeRecord(t, other, recordJournal(3, false))
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	check("a replacement")

	// The same bytes count, the same modification time, other words.
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	read(t, cache, path)
	stamp, ok := recordStampOf(before)
	if !ok {
		t.Skip("this platform keeps nothing, so nothing can go stale")
	}
	// A change time on a coarse tick: move it on before the rewrite, so the
	// rewrite is told apart by the stamp and not by luck.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(path)
		now, _ := recordStampOf(info)
		if now.ctime != stamp.ctime {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the change time never moved")
		}
	}
	text, _ := os.ReadFile(path)
	rewritten := strings.Replace(string(text), "question 0 about", "QUESTION 0 ABOUT", 1)
	if len(rewritten) != len(text) || rewritten == string(text) {
		t.Fatal("the rewrite is not a same-size change")
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte(rewritten), 0); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		t.Fatal("the rewrite changed size, time or identity")
	}
	got := read(t, cache, path)
	if got.Entries[0].Text != "QUESTION 0 ABOUT the loader" {
		t.Fatalf("a same-size rewrite read back as %q", got.Entries[0].Text)
	}
}

// A FILE WRITTEN A MOMENT AGO IS NOT KEPT: its change time may share a tick
// with the next write.
func TestAJournalStillBeingWrittenIsNotKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeRecord(t, path, recordJournal(3, false))
	cache := NewRecordCache(RecordCacheLimits{})
	read(t, cache, path)
	read(t, cache, path)
	if s := cache.Stats(); s.Entries != 0 || s.Parses != 2 {
		t.Fatalf("stats = %+v, want two parses and nothing kept", s)
	}
}

// A READING THAT SAID SOMETHING WAS WRONG IS NOT KEPT, and a fault is never
// answered from memory once the file is mended.
func TestAShortOrRefusedReadingIsNeverKept(t *testing.T) {
	dir := t.TempDir()
	cache := settledCache(RecordCacheLimits{})

	broken := filepath.Join(dir, "broken.jsonl")
	good := recordJournal(2, false)
	writeRecord(t, broken, good+`{"type":"message","role":"user","content":`+strings.Repeat("x", 9<<20)+"\n")
	for range 2 {
		if got := read(t, cache, broken); got.Unreadable == "" || !reflect.DeepEqual(got, ReadTranscript(broken)) {
			t.Fatalf("a short reading came back as %q", got.Unreadable)
		}
	}
	writeRecord(t, broken, good)
	if got := read(t, cache, broken); got.Unreadable != "" {
		t.Fatal("a mended journal was answered with its old fault")
	}

	newer := filepath.Join(dir, "newer.jsonl")
	writeRecord(t, newer, `{"type":"session","version":`+strconv.Itoa(sessionFileVersion+1)+`,"id":"n1"}`+"\n")
	for range 2 {
		if got := read(t, cache, newer); got.Unreadable != unreadNewerWord {
			t.Fatalf("a newer journal read as %q", got.Unreadable)
		}
	}

	if got := read(t, cache, filepath.Join(dir, "missing.jsonl")); !reflect.DeepEqual(got, Record{}) {
		t.Fatalf("a missing journal read as %+v", got)
	}
	if got := read(t, cache, dir); got.Unreadable == "" || len(got.Entries) != 0 {
		t.Fatalf("a directory read as %+v", got)
	}
	if s := cache.Stats(); s.Entries != 1 || s.Reserved != 0 {
		t.Fatalf("stats = %+v, want only the mended journal kept", s)
	}
}

// A NAMED PIPE IS NEVER OPENED: no writer is coming, and a reader that opened
// it would hold its slot forever.
func TestANamedPipeIsRefusedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.jsonl")
	if err := mkfifo(path); err != nil {
		t.Skipf("no named pipes here: %v", err)
	}
	cache := settledCache(RecordCacheLimits{})
	done := make(chan Record, 1)
	go func() { done <- read(t, cache, path) }()
	select {
	case got := <-done:
		if got.Unreadable == "" {
			t.Fatal("a named pipe read as an empty journal")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a named pipe blocked")
	}
	if s := cache.Stats(); s.Operations != 0 || s.Entries != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

// EVERY READING PASSES THE GATE. With both slots held by callbacks still
// running, a third reading — of a kept record, of a file never seen, of one too
// large to keep — is refused, and nothing is parsed for it.
func TestEveryKindOfReadingIsRefusedWhenTheSlotsAreTaken(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	large := filepath.Join(dir, "large.jsonl")
	writeRecord(t, kept, recordJournal(3, false))
	writeRecord(t, fresh, recordJournal(3, false))
	writeRecord(t, large, recordJournal(400, false))
	cache := settledCache(RecordCacheLimits{FileBytes: 4 << 10})
	read(t, cache, kept)

	release := make(chan struct{})
	var held sync.WaitGroup
	for _, path := range []string{kept, large} {
		entered := make(chan struct{})
		held.Add(1)
		go func() {
			defer held.Done()
			_ = cache.WithRecord(path, func(Record) error { close(entered); <-release; return nil })
		}()
		<-entered
	}
	parses := cache.Stats().Parses
	for _, path := range []string{kept, fresh, large} {
		err := cache.WithRecord(path, func(Record) error { t.Fatal("a refused reading ran its callback"); return nil })
		if !errors.Is(err, ErrRecordReadBusy) {
			t.Fatalf("a third reading of %s = %v, want busy", filepath.Base(path), err)
		}
	}
	if s := cache.Stats(); s.Parses != parses || s.Operations != 2 || s.Busy != 3 {
		t.Fatalf("stats = %+v", s)
	}
	close(release)
	held.Wait()
	if s := cache.Stats(); s.Operations != 0 || s.Reserved != 0 {
		t.Fatalf("after the callbacks returned: %+v", s)
	}
	// And the large journal, never kept, still reads whole.
	if got := read(t, cache, large); !reflect.DeepEqual(got, ReadTranscript(large)) || len(got.Entries) != 2000 {
		t.Fatalf("the large journal read %d entries", len(got.Entries))
	}
}

// THE BUDGET COUNTS WHAT IS OUT. A copy handed to a running callback is
// reserved, and a reading the budget cannot hold beside it is refused rather
// than read anyway.
func TestACopyInUseHoldsItsShareOfTheBudget(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	writeRecord(t, first, recordJournal(40, false))
	writeRecord(t, second, recordJournal(40, false))
	size := int64(len(recordJournal(40, false)))
	// Room for one kept record and its copy, and not for a second parse.
	cache := settledCache(RecordCacheLimits{Bytes: recordReserve(size) + recordReserve(size)/2})
	read(t, cache, first)
	kept := cache.Stats().Retained

	release := make(chan struct{})
	entered := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = cache.WithRecord(first, func(Record) error { close(entered); <-release; return nil })
	}()
	<-entered
	s := cache.Stats()
	if s.Reserved <= 0 || s.Retained != kept {
		t.Fatalf("a copy in use is not counted: %+v", s)
	}
	if err := cache.WithRecord(second, func(Record) error { return nil }); !errors.Is(err, ErrRecordReadBusy) {
		t.Fatalf("a parse past the budget = %v, want busy", err)
	}
	close(release)
	<-finished
	// With the copy back, the kept record may be evicted to make room.
	if got := read(t, cache, second); len(got.Entries) != 200 {
		t.Fatalf("second read %d entries", len(got.Entries))
	}
	if s := cache.Stats(); s.Retained > cache.limits.Bytes || s.Reserved != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

// AN EVICTED RECORD STAYS COUNTED WHILE A COPY OF IT IS OUT, because the copy
// still holds its strings.
func TestAnEvictedRecordIsCountedUntilItsLastCopyReturns(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	writeRecord(t, first, recordJournal(10, false))
	cache := settledCache(RecordCacheLimits{Entries: 1})
	read(t, cache, first)
	cost := cache.Stats().Retained

	release := make(chan struct{})
	entered := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = cache.WithRecord(first, func(Record) error { close(entered); <-release; return nil })
	}()
	<-entered
	second := filepath.Join(dir, "second.jsonl")
	writeRecord(t, second, recordJournal(10, false))
	read(t, cache, second)
	if s := cache.Stats(); s.Entries != 1 || s.Retained != 2*cost {
		t.Fatalf("while the copy is out: %+v, want both counted", s)
	}
	close(release)
	<-finished
	if s := cache.Stats(); s.Retained != cost || s.Reserved != 0 {
		t.Fatalf("after: %+v, want the evicted one gone", s)
	}
}

// SHORT LINES EXPAND INTO ENTRY STRUCTURES, not just display strings. The
// estimate must cover the measured record so that an ordinary settled journal
// can actually be retained after its shared parse.
func TestTheReservationCoversShortLineRecords(t *testing.T) {
	for _, turns := range []int{40, 2000, 10000} {
		t.Run(strconv.Itoa(turns), func(t *testing.T) {
			journal := recordJournal(turns, false)
			path := filepath.Join(t.TempDir(), "node.jsonl")
			writeRecord(t, path, journal)
			record := ReadTranscript(path)
			estimate, cost := recordReserve(int64(len(journal))), recordCost(record)
			if estimate < cost {
				t.Fatalf("reservation %d does not cover measured record %d (%d entries)", estimate, cost, len(record.Entries))
			}
			cache := settledCache(RecordCacheLimits{})
			read(t, cache, path)
			if got := cache.Stats(); got.Retained != cost || got.Reserved != 0 || got.Entries != 1 {
				t.Fatalf("after parse: %+v, want retained cost %d", got, cost)
			}
			if got := read(t, cache, path); !reflect.DeepEqual(got, record) {
				t.Fatal("cached record differs from the full reading")
			}
			if got := cache.Stats(); got.Parses != 1 || got.Hits != 1 || got.Retained > cache.limits.Bytes || got.Reserved != 0 {
				t.Fatalf("after cache hit: %+v", got)
			}
		})
	}
}

// READINGS OF ONE UNCHANGED FILE SHARE ONE PARSE.
func TestTwoReadingsOfOneFileShareOneParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeRecord(t, path, recordJournal(2000, false))
	want := ReadTranscript(path)
	cache := settledCache(RecordCacheLimits{})
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			if got := read(t, cache, path); !reflect.DeepEqual(got, want) {
				t.Error("a shared reading differs")
			}
		}()
	}
	group.Wait()
	if s := cache.Stats(); s.Parses != 1 || s.Hits != 1 {
		t.Fatalf("stats = %+v, want one parse and one reading answered from it", s)
	}
}

// THE CACHE STAYS WITHIN ITS BOUNDS however many journals pass through it, and
// a journal refused for its size does not stop a smaller one being kept.
func TestTheCacheStaysBoundedAcrossManyJournals(t *testing.T) {
	dir := t.TempDir()
	size := int64(len(recordJournal(20, false)))
	cache := settledCache(RecordCacheLimits{Entries: 4, Bytes: 6 * recordReserve(size), FileBytes: size})
	for n := range 40 {
		path := filepath.Join(dir, "j"+strconv.Itoa(n)+".jsonl")
		writeRecord(t, path, recordJournal(20, false))
		read(t, cache, path)
		if s := cache.Stats(); s.Entries > 4 || s.Retained > 6*recordReserve(size) || s.Reserved != 0 {
			t.Fatalf("after %d journals: %+v", n+1, s)
		}
	}
	big := filepath.Join(dir, "big.jsonl")
	writeRecord(t, big, recordJournal(21, false))
	read(t, cache, big)
	read(t, cache, big)
	if s := cache.Stats(); s.Hits != 0 {
		t.Fatalf("a journal past FileBytes was kept: %+v", s)
	}
	writeRecord(t, big, recordJournal(20, false))
	read(t, cache, big)
	read(t, cache, big)
	if s := cache.Stats(); s.Hits != 1 {
		t.Fatalf("a refused journal kept a smaller one out: %+v", s)
	}
}

// A CALLBACK THAT EDITS ITS RECORD REACHES NOBODY ELSE'S, run under -race with
// readers on both sides.
func TestACallerEditingItsRecordCannotReachAnother(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeRecord(t, path, recordJournal(50, true))
	want := ReadTranscript(path)
	cache := settledCache(RecordCacheLimits{})
	read(t, cache, path)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				err := cache.WithRecord(path, func(record Record) error {
					if !reflect.DeepEqual(record, want) {
						t.Error("a reader saw another reader's edit")
					}
					for i := range record.Entries {
						record.Entries[i].Text = "edited"
					}
					record.Entries = append(record.Entries, DisplayEntry{Role: "user"})
					return nil
				})
				if err != nil && !errors.Is(err, ErrRecordReadBusy) {
					t.Error(err)
				}
			}
		}()
	}
	group.Wait()
}

// EVERY SLICE AND POINTER IN AN ENTRY IS COPIED, AND EVERY BYTE IS COUNTED. The
// law reads the entry's own fields, so a field added later is checked the day
// it lands.
func TestCloneAndCostCoverEveryFieldOfAnEntry(t *testing.T) {
	var entry DisplayEntry
	fill(reflect.ValueOf(&entry).Elem())
	record := Record{Entries: []DisplayEntry{entry}, Requests: RequestBooks{Recent: []RequestLine{{1, 2}}}}
	copied := cloneRecord(record)
	if !reflect.DeepEqual(copied, record) {
		t.Fatal("the copy differs")
	}
	aliased(t, "Entries", reflect.ValueOf(record.Entries), reflect.ValueOf(copied.Entries))
	aliased(t, "Requests.Recent", reflect.ValueOf(record.Requests.Recent), reflect.ValueOf(copied.Requests.Recent))
	original, clone := reflect.ValueOf(record.Entries[0]), reflect.ValueOf(copied.Entries[0])
	for i := range original.NumField() {
		aliased(t, original.Type().Field(i).Name, original.Field(i), clone.Field(i))
	}
	if got, want := entriesCost(record.Entries), deepCost(reflect.ValueOf(record.Entries)); got != want {
		t.Fatalf("entriesCost = %d, the fields hold %d", got, want)
	}
}

// R1: neither the first callback nor a cache hit may edit the kept process.
func TestRecordCacheToolProcessIsOwnedByEachCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeRecord(t, path, `{"type":"session","version":1,"id":"process-cache","cwd":"."}
{"type":"message","role":"user","content":"start the command"}
{"type":"message","role":"assistant","toolCalls":[{"id":"cached","function":{"name":"bash","arguments":"{\"command\":\"worker\"}"}}]}
{"type":"message","role":"tool","toolCallId":"cached","content":"running","presentation":{"audience":"tool","invoked":true,"tool_process":{"job":3,"started":"2026-10-20T09:00:00Z"}}}
`)
	want := ToolProcess{Job: 3, Started: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	cache := settledCache(RecordCacheLimits{})
	for range 2 {
		if err := cache.WithRecord(path, func(record Record) error {
			process := toolEntryFor(t, record.Entries, "cached").ToolProcess
			if process == nil || !reflect.DeepEqual(*process, want) {
				t.Fatalf("a callback inherited another callback's process: %+v", process)
			}
			process.Job = 99
			process.Started = time.Time{}
			process.Service = "edited-service"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if stats := cache.Stats(); stats.Parses != 1 || stats.Hits != 1 || stats.Entries != 1 {
		t.Fatalf("the second callback did not read a kept record: %+v", stats)
	}
}

// R1: copied process objects need a reservation in both history regions;
// their immutable service strings are retained once rather than copied.
func TestRecordVectorsReserveToolProcessCopies(t *testing.T) {
	bare := Record{Entries: []DisplayEntry{{}, {}}, Earlier: []DisplayEntry{{}}}
	record := cloneRecord(bare)
	record.Entries[1].ToolProcess = &ToolProcess{Job: 3}
	record.Earlier[0].ToolProcess = &ToolProcess{Service: "kept-service"}
	objects := 2 * int64(unsafe.Sizeof(ToolProcess{}))
	if got := recordVectors(record) - recordVectors(bare); got != objects {
		t.Fatalf("process copy reservation = %d, want %d", got, objects)
	}
	if got, want := recordCost(record)-recordCost(bare), objects+int64(len("kept-service")); got != want {
		t.Fatalf("retained process cost = %d, want %d", got, want)
	}
}

// Retained backing arrays count even when only one media item or mark is used.
func TestComposedCostCountsSpareCapacity(t *testing.T) {
	var entry DisplayEntry
	fill(reflect.ValueOf(&entry).Elem())
	completionTasks := make([]TaskReplyTag, 1, 4)
	copy(completionTasks, entry.CompletionTasks)
	entry.CompletionTasks = completionTasks
	mark := entry.Person.Composed
	media := make([]MessageMedia, 1, 7)
	copy(media, mark.Media)
	mark.Media = media
	marks := make([]AnnotationMark, 1, 5)
	copy(marks, mark.Annotation.Marks)
	mark.Annotation.Marks = marks
	points := make([]Point, 1, 11)
	copy(points, marks[0].Shade.Points)
	marks[0].Shade.Points = points
	entries := []DisplayEntry{entry}
	if got, want := entriesCost(entries), deepCost(reflect.ValueOf(entries)); got != want {
		t.Fatalf("entriesCost = %d, retained fields hold %d", got, want)
	}
}

// fill gives every string, slice and pointer in v something to hold.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("seven7")
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i))
			}
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Map:
		panic("a map in an entry: teach cloneEntries and entriesCost to copy and count it")
	}
}

// aliased fails when a slice or pointer of the copy shares the original's
// memory.
func aliased(t *testing.T, name string, a, b reflect.Value) {
	t.Helper()
	switch a.Kind() {
	case reflect.Slice:
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			t.Fatalf("%s is shared by the copy", name)
		}
	case reflect.Pointer:
		if !a.IsNil() && a.Pointer() == b.Pointer() {
			t.Fatalf("%s is shared by the copy", name)
		}
	case reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
		t.Fatalf("%s is a %s nothing copies", name, a.Kind())
	}
}

// deepCost is what v points at, counted the way recordCost counts it.
func deepCost(v reflect.Value) int64 {
	switch v.Kind() {
	case reflect.String:
		return int64(v.Len())
	case reflect.Slice:
		cost := int64(v.Cap()) * int64(v.Type().Elem().Size())
		for i := range v.Len() {
			cost += deepCost(v.Index(i))
		}
		return cost
	case reflect.Pointer:
		if v.IsNil() {
			return 0
		}
		return int64(v.Type().Elem().Size()) + deepCost(v.Elem())
	case reflect.Struct:
		var cost int64
		for i := range v.NumField() {
			cost += deepCost(v.Field(i))
		}
		return cost
	}
	return 0
}

// BenchmarkRecordCache is a journal of 10k and 100k entries read the plain way,
// which is also what every first open costs, and answered from a kept record.
// retained-B is what the cache counts for the record it keeps; file-B is the
// journal's size.
func BenchmarkRecordCache(b *testing.B) {
	for _, size := range []int{10_000, 100_000} {
		path := filepath.Join(b.TempDir(), "node.jsonl")
		text := recordJournal(size/5, false)
		writeRecord(b, path, text)
		name := strconv.Itoa(size/1000) + "k"
		b.Run(name+"/plain", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if got := ReadTranscript(path); len(got.Entries) != size {
					b.Fatalf("%d entries", len(got.Entries))
				}
			}
		})
		b.Run(name+"/kept", func(b *testing.B) {
			cache := settledCache(RecordCacheLimits{})
			read(b, cache, path)
			b.ReportAllocs()
			for b.Loop() {
				if got := read(b, cache, path); len(got.Entries) != size {
					b.Fatalf("%d entries", len(got.Entries))
				}
			}
			s := cache.Stats()
			if s.Parses != 1 {
				b.Fatalf("stats = %+v", s)
			}
			b.ReportMetric(float64(s.Retained), "retained-B")
			b.ReportMetric(float64(len(text)), "file-B")
		})
	}
}

// A KEPT RECORD THAT NOBODY ASKS FOR GOES BACK TO THE HEAP. Paging a long
// conversation wants the next page from the same parse, a few seconds later;
// nothing wants a journal's whole reading an hour after the last page, and
// eight of them held for the life of a desktop engine were most of its
// resident set. Reading it again later is one parse.
func TestAKeptRecordNobodyAsksForIsDroppedWhenItIdles(t *testing.T) {
	clock := time.Now().Add(time.Hour)
	dir := t.TempDir()
	cache := NewRecordCache(RecordCacheLimits{Settle: time.Nanosecond, Now: func() time.Time { return clock }})
	var paths []string
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		path := filepath.Join(dir, name)
		writeRecord(t, path, recordJournal(6, false))
		paths = append(paths, path)
		read(t, cache, path)
	}
	if s := cache.Stats(); s.Entries != 2 || s.Retained == 0 {
		t.Fatalf("stats = %+v, want both journals kept", s)
	}
	clock = clock.Add(recordCacheIdle / 2)
	read(t, cache, paths[0]) // a hit: this one is in use again
	clock = clock.Add(recordCacheIdle/2 + time.Second)
	cache.Expire()
	s := cache.Stats()
	if s.Entries != 1 {
		t.Fatalf("after the idle time %d records are kept, want only the one read since", s.Entries)
	}
	clock = clock.Add(recordCacheIdle)
	cache.Expire()
	if s := cache.Stats(); s.Entries != 0 || s.Retained != 0 {
		t.Fatalf("stats = %+v, want an empty cache once nothing was read for the idle time", s)
	}
	read(t, cache, paths[1])
	if s := cache.Stats(); s.Parses != 3 {
		t.Fatalf("parses = %d, want two first readings and the expired journal read afresh", s.Parses)
	}
}

func TestRecordExpirySchedulesTheEarliestRemainingDeadline(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.jsonl")
	second := filepath.Join(t.TempDir(), "second.jsonl")
	writeRecord(t, first, recordJournal(1, false))
	writeRecord(t, second, recordJournal(1, false))
	now := time.Now().Add(time.Hour)
	cache := NewRecordCache(RecordCacheLimits{Now: func() time.Time { return now }})
	var delay time.Duration
	var fire func()
	cache.after = func(d time.Duration, f func()) func() { delay, fire = d, f; return func() {} }
	read(t, cache, first)
	if delay != 30*time.Second {
		t.Fatalf("first deadline in %s, want 30s", delay)
	}
	now = now.Add(time.Second)
	read(t, cache, second)
	now = now.Add(29 * time.Second)
	fire()
	if got := cache.Stats().Entries; got != 1 {
		t.Fatalf("first deadline kept %d entries, want 1", got)
	}
	if delay != time.Second {
		t.Fatalf("next expiry in %s, want the remaining 1s", delay)
	}
	now = now.Add(time.Second)
	fire()
	if got := cache.Stats().Entries; got != 0 {
		t.Fatalf("second deadline kept %d entries, want 0", got)
	}
}
