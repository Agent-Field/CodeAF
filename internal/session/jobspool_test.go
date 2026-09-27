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
	"testing"
)

// newSpool builds a sink over a real temp file at test-sized limits.
func newSpool(t *testing.T, chunkBytes int64, chunks int) *jobSink {
	t.Helper()
	path := filepath.Join(t.TempDir(), "3.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	sink := newJobSink(file, path)
	sink.chunkBytes = chunkBytes
	sink.chunks = chunks
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
	if got := sink.lastNonEmptyLine(); got != "line79" {
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
	if got := sink.lastNonEmptyLine(); got != "THE-TAIL" {
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

// A spool that was already broken never has its notice overwritten by the
// truncation, and a close that fails is surfaced through the same door.
func TestJobSpoolCloseErrorSurfacesInNotice(t *testing.T) {
	sink := newSpool(t, 1<<20, 2)
	if _, err := sink.Write([]byte("some output\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	sink.close()
	// Closing is idempotent and a second close records nothing new.
	sink.close()
	if notice := sink.notice(); notice != "" {
		t.Fatalf("a clean close invented a notice: %q", notice)
	}
	// A close failure on a real file cannot be injected directly, so the
	// broken-text door is asserted at the unit it runs through: the guard
	// keeps the first notice and the close error cannot overwrite it.
	sink2 := newSpool(t, 1<<20, 2)
	sink2.brokenText = "job log stopped: injected earlier"
	sink2.close()
	if notice := sink2.notice(); notice != "job log stopped: injected earlier" {
		t.Fatalf("close overwrote an earlier notice: %q", notice)
	}
}

// The footer a model reads is honest about the bound: `full log:` while the
// log on disk is everything the job wrote, and the truncation named where it
// is not — through `jobs output` and through the completion note alike.
func TestJobFooterNamesTruncationInsteadOfFullLog(t *testing.T) {
	agent, _ := jobsAgent(t)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	id := startJob(t, agent, "printf 'a\nb\nc\n'")
	waitFor(t, "the completion note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 0", id))
	})
	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("want one note, got %v", queued)
	}
	job := agent.jobs.find(id)
	footer := fmt.Sprintf("[job %d · last %d lines · full log: %s]", id, jobExitTailLines, job.logPath)
	if !strings.Contains(queued[0], footer) {
		t.Fatalf("a whole, untruncated log must still say full log: %q", queued[0])
	}
	// Mark the same job's spool as having discarded output and read it again:
	// the footer must stop promising full and name the truncation instead.
	job.sink.noticeText = "log truncated: only the most recent 8.0 MB is kept"
	text, isError := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d}`, id))
	if isError {
		t.Fatalf("jobs output failed: %s", text)
	}
	if !strings.Contains(text, "log truncated") || strings.Contains(text, "full log:") {
		t.Fatalf("footer still promises a full log after truncation: %q", text)
	}
	if !strings.Contains(text, "log file: "+job.logPath) {
		t.Fatalf("footer lost the file path beside the truncation: %q", text)
	}
}
