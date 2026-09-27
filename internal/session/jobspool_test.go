package session

// The disk spool's bound (issue #1599): a job that prints without end fills at
// most jobSpoolChunks chunks on disk, keeps the newest tail, says what it has
// discarded, and never lets one Write grow a temporary to match it. Every
// limit here is set tiny and every failure injected — no test generates data
// anywhere near the production figures.
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newSpool builds a sink over a real temp file at test-sized limits.
func newSpool(t *testing.T, chunkBytes int64, chunks int) *jobSink {
	t.Helper()
	path := filepath.Join(t.TempDir(), "3.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	sink := newJobSink(file, path)
	sink.chunkBytes = chunkBytes
	if chunks != jobSpoolChunks {
		t.Fatal("fixture must use production chunk count")
	}
	t.Cleanup(func() { sink.close() })
	return sink
}

// spoolFiles lists the chunks the spool is holding, with their sizes.
func spoolFiles(t *testing.T, sink *jobSink) map[string]int64 {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(sink.base))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	files := map[string]int64{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", entry.Name(), err)
		}
		files[entry.Name()] = info.Size()
	}
	return files
}

// A job that prints far more than the window holds leaves exactly the chunk
// files the window promises, the newest data in them, and nothing else — and
// says once that the beginning is gone.
func TestJobSpoolRotatesAndStaysBounded(t *testing.T) {
	sink := newSpool(t, 1000, 2)
	var wrote []byte
	for index := 0; index < 120; index++ {
		line := []byte(fmt.Sprintf("line%-20d\n", index))
		wrote = append(wrote, line...)
		if _, err := sink.Write(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// One live chunk at or under its cap, one kept chunk, nothing else: the
	// disk holds the window and not the job's whole output.
	files := spoolFiles(t, sink)
	if len(files) != 2 {
		t.Fatalf("spool holds %d files (%v), want the live chunk and one kept", len(files), files)
	}
	for name, size := range files {
		if name != "3.log" && name != "3.log.1" {
			t.Fatalf("unexpected chunk %q (%v)", name, files)
		}
		if size > 1000 {
			t.Fatalf("chunk %s is %d bytes, over its 1000 cap", name, size)
		}
	}
	// The live chunk is the NEWEST data: its first line follows the rotated
	// data, and its last line is the last thing written.
	live, err := os.ReadFile(sink.base)
	if err != nil {
		t.Fatalf("read live chunk: %v", err)
	}
	if !strings.HasSuffix(string(wrote), string(live)) {
		t.Fatal("live chunk is not a suffix of what was written")
	}
	if got := sink.lastNonEmptyLine(); got != "line119" {
		t.Fatalf("ring lost the newest line: %q", got)
	}
	// The kept chunk holds the middle of the stream, not the beginning: the
	// first chunk (lines 0 to 39) was clobbered by the second rotation, which
	// is the discard the notice is about.
	kept, err := os.ReadFile(sink.base + ".1")
	if err != nil {
		t.Fatalf("read kept chunk: %v", err)
	}
	if !strings.Contains(string(kept), "line40") || strings.Contains(string(kept), "line20") {
		t.Fatal("kept chunk is not the second chunk; the discard never happened")
	}
	// And the notice says the truncation out loud, once.
	if notice := sink.notice(); !strings.Contains(notice, "log truncated") {
		t.Fatalf("a bounded spool that discarded output says nothing: %q", notice)
	}
}

// The ring stays a tail under writes far larger than it: one huge Write must
// not grow a temporary to match, and the newest bytes must still arrive.
func TestJobSinkHugeSingleWriteKeepsOnlyTheTail(t *testing.T) {
	sink := newSpool(t, 1<<20, 2)
	// Well over the 64KB ring, nowhere near huge: the point is the tail path,
	// not tonnage.
	huge := make([]byte, 256<<10)
	for index := range huge {
		huge[index] = byte('a' + index%26)
	}
	copy(huge[len(huge)-8:], "THE-TAIL")
	if _, err := sink.Write(huge); err != nil {
		t.Fatalf("write: %v", err)
	}
	sink.mu.Lock()
	size := len(sink.ring)
	sink.mu.Unlock()
	if size > jobRingBytes*2 {
		t.Fatalf("ring grew to %d bytes for one huge write", size)
	}
	if got := sink.lastNonEmptyLine(); !strings.HasSuffix(got, "THE-TAIL") {
		t.Fatalf("ring lost the tail of a huge write: %q", got)
	}
	if !strings.Contains(sink.text(), "THE-TAIL") {
		t.Fatal("text lost the tail")
	}
}

// A spool write that fails is recorded once, stops the retries, leaves the
// job's drain and ring alive, and is what the footer reports — never an error
// the job dies of.
func TestJobSpoolWriteFailureIsRecordedNotFatal(t *testing.T) {
	sink := newSpool(t, 1<<20, 2)
	calls := 0
	boom := errors.New("disk full")
	sink.hook = func(data []byte) (int, error) {
		calls++
		if calls == 3 {
			return 0, boom
		}
		return len(data), nil
	}
	for index := 0; index < 5; index++ {
		if _, err := sink.Write([]byte("hello\n")); err != nil {
			t.Fatalf("Write reported %v; the drain must survive a spool failure", err)
		}
	}
	if !sink.spoolBroken {
		t.Fatal("a failed spool write left the spool unbroken")
	}
	if calls != 3 {
		t.Fatalf("spool was attempted %d times after the failure stopped it", calls)
	}
	// The ring kept draining after the failure.
	if got := sink.lastNonEmptyLine(); got != "hello" {
		t.Fatalf("ring stopped draining after the spool failed: %q", got)
	}
	// The notice carries the failure, not a promise of a log.
	if notice := sink.notice(); !strings.Contains(notice, "disk full") {
		t.Fatalf("notice does not carry the failure: %q", notice)
	}
}

// A short write is the same recordable failure a failed write is.
func TestJobSpoolShortWriteIsRecorded(t *testing.T) {
	sink := newSpool(t, 1<<20, 2)
	sink.hook = func(data []byte) (int, error) {
		if len(data) >= 4 {
			return len(data) - 2, nil // two bytes short
		}
		return len(data), nil
	}
	if _, err := sink.Write([]byte("payload\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !sink.spoolBroken {
		t.Fatal("a short write left the spool healthy")
	}
	if notice := sink.notice(); !strings.Contains(notice, "short write") {
		t.Fatalf("notice does not carry the short write: %q", notice)
	}
}

// Closing the actual fd behind the sink induces a real close failure. It
// must remain visible even after the disk window has discarded older bytes.
func TestJobSpoolCloseErrorSurfacesInNotice(t *testing.T) {
	sink := newSpool(t, 4, 2)
	_, _ = sink.Write([]byte("0123456789"))
	if err := sink.file.Close(); err != nil {
		t.Fatal(err)
	}
	sink.close()
	first := sink.notice()
	if !strings.Contains(first, "close:") || !strings.Contains(first, "truncated") {
		t.Fatalf("close error hidden: %q", first)
	}
	sink.close()
	if sink.notice() != first {
		t.Fatal("repeated close changed first failure")
	}
}

func TestJobFooterNamesTruncationInsteadOfFullLog(t *testing.T) {
	var note string
	registry := newJobRegistry(t.TempDir(), Place{}, func(s string) { note = s })
	job, err := registry.newJob("fixture", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(job.sink.close)
	if err := registry.add(job); err != nil {
		t.Fatal(err)
	}
	job.sink.chunkBytes = 4
	_, _ = job.sink.Write([]byte("a\nb\nc\nd\ne\n"))
	registry.settleExit(job, 0)
	output, failed := registry.output(job.id, 10)
	if failed {
		t.Fatal(output)
	}
	for _, text := range []string{note, output} {
		if !strings.Contains(text, "log truncated") || strings.Contains(text, "full log:") || !strings.Contains(text, job.logPath+".1") {
			t.Fatalf("dishonest footer: %s", text)
		}
	}
}

func TestJobSpoolRotationPreservesIdentityAndNamesBothChunks(t *testing.T) {
	sink := newSpool(t, 4, 2)
	before, err := os.Stat(sink.base)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sink.Write([]byte("abcde"))
	after, err := os.Stat(sink.base)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("rotation replaced the live job identity")
	}
	footer := sink.logFooter(sink.base)
	if strings.Contains(footer, "full log:") || !strings.Contains(footer, sink.base+".1") {
		t.Fatalf("first rotation footer omitted retained chunk: %s", footer)
	}
	contender, err := os.OpenFile(sink.base, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if contender != nil {
		contender.Close()
		t.Fatal("a competing creator claimed the live ID")
	}
	if !os.IsExist(err) {
		t.Fatalf("competing claim error: %v", err)
	}
}

func TestJobSinkHugeWriteReplacesStalePrefix(t *testing.T) {
	sink := newSpool(t, 1<<20, 2)
	_, _ = sink.Write([]byte("OLD\n"))
	latest := strings.Repeat("new\n", jobRingBytes)
	_, _ = sink.Write([]byte(latest))
	if got, want := sink.text(), latest[len(latest)-jobRingBytes:]; got != want {
		t.Fatal("large write kept stale bytes before its newest suffix")
	}
}

func TestJobSpoolUnsafeBackupCannotTruncateAnotherFile(t *testing.T) {
	sink := newSpool(t, 4, 2)
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, sink.base+".1"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _ = sink.Write([]byte("abcdefghij"))
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("rotation changed another file: %q, %v", data, err)
	}
	if !strings.Contains(sink.notice(), "job log stopped") || sink.text() != "abcdefghij" {
		t.Fatal("unsafe backup must stop disk writes but keep draining")
	}
}

func TestJobSpoolEmptyCompletionStillReportsFailure(t *testing.T) {
	var note string
	registry := newJobRegistry(t.TempDir(), Place{}, func(s string) { note = s })
	job, err := registry.newJob("fixture", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(job.sink.close)
	job.sink.hook = func([]byte) (int, error) { return 0, errors.New("injected full disk") }
	_, _ = job.sink.Write([]byte("\n"))
	registry.settleExit(job, 0)
	if !strings.Contains(note, "injected full disk") || strings.Contains(note, "full log:") {
		t.Fatalf("blank output hid failure: %s", note)
	}
}

func TestJobSpoolConcurrentWritersAndCloseRemainBounded(t *testing.T) {
	sink := newSpool(t, 64, 2)
	var group sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 100; i++ {
				_, _ = sink.Write([]byte("stdout stderr\n"))
			}
			sink.close()
		}()
	}
	group.Wait()
	for _, size := range spoolFiles(t, sink) {
		if size > 64 {
			t.Fatalf("concurrent spool exceeded cap: %d", size)
		}
	}
	if sink.text() == "" {
		t.Fatal("concurrent close stopped memory drain")
	}
}
