package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appFollowReadCounter struct {
	io.Reader
	bytes int
	reads int
}

func (c *appFollowReadCounter) Read(p []byte) (int, error) {
	c.reads++
	n, err := c.Reader.Read(p)
	c.bytes += n
	return n, err
}

func appendFollowText(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(text)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

// Contract B: once a 10 MB spool is drained, EOF checks read no bytes and an
// append reads only new bytes. Existing partial lines survive the first flush.
func TestAppFollowBDrainedTenMBStopsReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte(strings.Repeat("old line\n", 1_250_000)+"partial"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &appFollowReader{}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	counter := &appFollowReadCounter{Reader: reader.file}
	reader.reader = bufio.NewReaderSize(counter, 64<<10)
	for range 100 {
		lines, err := reader.read(path, appOutputLines)
		if err != nil || len(lines) != 0 {
			t.Fatalf("drained read = %q, %v", lines, err)
		}
	}
	if counter.bytes != 0 || counter.reads != 0 {
		t.Fatalf("drained EOF: %d bytes in %d reads", counter.bytes, counter.reads)
	}
	text := " finishes\r\nsecond\n"
	appendFollowText(t, path, text)
	lines, err := reader.read(path, appOutputLines)
	if err != nil || !reflect.DeepEqual(lines, []string{"partial finishes", "second"}) {
		t.Fatalf("append = %q, %v", lines, err)
	}
	if counter.bytes != len(text) {
		t.Fatalf("append read %d bytes, want %d", counter.bytes, len(text))
	}
	before := counter.reads
	for range 100 {
		if _, err := reader.read(path, appOutputLines); err != nil {
			t.Fatal(err)
		}
	}
	if counter.reads != before {
		t.Fatal("ended, drained EOF kept reading")
	}
}

// Contract B: replacement, truncation and missing files start a new generation,
// while batches, CRLF, long lines and unfinished suffixes lose/duplicate no line.
func TestAppFollowBSpoolChangesDeliverEachLineOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &appFollowReader{}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	read := func(want []string) {
		t.Helper()
		got, err := reader.read(path, 2)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("read = %q, %v; want %q", got, err, want)
		}
	}
	appendFollowText(t, path, "a\nb\nc\nunfinished")
	read([]string{"a", "b"})
	read([]string{"c"})
	read(nil)
	appendFollowText(t, path, " done\r\n")
	read([]string{"unfinished done"})
	read(nil)
	// Same-size replacement must be noticed even with the old mtime restored.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := strings.Repeat("x", int(info.Size())-1) + "\n"
	other := path + ".new"
	if err := os.WriteFile(other, []byte(replacement), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	read([]string{strings.TrimSuffix(replacement, "\n")})
	if err := os.WriteFile(path, []byte("cut\n"), 0600); err != nil {
		t.Fatal(err)
	}
	read([]string{"cut"})
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := before.ModTime().Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	read([]string{"new"})
	appendFollowText(t, path, "discard this partial")
	read(nil)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	read(nil)
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	read([]string{"new"})
	long := strings.Repeat("z", 150<<10)
	appendFollowText(t, path, long)
	read(nil)
	read(nil)
	appendFollowText(t, path, "\n\n")
	read([]string{long, ""})
	read(nil)
}

// Contract B: an ended process remains followed at EOF, catches late flushes,
// and closes its reader promptly when explicitly unfollowed.
func TestAppFollowBEndedLateFlushAndStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte("existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := session.JobNotice{ID: 3, State: session.JobDone, LogPath: path, Started: time.Now()}
	backend, rec, agent := workBackend(t, &fakeConversation{jobs: []session.JobNotice{job}})
	id := appProcessID(agent.ID, job)
	if err := backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * appFollowEvery)
	appendFollowText(t, path, "late\n")
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		out, ok := data.(appwire.ProcessOutput)
		return ok && out.ID == id && reflect.DeepEqual(out.Lines, []string{"late"})
	})
	if err := backend.FollowProcess(id, false); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	outputs := 0
	for i, event := range rec.events {
		if event == appwire.EventProcessOutput {
			outputs++
			if !reflect.DeepEqual(rec.data[i].(appwire.ProcessOutput).Lines, []string{"late"}) {
				t.Fatal("old or duplicate lines emitted")
			}
		}
	}
	if outputs != 1 {
		t.Fatalf("got %d output events, want 1", outputs)
	}
}

// Contract B: public output still uses complete-line cursors and its original
// line cap, independently of the byte cursor used by follow.
func TestAppFollowBPublicLineCursorUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte("zero\r\none\ntwo\npartial"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, limit, next int
		lines             []string
	}{
		{0, 2, 2, []string{"zero", "one"}},
		{2, 2, 3, []string{"two"}},
		{3, 2, 3, []string{}},
		{20, 2, 20, []string{}},
	} {
		lines, next, err := appReadLines(path, tc.from, tc.limit)
		if err != nil || next != tc.next || !reflect.DeepEqual(lines, tc.lines) {
			t.Fatalf("cursor %d = %q, %d, %v", tc.from, lines, next, err)
		}
	}
}

// Contract B: truncation also resets unread buffered lines when the new size is
// still beyond the delivered byte offset.
func TestAppFollowBTruncationDiscardsBufferedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	reader := &appFollowReader{}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	appendFollowText(t, path, "a\nb\nstale buffered lines\n")
	lines, err := reader.read(path, 2)
	if err != nil || !reflect.DeepEqual(lines, []string{"a", "b"}) {
		t.Fatalf("first batch = %q, %v", lines, err)
	}
	if err := os.WriteFile(path, []byte("new\nfile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lines, err = reader.read(path, 2)
	if err != nil || !reflect.DeepEqual(lines, []string{"new", "file"}) {
		t.Fatalf("truncated batch = %q, %v", lines, err)
	}
}

// Contract B: the production batch limit applies to following too, and a
// credential split between writes is redacted only after its line completes.
func TestAppFollowBBatchLimitAndSplitSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	reader := &appFollowReader{}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	appendFollowText(t, path, strings.Repeat("line\n", appOutputLines+1)+"gho_")
	lines, err := reader.read(path, appOutputLines)
	if err != nil || len(lines) != appOutputLines {
		t.Fatalf("first batch has %d lines, %v", len(lines), err)
	}
	lines, err = reader.read(path, appOutputLines)
	if err != nil || !reflect.DeepEqual(lines, []string{"line"}) {
		t.Fatalf("second batch = %q, %v", lines, err)
	}
	appendFollowText(t, path, strings.Repeat("x", 36)+"\n")
	lines, err = reader.read(path, appOutputLines)
	if err != nil || !reflect.DeepEqual(lines, []string{"[redacted token · gho_…]"}) {
		t.Fatalf("split secret = %q, %v", lines, err)
	}
	lines, err = reader.read(path, appOutputLines)
	if err != nil || len(lines) != 0 {
		t.Fatal("batch boundary duplicated a line")
	}
}

// Contract B: the real output door retains its initial 512 KB tail marker and
// line cursor, and backend close finishes a follower and its deferred cleanup.
func TestAppFollowBPublicTailAndBackendClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	line := strings.Repeat("a", 63)
	total := appOutputKeep/64 + 64
	if err := os.WriteFile(path, []byte(strings.Repeat(line+"\n", total)+"partial"), 0600); err != nil {
		t.Fatal(err)
	}
	job := session.JobNotice{ID: 3, State: session.JobDone, LogPath: path, Started: time.Now()}
	backend, _, agent := workBackend(t, &fakeConversation{jobs: []session.JobNotice{job}})
	id := appProcessID(agent.ID, job)
	lines, next, err := backend.ProcessOutput(id, 0)
	if err != nil || len(lines) != appOutputLines+1 || lines[0] != appTrimmedLine || next != 65+appOutputLines {
		t.Fatalf("initial tail: %d lines, cursor %d, %v", len(lines), next, err)
	}
	for _, got := range lines[1:] {
		if got != line {
			t.Fatal("initial tail changed a line")
		}
	}
	lines, continued, err := backend.ProcessOutput(id, next)
	if err != nil || len(lines) != appOutputLines || continued != next+appOutputLines || lines[0] != line {
		t.Fatal("public line cursor did not continue the tail")
	}
	lines, next, err = backend.ProcessOutput(id, total)
	if err != nil || len(lines) != 0 || next != total {
		t.Fatal("public output returned an unfinished line")
	}
	appendFollowText(t, path, "\n")
	lines, next, err = backend.ProcessOutput(id, next)
	if err != nil || !reflect.DeepEqual(lines, []string{"partial"}) || next != total+1 {
		t.Fatal("public cursor lost a completed partial line")
	}
	if err := backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	var follow *appFollow
	backend.locked(func() { follow = backend.follows[id] })
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-follow.done:
	case <-time.After(time.Second):
		t.Fatal("backend close left a follower or its resources running")
	}
}
