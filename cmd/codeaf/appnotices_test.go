package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// noticeAt is episode e of service s in agent a's chat.
func noticeAt(a string, s, e int) appServiceNotice {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	local := "s" + itoa(s)
	return appServiceNotice{Agent: a, Ref: a + ":" + local, Episode: local + "." + itoa(e), Text: "web is not answering", At: base.Add(time.Duration(s*1000+e) * time.Second)}
}

// readBack is every notice id the record gives, oldest first.
func readBack(t *testing.T, l *appNoticeLog, agent string) []string {
	t.Helper()
	r, err := l.Reader(agent)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		return nil
	}
	defer r.Close()
	var ids []string
	for {
		n, ok := r.Prev()
		if !ok {
			break
		}
		ids = append([]string{n.id()}, ids...)
	}
	return ids
}

func mustAppend(t *testing.T, l *appNoticeLog, n appServiceNotice) {
	t.Helper()
	if err := l.Append(n); err != nil {
		t.Fatal(err)
	}
}

func countOf(ids []string, id string) int {
	c := 0
	for _, have := range ids {
		if have == id {
			c++
		}
	}
	return c
}

// TestAHugeIndexIsNeverReadWhole: a sparse gigabyte where the index should
// be is not loaded — the first question reads at most a few kilobytes of it,
// throws it away, and counts the record from its start instead.
func TestAHugeIndexIsNeverReadWhole(t *testing.T) {
	dir := t.TempDir()
	l := newAppNoticeLog(dir)
	mustAppend(t, l, noticeAt("a1", 1, 1))
	_, indexPath, _ := l.paths("a1")
	f, err := os.Create(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	again := newAppNoticeLog(dir)
	known := again.Noticed(noticeAt("a1", 1, 1))
	runtime.ReadMemStats(&after)
	if !known {
		t.Fatal("a broken index hid the episode the record holds")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("one question allocated %d bytes over a 1GiB index", grew)
	}
	mustAppend(t, again, noticeAt("a1", 1, 1))
	if ids := readBack(t, again, "a1"); len(ids) != 1 {
		t.Fatalf("the record holds %v", ids)
	}
}

// TestAFIFOIsNeverOpened: a FIFO at the record's or the index's path is
// refused at once, not waited on.
func TestAFIFOIsNeverOpened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs")
	}
	dir := t.TempDir()
	l := newAppNoticeLog(dir)
	record, indexPath, _ := l.paths("a1")
	if err := syscall.Mkfifo(record, 0o600); err != nil {
		t.Skip(err)
	}
	if err := syscall.Mkfifo(indexPath, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() {
		if l.Noticed(noticeAt("a1", 1, 1)) {
			done <- errors.New("a FIFO said noticed")
			return
		}
		if err := l.Append(noticeAt("a1", 1, 1)); err == nil {
			done <- errors.New("a FIFO took a notice")
			return
		}
		if _, err := l.Reader("a1"); err == nil {
			done <- errors.New("a FIFO was read")
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO was waited on")
	}
}

// TestAnUnfinishedLineIsEndedNotCut: a crash in the middle of a write leaves
// a fragment after the last newline. The next notice starts a line of its
// own, every finished notice before it is still read back, and a fragment that
// was a whole notice short only its newline is kept and not written twice.
func TestAnUnfinishedLineIsEndedNotCut(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fragment func(raw string) string
	}{
		{"half a line", func(raw string) string { return raw[:len(raw)/2] }},
		{"all but its newline", func(raw string) string { return raw }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			l := newAppNoticeLog(dir)
			for e := 1; e <= 3; e++ {
				mustAppend(t, l, noticeAt("a1", 1, e))
			}
			record, _, _ := l.paths("a1")
			raw, _ := os.ReadFile(record)
			lines := strings.SplitAfter(string(raw), "\n")
			crashed := noticeAt("a1", 2, 1)
			whole := `{"agent":"a1","ref":"a1:s2","episode":"s2.1","text":"web is not answering","at":"` + crashed.At.Format(time.RFC3339Nano) + `"}`
			f, _ := os.OpenFile(record, os.O_WRONLY|os.O_APPEND, 0)
			_, _ = f.WriteString(tc.fragment(whole))
			_ = f.Close()

			// A question does not write; the append that follows ends the
			// fragment, and counts it when it is a whole notice.
			again := newAppNoticeLog(dir)
			if again.Noticed(crashed) {
				t.Fatal("an unfinished line is known as noticed")
			}
			mustAppend(t, again, crashed)
			mustAppend(t, again, noticeAt("a1", 3, 1))
			ids := readBack(t, newAppNoticeLog(dir), "a1")
			want := []string{noticeAt("a1", 1, 1).id(), noticeAt("a1", 1, 2).id(), noticeAt("a1", 1, 3).id(), crashed.id(), noticeAt("a1", 3, 1).id()}
			if strings.Join(ids, " ") != strings.Join(want, " ") {
				t.Fatalf("read back\n %v\nwant\n %v", ids, want)
			}
			after, _ := os.ReadFile(record)
			if !strings.HasPrefix(string(after), strings.Join(lines, "")+tc.fragment(whole)+"\n") {
				t.Fatalf("the record's earlier bytes changed:\n%s", after)
			}
		})
	}
}

// TestAFailedWriteTakesBackOnlyItsOwnBytes: a write that fails part-way
// leaves the record as it was, says so, and the next try keeps the notice once.
func TestAFailedWriteTakesBackOnlyItsOwnBytes(t *testing.T) {
	dir := t.TempDir()
	l := newAppNoticeLog(dir)
	mustAppend(t, l, noticeAt("a1", 1, 1))
	record, _, _ := l.paths("a1")
	before, _ := os.ReadFile(record)
	real := appNoticeWrite
	t.Cleanup(func() { appNoticeWrite = real })
	appNoticeWrite = func(f *os.File, line []byte) (int, error) {
		n, _ := f.Write(line[:len(line)/2])
		return n, errors.New("disk full")
	}
	if err := l.Append(noticeAt("a1", 1, 2)); err == nil {
		t.Fatal("a failed write was called kept")
	}
	if now, _ := os.ReadFile(record); string(now) != string(before) {
		t.Fatalf("a failed write left\n%s", now)
	}
	if l.Noticed(noticeAt("a1", 1, 2)) {
		t.Fatal("a failed write is known as noticed")
	}
	appNoticeWrite = real
	mustAppend(t, l, noticeAt("a1", 1, 2))
	mustAppend(t, l, noticeAt("a1", 1, 2))
	ids := readBack(t, newAppNoticeLog(dir), "a1")
	if len(ids) != 2 || ids[1] != noticeAt("a1", 1, 2).id() {
		t.Fatalf("read back %v", ids)
	}
}

// TestAQuietOldEpisodeIsNotWrittenTwiceAfterAStaleIndex: a service's episode
// noticed long ago and still open, then eight hundred other notices (far past
// any tail a reading might have stopped at), then a relaunch with an index
// that is behind, missing, or lying about where the record ends. The old
// episode is still known, and its notice is never written a second time.
func TestAQuietOldEpisodeIsNotWrittenTwiceAfterAStaleIndex(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index func(path string, stale []byte)
	}{
		{"behind", func(path string, stale []byte) { _ = os.WriteFile(path, stale, 0o600) }},
		{"missing", func(path string, _ []byte) { _ = os.Remove(path) }},
		{"ends mid-line", func(path string, _ []byte) {
			_ = os.WriteFile(path, []byte(`{"v":1,"offset":7,"newest":{}}`), 0o600)
		}},
		{"not an index", func(path string, _ []byte) { _ = os.WriteFile(path, []byte(`{"s1":99}`), 0o600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			l := newAppNoticeLog(dir)
			quiet := noticeAt("a1", 1, 1)
			mustAppend(t, l, quiet)
			_, indexPath, _ := l.paths("a1")
			stale, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			for e := 1; e <= 800; e++ {
				mustAppend(t, l, noticeAt("a1", 2, e))
			}
			record, _, _ := l.paths("a1")
			if info, _ := os.Stat(record); info.Size() <= appNoticeChunk {
				t.Fatalf("the record is only %d bytes", info.Size())
			}
			tc.index(indexPath, stale)

			again := newAppNoticeLog(dir)
			if !again.Noticed(quiet) {
				t.Fatal("the quiet old episode is not known after the relaunch")
			}
			mustAppend(t, again, quiet)
			mustAppend(t, again, noticeAt("a1", 2, 800))
			ids := readBack(t, again, "a1")
			if len(ids) != 801 || countOf(ids, quiet.id()) != 1 || ids[0] != quiet.id() {
				t.Fatalf("read back %d notices, the quiet one %d times, oldest %s", len(ids), countOf(ids, quiet.id()), ids[0])
			}
		})
	}
}

// TestALineTheIndexMissedIsCounted: the line reached the record but its index
// never did — a crash, or a sync that failed after the write — and the next
// look counts it rather than writing it again.
func TestALineTheIndexMissedIsCounted(t *testing.T) {
	dir := t.TempDir()
	l := newAppNoticeLog(dir)
	mustAppend(t, l, noticeAt("a1", 1, 1))
	record, _, _ := l.paths("a1")
	f, _ := os.OpenFile(record, os.O_WRONLY|os.O_APPEND, 0)
	_, _ = f.WriteString(`{"agent":"a1","ref":"a1:s1","episode":"s1.2","text":"down","at":"2026-10-01T00:00:00Z"}` + "\n")
	_ = f.Close()
	// The same log, holding an index that has not seen the line.
	if !l.Noticed(noticeAt("a1", 1, 2)) {
		t.Fatal("a line past the index is not counted")
	}
	mustAppend(t, l, noticeAt("a1", 1, 2))
	if ids := readBack(t, l, "a1"); len(ids) != 2 {
		t.Fatalf("read back %v", ids)
	}
}

// TestWhatIsHeldStaysBounded: two hundred services in one chat and forty
// chats keep the held indexes within their bounds, and the index file small,
// while the record keeps every notice and a service let go of long ago is
// still known — before and after a relaunch.
func TestWhatIsHeldStaysBounded(t *testing.T) {
	dir := t.TempDir()
	l := newAppNoticeLog(dir)
	for s := 1; s <= 200; s++ {
		mustAppend(t, l, noticeAt("a1", s, 1))
	}
	for a := 0; a < 40; a++ {
		mustAppend(t, l, noticeAt("b"+itoa(a), 1, 1))
	}
	if len(l.owners) > appNoticeOwners {
		t.Fatalf("%d indexes held", len(l.owners))
	}
	for agent, ix := range l.owners {
		if len(ix.Newest) > appNoticeServices {
			t.Fatalf("%s holds %d services", agent, len(ix.Newest))
		}
	}
	_, indexPath, _ := l.paths("a1")
	if info, err := os.Stat(indexPath); err != nil || info.Size() > 4<<10 {
		t.Fatalf("the index: %v, %v", info, err)
	}
	for _, log := range []*appNoticeLog{l, newAppNoticeLog(dir)} {
		for _, s := range []int{1, 100, 200} {
			n := noticeAt("a1", s, 1)
			if !log.Noticed(n) {
				t.Fatalf("s%d is not known", s)
			}
			mustAppend(t, log, n)
		}
		if log.Noticed(noticeAt("a1", 1, 2)) || log.Noticed(noticeAt("a1", 201, 1)) {
			t.Fatal("an episode never written is known")
		}
		if ids := readBack(t, log, "a1"); len(ids) != 200 || ids[0] != noticeAt("a1", 1, 1).id() || ids[199] != noticeAt("a1", 200, 1).id() {
			t.Fatalf("read back %d notices", len(ids))
		}
	}
	for a := 0; a < 40; a++ {
		if ids := readBack(t, l, "b"+itoa(a)); len(ids) != 1 {
			t.Fatalf("b%d holds %v", a, ids)
		}
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "*.index.json.*")); len(entries) > 0 {
		t.Fatalf("index temporaries left behind: %v", entries)
	}
}

// BenchmarkAColdCountOfTenThousandNotices is the price of a first question
// about a chat whose index is gone: the whole record, ten thousand notices
// over forty services, counted one line at a time.
func BenchmarkAColdCountOfTenThousandNotices(b *testing.B) {
	dir := b.TempDir()
	l := newAppNoticeLog(dir)
	record, indexPath, _ := l.paths("a1")
	var sb strings.Builder
	for i := 1; i <= 10000; i++ {
		raw, _ := json.Marshal(noticeAt("a1", i%40+1, i))
		sb.Write(append(raw, '\n'))
	}
	if err := os.WriteFile(record, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(sb.Len()))
	b.ReportAllocs()
	for b.Loop() {
		_ = os.Remove(indexPath)
		if !newAppNoticeLog(dir).Noticed(noticeAt("a1", 1, 40)) {
			b.Fatal("not counted")
		}
	}
}
