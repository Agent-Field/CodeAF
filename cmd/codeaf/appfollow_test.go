package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appFollowReadCounter struct {
	appFollowFile
	bytes      int
	reads      int
	beforeRead func()
}

func (c *appFollowReadCounter) Read(p []byte) (int, error) {
	if c.beforeRead != nil {
		before := c.beforeRead
		c.beforeRead = nil
		before()
	}
	c.reads++
	n, err := c.appFollowFile.Read(p)
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
	counter := &appFollowReadCounter{}
	reader.openFile = func(path string) (appFollowFile, error) {
		file, err := os.Open(path)
		counter.appFollowFile = file
		return counter, err
	}
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
	synctest.Test(t, func(t *testing.T) {
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
	})
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

// Contract P1: an idle follow permits replacement and retention removal on
// Windows too; Linux also observes descriptor ownership between reads.
func TestAppFollowNoHandleBetweenTicks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := session.JobNotice{ID: 3, State: session.JobDone, LogPath: path, Started: time.Now()}
	backend, rec, agent := workBackend(t, &fakeConversation{jobs: []session.JobNotice{job}})
	id := appProcessID(agent.ID, job)
	if err := backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.FollowProcess(id, false) }()
	noHandle := func() {
		t.Helper()
		if runtime.GOOS != "linux" {
			return
		}
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			target, _ := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if target == path || target == path+" (deleted)" {
				t.Fatalf("follow retained spool descriptor %s between ticks", entry.Name())
			}
		}
	}
	noHandle()
	replacement := path + ".new"
	if err := os.WriteFile(replacement, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	waitLine := func(line string) {
		t.Helper()
		rec.waitFor(t, 6*time.Second, func(event string, data any) bool {
			out, ok := data.(appwire.ProcessOutput)
			return event == appwire.EventProcessOutput && ok && out.ID == id && reflect.DeepEqual(out.Lines, []string{line})
		})
		noHandle()
	}
	waitLine("new")
	// Create the next inode before removing the old one so the filesystem
	// cannot reuse its identity between two observations.
	if err := os.WriteFile(replacement, []byte("recreated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	waitLine("recreated")
	appendFollowText(t, path, "appended\n")
	waitLine("appended")
	if err := backend.FollowProcess(id, false); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var lines []string
	for i, event := range rec.events {
		if event == appwire.EventProcessOutput {
			lines = append(lines, rec.data[i].(appwire.ProcessOutput).Lines...)
		}
	}
	if !reflect.DeepEqual(lines, []string{"new", "recreated", "appended"}) {
		t.Fatalf("follow output = %q", lines)
	}
}

// followClock starts the ordinary scheduler with real spool IO and fake time.
// Process updates use the same lane as the public follow's live conversation.
func followClock(t *testing.T, state session.JobState, text string) (*appBackend, *appRecorder, string, string, *int, *fakeConversation, session.JobNotice) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	job := session.JobNotice{ID: 3, State: state, LogPath: path, Started: time.Now()}
	conv := &fakeConversation{jobs: []session.JobNotice{job}}
	backend, rec, agent := workBackend(t, conv)
	id := appProcessID(agent.ID, job)
	stats := new(int)
	reader := &appFollowReader{stat: func(path string) (os.FileInfo, error) {
		*stats++
		return os.Stat(path)
	}}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	f := &appFollow{stop: make(chan struct{}), done: make(chan struct{})}
	backend.locked(func() { backend.follows[id] = f })
	go backend.follow(id, path, reader, f)
	conv.lane(session.Event{Kind: session.EventJobUpdate, Job: &job})
	synctest.Wait()
	return backend, rec, id, path, stats, conv, job
}

func followClockAdvance(t *testing.T, stats *int, delay time.Duration, want int) {
	t.Helper()
	before := *stats
	time.Sleep(delay)
	synctest.Wait()
	if got := *stats - before; got != want {
		t.Fatalf("%v: %d path stats, want %d", delay, got, want)
	}
}

func followClockLines(rec *appRecorder) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var lines []string
	for i, event := range rec.events {
		if event == appwire.EventProcessOutput {
			lines = append(lines, rec.data[i].(appwire.ProcessOutput).Lines...)
		}
	}
	return lines
}

// Contract P2: ended/drained output reaches 12 stats/minute, then wakes for
// late flushes and equal-size, equal-mtime replacements without duplication.
func TestAppFollowEndedBackoffAndLateGenerations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend, rec, id, path, stats, _, _ := followClock(t, session.JobDone, "existing\n")
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, 500*time.Millisecond, 0)
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, 5*time.Second, 1)
		followClockAdvance(t, stats, time.Minute, 12)

		appendFollowText(t, path, "late\n")
		followClockAdvance(t, stats, 5*time.Second, 1)
		if got := followClockLines(rec); !reflect.DeepEqual(got, []string{"late"}) {
			t.Fatalf("late flush = %q", got)
		}
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Second, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, 5*time.Second, 1)

		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		other := path + ".new"
		if err := os.WriteFile(other, []byte("replacement!!\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(other, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(other, path); err != nil {
			t.Fatal(err)
		}
		followClockAdvance(t, stats, 5*time.Second, 1)
		appendFollowText(t, path, "next\n")
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Second, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, 5*time.Second, 1)
		if got := followClockLines(rec); !reflect.DeepEqual(got, []string{"late", "replacement!!", "next"}) {
			t.Fatalf("generations = %q", got)
		}
		if err := backend.FollowProcess(id, false); err != nil {
			t.Fatal(err)
		}
		followClockAdvance(t, stats, time.Minute, 0)
	})
}

// Contract P2: running/unknown processes keep 500 ms even after a quiet minute;
// a known ended update enables backoff without another registry read.
func TestAppFollowRunningAndUnknownStayFastUntilEnded(t *testing.T) {
	for _, state := range []session.JobState{session.JobRunning, ""} {
		t.Run(string(state), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				_, _, _, _, stats, conv, job := followClock(t, state, "old\n")
				followClockAdvance(t, stats, time.Minute, 120)
				job.State = session.JobStopped
				conv.lane(session.Event{Kind: session.EventJobUpdate, Job: &job})
				synctest.Wait()
				followClockAdvance(t, stats, 500*time.Millisecond, 1)
				followClockAdvance(t, stats, time.Second, 1)
				followClockAdvance(t, stats, 2*time.Second, 1)
				followClockAdvance(t, stats, time.Minute, 12)
			})
		})
	}
}

// Contract P2: partial suffixes and unread batches stay on the fast schedule;
// only a fully drained ended generation can rest.
func TestAppFollowPartialAndUnreadBatchStayFast(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, _, path, stats, _, _ := followClock(t, session.JobFailed, "partial")
		followClockAdvance(t, stats, time.Minute, 120)
		appendFollowText(t, path, "\n"+strings.Repeat("line\n", 2000))
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		if got := followClockLines(rec); len(got) != 2000 || got[0] != "partial" {
			t.Fatalf("first batch: %d lines", len(got))
		}
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		if got := followClockLines(rec); len(got) != 2001 || got[2000] != "line" {
			t.Fatalf("drained batch: %d lines", len(got))
		}
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Second, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, time.Minute, 12)
	})
}

// Contract P2: an empty replacement resets the fast schedule even without a
// complete line; a missing spool also stays fast until its generation returns.
func TestAppFollowEndedEmptyReplacementAndMissingStayFast(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, _, path, stats, _, _ := followClock(t, session.JobDone, "old\n")
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Second, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, 5*time.Second, 1)
		other := path + ".new"
		if err := os.WriteFile(other, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(other, path); err != nil {
			t.Fatal(err)
		}
		followClockAdvance(t, stats, 5*time.Second, 1)
		appendFollowText(t, path, "part")
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Minute, 120)
		appendFollowText(t, path, "ial\n")
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		followClockAdvance(t, stats, time.Second, 1)
		followClockAdvance(t, stats, 2*time.Second, 1)
		followClockAdvance(t, stats, 5*time.Second, 1)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		followClockAdvance(t, stats, 5*time.Second, 1)
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		if err := os.WriteFile(path, []byte("returned\n"), 0600); err != nil {
			t.Fatal(err)
		}
		followClockAdvance(t, stats, 500*time.Millisecond, 1)
		if got := followClockLines(rec); !reflect.DeepEqual(got, []string{"partial", "returned"}) {
			t.Fatalf("replacement and recreation = %q", got)
		}
	})
}

// Contract P1: truncation below consumed progress resets even when a concurrent
// append made that progress newer than the last metadata snapshot.
func TestAppFollowTruncationAfterConcurrentAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	reader := &appFollowReader{}
	if err := reader.start(path); err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	counter := &appFollowReadCounter{beforeRead: func() {
		appendFollowText(t, path, "a concurrent append\n")
	}}
	reader.openFile = func(path string) (appFollowFile, error) {
		file, err := os.Open(path)
		counter.appFollowFile = file
		return counter, err
	}
	appendFollowText(t, path, "a\n")
	lines, err := reader.read(path, appOutputLines)
	if err != nil || !reflect.DeepEqual(lines, []string{"a", "a concurrent append"}) {
		t.Fatalf("concurrent append = %q, %v", lines, err)
	}
	if err := os.WriteFile(path, []byte("new\nfile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lines, err = reader.read(path, appOutputLines)
	if err != nil || !reflect.DeepEqual(lines, []string{"new", "file"}) {
		t.Fatalf("truncation after append = %q, %v", lines, err)
	}
}
