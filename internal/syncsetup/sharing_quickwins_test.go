package syncsetup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// savedHomeChat saves a chat with its own folder under the home's projects, as
// a chat made on this computer is: one sealed turn holding size random bytes.
func savedHomeChat(t *testing.T, r *driveRig, parent, title string, size int) cell.Cell {
	t.Helper()
	return savedChatAt(t, r, parent, "", title, size)
}

// savedChatAt is savedHomeChat whose folder is moved to root first, as an
// older chat's copy is made at cell.CopyRoot of its original.
func savedChatAt(t *testing.T, r *driveRig, parent, root, title string, size int) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(parent, cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	if root != "" {
		mustNil(t, os.Rename(c.Root, root))
		c, err = cell.OpenAt(root, filepath.Base(root))
		mustNil(t, err)
	}
	work := filepath.Join(c.Root, "work")
	mustNil(t, os.MkdirAll(work, 0700))
	payload := make([]byte, size)
	_, _ = rand.Read(payload)
	mustNil(t, os.WriteFile(filepath.Join(work, "data.bin"), payload, 0600))
	mustNil(t, session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: title, Workspace: work, Owned: true, Created: time.Now()}))
	appendTo(t, transcriptOf(c), `{"type":"session","version":3,"id":"`+c.ID+`"}`+"\n")
	e := cellstore.EngineFor(work)
	e.DataRoot, e.Binary, e.Transport = filepath.Join(r.a.Home, "v3", "stores"), r.bin, cellstore.Spawn{Binary: r.bin}
	_, err = e.Seal(t.Context(), c, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(t.Context(), c))
	return c
}

func projectsOf(s *Sync) string { return filepath.Join(s.Home, "v3", "projects", "local") }

// slowStore answers every frame and holdings question after delay.
type slowStore struct {
	blobstore.Store
	delay time.Duration
}

func (s slowStore) wait(ctx context.Context) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s slowStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if err := s.wait(ctx); err != nil {
		return "", err
	}
	return s.Store.PutFrame(ctx, frame)
}

func (s slowStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return s.Store.Has(ctx, rids)
}

func shortIdle(t *testing.T, d time.Duration) {
	t.Helper()
	was := PublishIdleWithin
	PublishIdleWithin = d
	t.Cleanup(func() { PublishIdleWithin = was })
}

func listed(t *testing.T, s *Sync, id string) bool {
	t.Helper()
	_, err := s.Dir.Cell(t.Context(), id)
	if err != nil && !errors.Is(err, directory.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// TestABackgroundPublishLastsAsLongAsItMakesProgress: the budget of a
// background publish is time without progress, not time in all. An upload whose
// every answer comes within the budget finishes although it takes longer than
// the budget; a store that stops answering ends the attempt after one budget.
func TestABackgroundPublishLastsAsLongAsItMakesProgress(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	shortIdle(t, 400*time.Millisecond)
	slow := savedHomeChat(t, r, projectsOf(s), "slow but moving", 2<<20)
	real := s.Store
	s.Store = slowStore{Store: real, delay: 250 * time.Millisecond}
	began := time.Now()
	mustNil(t, s.PublishLocalChats(t.Context(), time.Now, nil))
	if took := time.Since(began); took <= PublishIdleWithin {
		t.Fatalf("fixture took %s, not longer than the %s budget", took, PublishIdleWithin)
	}
	if !listed(t, s, slow.ID) {
		t.Fatal("a chat whose upload kept moving was not shared")
	}

	stuck := savedHomeChat(t, r, projectsOf(s), "stuck", 1<<10)
	s.Store = slowStore{Store: real, delay: time.Minute}
	began = time.Now()
	err := s.PublishLocalChats(t.Context(), time.Now, nil)
	if !errors.Is(err, errPublishStalled) {
		t.Fatalf("a stuck upload ended with %v, want no progress", err)
	}
	if took := time.Since(began); took > 10*PublishIdleWithin {
		t.Fatalf("a stuck upload took %s to end", took)
	}
	if listed(t, s, stuck.ID) {
		t.Fatal("a stuck upload was listed")
	}
}

// TestAChatSavedAfterTheFirstPassIsSharedByTheNext: the pass once scanned the
// disk only when it first ran, so a chat restored or written by another process
// later waited for the next launch.
func TestAChatSavedAfterTheFirstPassIsSharedByTheNext(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	first := savedHomeChat(t, r, projectsOf(s), "first", 1<<10)
	mustNil(t, s.PublishLocalChats(t.Context(), time.Now, nil))
	if !listed(t, s, first.ID) {
		t.Fatal("first chat not shared")
	}
	later := savedHomeChat(t, r, projectsOf(s), "later", 1<<10)
	mustNil(t, s.PublishLocalChats(t.Context(), time.Now, nil))
	if !listed(t, s, later.ID) {
		t.Fatal("a chat saved after the first pass was not shared by the next one")
	}
}

// TestAFinishedOlderChatCopyIsSharedByThePass: the copy of an older chat lives
// in its project's `.moving` folder, outside the folders the pass scanned. The
// terminal published it itself, so with only the app running it was never
// shared. The pass now finds it and marks it offered; the copy of a chat that
// works in the home folder stays on this computer and is not counted.
func TestAFinishedOlderChatCopyIsSharedByThePass(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	person := t.TempDir()
	t.Setenv("HOME", person)
	olderCopy := func(name, workspace string) cell.Cell {
		original := filepath.Join(projectsOf(s), name)
		mustNil(t, os.MkdirAll(original, 0700))
		mustNil(t, os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(`{"type":"session","version":1,"id":"`+name+`"}`+"\n"), 0600))
		mustNil(t, session.SaveMeta(original, session.Meta{ID: name, Title: name, Workspace: workspace, LastUserAt: time.Now()}))
		moving := filepath.Join(projectsOf(s), ".moving")
		mustNil(t, os.MkdirAll(moving, 0700))
		c := savedChatAt(t, r, moving, cell.CopyRoot(original), name, 1<<10)
		mustNil(t, cell.StampCopyReady(c.Root))
		return c
	}
	copied := olderCopy("older-chat", t.TempDir())
	inHome := olderCopy("home-chat", person)
	if u, err := s.CountUnshared(); err != nil || u.Waiting != 1 {
		t.Fatalf("unshared before the pass = %+v, %v; want one waiting", u, err)
	}
	mustNil(t, s.PublishLocalChats(t.Context(), time.Now, nil))
	if !listed(t, s, copied.ID) {
		t.Fatal("the finished copy of an older chat was not shared")
	}
	if listed(t, s, inHome.ID) {
		t.Fatal("the copy of an older chat in the home folder was shared")
	}
	if _, err := os.Stat(filepath.Join(copied.Root, filepath.FromSlash(cell.CopyOfferedPath))); err != nil {
		t.Fatalf("shared copy not marked offered: %v", err)
	}
	if u, err := s.CountUnshared(); err != nil || u.Total() != 0 {
		t.Fatalf("unshared after the pass = %+v, %v", u, err)
	}
}

// TestUnsharedCountsWaitingAndFailingChats: a chat with nothing saved is not
// counted, a saved one waits, one whose upload failed is failing, and a shared
// one is not counted.
func TestUnsharedCountsWaitingAndFailingChats(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	empty, err := cell.CreateIn(projectsOf(s), cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	_ = empty
	savedHomeChat(t, r, projectsOf(s), "one", 1<<10)
	if u, err := s.CountUnshared(); err != nil || u != (Unshared{Waiting: 1}) {
		t.Fatalf("before any pass = %+v, %v", u, err)
	}
	block := &blocker{Store: s.Store}
	block.set(true)
	s.Store = block
	if err := s.PublishLocalChats(t.Context(), time.Now, nil); err == nil {
		t.Fatal("expected the upload to fail")
	}
	if u, err := s.CountUnshared(); err != nil || u != (Unshared{Failing: 1}) {
		t.Fatalf("after a failed pass = %+v, %v", u, err)
	}
	block.set(false)
	later := func() time.Time { return time.Now().Add(time.Hour) }
	mustNil(t, s.PublishLocalChats(t.Context(), later, nil))
	if u, err := s.CountUnshared(); err != nil || u.Total() != 0 {
		t.Fatalf("after sharing = %+v, %v", u, err)
	}
}

// TestPublishingLogsOneLinePerChatAndOutcome: each attempt's duration and
// bytes are logged, once per chat and outcome, so a stalled connection reads
// differently from a slow one and a retry loop does not flood the log.
func TestPublishingLogsOneLinePerChatAndOutcome(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	c := savedHomeChat(t, r, projectsOf(s), "logged", 1<<10)
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	block := &blocker{Store: s.Store}
	block.set(true)
	s.Store = block
	later := time.Now()
	clock := func() time.Time { return later }
	for range 3 {
		later = later.Add(time.Hour)
		_ = s.PublishLocalChats(t.Context(), clock, nil)
	}
	block.set(false)
	later = later.Add(time.Hour)
	mustNil(t, s.PublishLocalChats(t.Context(), clock, nil))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var failed, shared int
	for _, line := range lines {
		if !strings.Contains(line, "sharing chat "+c.ID) {
			continue
		}
		if !strings.Contains(line, " bytes sent") || !strings.Contains(line, " after ") {
			t.Fatalf("log line lacks duration or bytes: %q", line)
		}
		switch {
		case strings.Contains(line, ": failed: "):
			failed++
		case strings.Contains(line, ": shared after "):
			shared++
		}
	}
	if failed != 1 || shared != 1 {
		t.Fatalf("log has %d failure and %d shared lines, want one each:\n%s", failed, shared, out.String())
	}
}

// TestPendingChatsGoUntriedNewestFirstThenSmallest: one large chat must not
// hold the others back on every pass, and the chat used last comes first.
func TestPendingChatsGoUntriedNewestFirstThenSmallest(t *testing.T) {
	now := time.Now()
	s := &Sync{publication: map[string]*pendingPublication{
		"old":        {changed: now.Add(-time.Hour)},
		"new":        {changed: now},
		"big-tried":  {changed: now, size: 3 << 30, err: errors.New("no progress")},
		"small-try":  {changed: now.Add(-2 * time.Hour), size: 1 << 20, err: errors.New("refused")},
		"middle-new": {changed: now.Add(-time.Minute)},
	}}
	got := strings.Join(s.publicationOrder(), " ")
	if want := "new middle-new old small-try big-tried"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}
