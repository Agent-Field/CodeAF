package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// These hooks pause the engine's real conversation door, without timing races.
type restConversation struct {
	fakeConversation
	readTasks func() ([]session.AppTaskView, error)
	retry     func(uint64) error
}

func (c *restConversation) AppTasks() ([]session.AppTaskView, error) {
	if c.readTasks != nil {
		return c.readTasks()
	}
	return c.fakeConversation.AppTasks()
}
func (c *restConversation) RetryTask(id uint64) error {
	if c.retry != nil {
		return c.retry(id)
	}
	return c.fakeConversation.RetryTask(id)
}

func restBackend(t *testing.T, c *restConversation) (*appBackend, *appAgent) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: transcript, Assembled: true, Release: func() {}}, nil
	})
	b.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { b.locked(func() { b.crewRun = nil }); b.Close() })
	view, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a, err := b.find(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	b.letGo(a)
	b.locked(func() { b.crewRun = &appCrewRun{held: map[string][]string{view.ID: {"crew"}}} })
	return b, a
}

func TestAppRestReleaseCoordinatesWithRetry(t *testing.T) {
	c := &restConversation{}
	b, a := restBackend(t, c)
	var running atomic.Bool
	c.retry = func(uint64) error {
		if a.gate.TryLock() {
			a.gate.Unlock()
			t.Error("retry admitted outside the release gate")
		}
		running.Store(true)
		return nil
	}
	var once sync.Once
	c.readTasks = func() ([]session.AppTaskView, error) {
		old := !running.Load()
		once.Do(func() {
			if err := b.RetryTask(a.view.ID + ":1"); err != nil {
				t.Error(err)
			}
		})
		state := session.TaskRunning
		if old {
			state = session.TaskFailed
		}
		return []session.AppTaskView{{ID: "1", State: string(state)}}, nil
	}
	if _, err := b.ListTasks(a.view.ID); err != nil {
		t.Fatal(err)
	}
	if b.liveOf(a) == nil {
		t.Fatal("listing cancelled newly retried background work")
	}
}

func TestAppRestConcurrentReadersKeepOwnerOverlay(t *testing.T) {
	c := &restConversation{}
	b, a := restBackend(t, c)
	first, second, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c.readTasks = func() ([]session.AppTaskView, error) {
		switch calls.Add(1) {
		case 1:
			close(first)
			<-second
		case 2:
			close(second)
			<-finish
		}
		return []session.AppTaskView{{ID: "1", State: string(session.TaskDone), Step: 7}}, nil
	}
	type answer struct {
		tasks []appwire.Task
		err   error
	}
	results := make(chan answer, 2)
	go func() { tasks, err := b.ListTasks(a.view.ID); results <- answer{tasks, err} }()
	<-first
	go func() { tasks, err := b.ListTasks(a.view.ID); results <- answer{tasks, err} }()
	<-second
	one := <-results
	if b.liveOf(a) == nil {
		t.Error("first reader released the second reader's conversation")
	}
	close(finish)
	two := <-results
	for _, got := range []answer{one, two} {
		if got.err != nil || len(got.tasks) != 1 || got.tasks[0].Steps == nil || *got.tasks[0].Steps != 7 {
			t.Errorf("owner overlay lost: %+v, %v", got.tasks, got.err)
		}
	}
	if b.liveOf(a) != nil {
		t.Error("last reader did not release the idle conversation")
	}
}

func TestAppRestDoesNotCertifyWritesDuringRead(t *testing.T) {
	c := &restConversation{}
	b, a := restBackend(t, c)
	path := filepath.Join(filepath.Dir(a.transcript), "tasks.json")
	var calls atomic.Int32
	c.readTasks = func() ([]session.AppTaskView, error) {
		n := calls.Add(1)
		if n == 1 {
			if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		title := "new"
		if n == 1 {
			title = "old"
		}
		return []session.AppTaskView{{ID: "1", Title: title, State: string(session.TaskDone)}}, nil
	}
	if _, err := b.ListTasks(a.view.ID); err != nil {
		t.Fatal(err)
	}
	b.restMemo = appRestMemo{} // Relaunch must not revive the stale answer either.
	got, err := b.ListTasks(a.view.ID)
	if err != nil || len(got) != 1 || got[0].Title != "new" {
		t.Fatalf("write during read certified stale rows: %+v %v", got, err)
	}
}

func TestAppRestStampDetectsSameSizeReplacement(t *testing.T) {
	b, a := restBackend(t, &restConversation{})
	path := filepath.Join(filepath.Dir(a.transcript), "tasks.json")
	old := time.Now().Add(-time.Hour)
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "old")
	before := b.restStamp(a)
	write(path+".new", "new")
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if after := b.restStamp(a); after == before {
		t.Fatal("same-size replacement with restored mtime left cache valid")
	}
	before = b.restStamp(a)
	write(path, "now")
	if after := b.restStamp(a); after == before {
		t.Fatal("in-place write with restored mtime left cache valid")
	}
}

func TestAppRestDoesNotCacheRecentFiles(t *testing.T) {
	c := &restConversation{}
	b, a := restBackend(t, c)
	path := filepath.Join(filepath.Dir(a.transcript), "tasks.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	now := info.ModTime().Add(time.Second)
	b.now = func() time.Time { return now }
	var calls atomic.Int32
	c.readTasks = func() ([]session.AppTaskView, error) { calls.Add(1); return nil, nil }
	for range 2 {
		if _, err := b.ListTasks(a.view.ID); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() < 2 {
		t.Fatal("file inside timestamp resolution was cached")
	}
}

func TestAppRestBudgetCountsRetainedRows(t *testing.T) {
	tasks := make([]appwire.Task, appRestMemoBytes/int(unsafe.Sizeof(appwire.Task{}))+1)
	var m appRestMemo
	m.put("agent", "stamp", tasks)
	if _, ok := m.get("agent", "stamp"); ok {
		t.Fatal("memory cache admitted more than 24 MiB of task structs")
	}
}

func TestAppRestRejectedMemoryEntryIsNeverWritten(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	tasks := make([]appwire.Task, appRestMemoBytes/int(unsafe.Sizeof(appwire.Task{}))+1)
	appRestWrite("agent", "stamp", tasks)
	if _, err := os.Stat(appRestPath("agent")); !os.IsNotExist(err) {
		t.Fatalf("memory-rejected list written to disk: %v", err)
	}
}

func TestAppRestDropsOversizedAndCorruptDiskFiles(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	path := appRestPath("agent")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"oversized", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "oversized" {
				err = f.Truncate(64 << 20)
			} else {
				_, err = f.WriteString("!" + strings.Repeat(" ", 8<<20))
			}
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, ok := appRestRead("agent", "stamp")
			runtime.ReadMemStats(&after)
			if ok {
				t.Fatal("invalid disk file admitted")
			}
			if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
				t.Errorf("invalid disk file allocated %d bytes before rejection", n)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Error("invalid disk file was not dropped")
			}
		})
	}
}

func TestAppRestDiskReadWaitsForAdmission(t *testing.T) {
	b, a := restBackend(t, &restConversation{})
	b.restSlots = make(chan struct{})
	appRestWrite(a.view.ID, b.restStamp(a), []appwire.Task{{ID: "cached", Title: "cached"}})
	done := make(chan struct{})
	go func() { _, _ = b.ListTasks(a.view.ID); close(done) }()
	select {
	case <-done:
		t.Fatal("disk decoding bypassed read admission")
	case <-b.restSlots: // The reader has reached admission, before decoding.
	}
	b.restSlots <- struct{}{} // Return the reservation on the reader's exit.
	<-done
}

func TestAppRestRealIdleOpenKeepsTheMemoryWin(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	var opens atomic.Int32
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		engine, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: path, Model: "test/model", APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
		if err != nil {
			return nil, err
		}
		opens.Add(1)
		c := &restConversation{readTasks: engine.AppTasks}
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: path, Assembled: true, Release: func() { _ = engine.Close() }}, nil
	})
	b.mint = func(string) (string, error) { return path, nil }
	b.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { b.locked(func() { b.crewRun = nil }); b.Close() })
	view, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a, err := b.find(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	b.letGo(a)
	b.locked(func() { b.crewRun = &appCrewRun{held: map[string][]string{view.ID: {"crew"}}} })
	before := opens.Load()
	stamp := b.restStamp(a)
	if _, err := b.ListTasks(view.ID); err != nil {
		t.Fatal(err)
	}
	if b.liveOf(a) != nil {
		t.Fatal("real idle session remained held")
	}
	b.restMemo = appRestMemo{}
	if _, err := b.ListTasks(view.ID); err != nil {
		t.Fatal(err)
	}
	if n := opens.Load() - before; n != 1 {
		t.Fatalf("real session opened %d times across relaunch, want 1; snapshot stable=%v", n, stamp == b.restStamp(a))
	}
}

func TestAppRestDiskDecodeBoundsCompactRows(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	path := appRestPath("agent")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(appRestFile{Build: buildinfo.Identity(), Stamp: "stamp"})
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(header), `"tasks":null`, `"tasks":[`+strings.Repeat(`{},`, 200000)+`{}]`, 1)
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, ok := appRestRead("agent", "stamp")
	runtime.ReadMemStats(&after)
	if ok {
		t.Fatal("compact disk rows bypassed the retained-memory budget")
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 4*appRestMemoBytes {
		t.Fatalf("compact rows allocated %d bytes, want under %d", n, 4*appRestMemoBytes)
	}
}

func TestAppRestExplicitCloseDrainsReadersOutsideAdmission(t *testing.T) {
	b, a := restBackend(t, &restConversation{})
	_, _, endRead, err := b.borrowLive(a.view.ID)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{})
	b.waitReaders = func(done <-chan struct{}) { close(waiting); <-done }
	closed := make(chan struct{})
	go func() { b.letGo(a); close(closed) }()
	<-waiting
	if a.gate.TryLock() {
		a.gate.Unlock()
	} else {
		t.Error("close held admission while waiting for a reader that may need it")
	}
	if b.liveOf(a) == nil {
		t.Error("close released an active reader's conversation")
	}
	endRead()
	<-closed
	if b.liveOf(a) != nil {
		t.Fatal("close failed to release after the last reader returned")
	}
}

func TestAppRestOwnerReadFailureKeepsConversation(t *testing.T) {
	c := &restConversation{}
	b, a := restBackend(t, c)
	var reads atomic.Int32
	c.readTasks = func() ([]session.AppTaskView, error) {
		if reads.Add(1) > 1 {
			return nil, os.ErrPermission
		}
		return []session.AppTaskView{{ID: "1", State: "done"}}, nil
	}
	if _, err := b.ListTasks(a.view.ID); err != nil {
		t.Fatal(err)
	}
	if b.liveOf(a) == nil {
		t.Fatal("failed live-work revalidation was treated as idle")
	}
}

func TestAppRestCellStampIncludesRootPlanAndChildRecords(t *testing.T) {
	b, a := restBackend(t, &restConversation{})
	root := t.TempDir()
	truth := filepath.Join(root, ".cell")
	if err := os.MkdirAll(filepath.Join(truth, "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	b.locked(func() { a.transcript = filepath.Join(truth, "transcript.jsonl") })
	for _, path := range []string{filepath.Join(root, "plandb.db"), filepath.Join(root, "presence.json"), filepath.Join(truth, "session.json"), filepath.Join(truth, "tasks", "node.jsonl")} {
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		before := b.restStamp(a)
		if before == "" {
			t.Fatal("settled cell fixture was not stampable")
		}
		if err := os.WriteFile(path+".new", []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".new", path); err != nil {
			t.Fatal(err)
		}
		if b.restStamp(a) == before {
			t.Errorf("cell cache missed change to %s", path)
		}
	}
}

func TestAppRestCellReadsPresenceFromSessionRoot(t *testing.T) {
	b, a := restBackend(t, &restConversation{})
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	b.locked(func() { a.transcript = filepath.Join(root, ".cell", "transcript.jsonl") })
	if err := os.WriteFile(filepath.Join(root, "presence.json"), []byte(`{"schema":1,"state":"working"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if b.memberRests(a) {
		t.Fatal("cell member with working root presence was admitted as resting")
	}
}
