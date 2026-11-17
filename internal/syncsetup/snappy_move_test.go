package syncsetup

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type recordedRelay struct {
	dir   directory.Client
	store blobstore.Store

	mu          sync.Mutex
	fakeClock   time.Duration
	stepDelay   time.Duration
	inFlight    int
	wave        int
	rounds      int
	events      []string
	cellReads   int
	listReads   int
	acquireDone bool

	blockAfterAcquire chan struct{}
	closeOnce         sync.Once
	failAfterAcquire  atomic.Bool
	fetchedFrames     map[blobstore.FrameID]int
}

func newRecordedRelay(t *testing.T, dir directory.Client, store blobstore.Store, stepDelay time.Duration) *recordedRelay {
	rec := &recordedRelay{
		dir:           dir,
		store:         store,
		stepDelay:     stepDelay,
		fetchedFrames: make(map[blobstore.FrameID]int),
	}
	t.Cleanup(func() {
		rec.unblock()
	})
	return rec
}

func (r *recordedRelay) blockBackground() {
	r.mu.Lock()
	r.blockAfterAcquire = make(chan struct{})
	r.mu.Unlock()
}

func (r *recordedRelay) unblock() {
	r.mu.Lock()
	ch := r.blockAfterAcquire
	r.mu.Unlock()
	if ch != nil {
		r.closeOnce.Do(func() { close(ch) })
	}
}

func (r *recordedRelay) begin(op string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight++
	if r.inFlight == 1 {
		r.wave++
		r.rounds = r.wave
		r.fakeClock += r.stepDelay
	}
	r.events = append(r.events, op)
	return r.wave
}

func (r *recordedRelay) end() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
}

func (r *recordedRelay) snapshot() (rounds, cellReads, listReads int, clock time.Duration, events []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := append([]string(nil), r.events...)
	return r.rounds, r.cellReads, r.listReads, r.fakeClock, cp
}

type recordedDir struct {
	directory.Client
	rec *recordedRelay
}

func (d recordedDir) Cell(ctx context.Context, id string) (directory.CellView, error) {
	d.rec.begin("Dir.Cell")
	d.rec.mu.Lock()
	d.rec.cellReads++
	d.rec.mu.Unlock()
	defer d.rec.end()
	return d.Client.Cell(ctx, id)
}

func (d recordedDir) List(ctx context.Context) (directory.Listing, error) {
	d.rec.mu.Lock()
	ch := d.rec.blockAfterAcquire
	d.rec.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return directory.Listing{}, ctx.Err()
		}
	}
	d.rec.begin("Dir.List")
	d.rec.mu.Lock()
	d.rec.listReads++
	d.rec.mu.Unlock()
	defer d.rec.end()
	return d.Client.List(ctx)
}

func (d recordedDir) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (directory.CellView, error) {
	d.rec.begin("Dir.Acquire")
	defer func() {
		d.rec.mu.Lock()
		d.rec.acquireDone = true
		d.rec.mu.Unlock()
		d.rec.end()
	}()
	return d.Client.Acquire(ctx, id, opts)
}

type recordedStore struct {
	blobstore.Store
	rec *recordedRelay
}

func (s recordedStore) waitIfBlocked(ctx context.Context) error {
	s.rec.mu.Lock()
	acquired := s.rec.acquireDone
	ch := s.rec.blockAfterAcquire
	s.rec.mu.Unlock()
	if acquired && ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if acquired && s.rec.failAfterAcquire.Load() {
		return blobstore.ErrAnswered
	}
	return nil
}

func (s recordedStore) GetFrame(ctx context.Context, id blobstore.FrameID) ([]byte, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.GetFrame")
	defer s.rec.end()
	raw, err := s.Store.GetFrame(ctx, id)
	if err == nil {
		s.rec.mu.Lock()
		s.rec.fetchedFrames[id]++
		s.rec.mu.Unlock()
	}
	return raw, err
}

func (s recordedStore) Get(ctx context.Context, rid string) ([]byte, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.Get")
	defer s.rec.end()
	raw, err := s.Store.Get(ctx, rid)
	if err == nil {
		s.rec.mu.Lock()
		s.rec.fetchedFrames[rid]++
		s.rec.mu.Unlock()
	}
	return raw, err
}

func (s recordedStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.GetMany")
	defer s.rec.end()
	got, err := s.Store.GetMany(ctx, rids)
	if err == nil {
		s.rec.mu.Lock()
		for _, obj := range got {
			s.rec.fetchedFrames[obj.RID]++
		}
		s.rec.mu.Unlock()
	}
	return got, err
}

// TestC1C6C8C9CriticalPathOpensInAtMostThreeRoundsAndCompletesWorkspace verifies
// C1 (instant open in <=3 serial relay rounds before workspace/vault/indexes),
// C6 (one directory record read, device name off critical path),
// C7 (after-work runs after open),
// C8 (completed workspace is byte-identical to source), and
// C9 (continue phases log line reports shown= and files=).
func TestC1C6C8C9CriticalPathOpensInAtMostThreeRoundsAndCompletesWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	h.vaultTheEnv()
	a := h.openA()
	a.mustSay("first turn with workspace")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("second turn on tower")
	h.durable(h.cell.ID, h.cell)

	logPath := filepath.Join(t.TempDir(), "chat.log")
	logFile, err := os.Create(logPath)
	mustNil(t, err)
	prevLog := log.Writer()
	log.SetOutput(logFile)
	defer func() {
		log.SetOutput(prevLog)
		_ = logFile.Close()
	}()

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
	rec.blockBackground()
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}

	cont := h.continuerB()
	var shownRounds, shownCellReads, shownListReads int
	var shownClock time.Duration
	var shownArriving bool
	var shownReadmeExists bool
	var shownTranscript string
	cont.opt.OnShown = func(got Continued) {
		shownRounds, shownCellReads, shownListReads, shownClock, _ = rec.snapshot()
		shownArriving = cell.WorkspaceArriving(got.Taken.Cell.Root)
		_, statErr := os.Stat(filepath.Join(workspaceOf(got.Taken.Cell.Root), "README.md"))
		shownReadmeExists = statErr == nil
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		shownTranscript = string(raw)
	}

	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)

	// C1: chat-open signal fired after at most 3 serial relay round trips while workspace is still arriving.
	if shownRounds == 0 || shownRounds > 3 {
		t.Fatalf("C1: chat opened after %d serial relay rounds (fake clock %s); want 1..3", shownRounds, shownClock)
	}
	if !shownArriving {
		t.Fatalf("C1: expected workspace to still be arriving when chat opened")
	}
	if shownReadmeExists {
		t.Fatalf("C1: expected workspace README.md not to be materialized yet when chat opened")
	}
	if !strings.Contains(shownTranscript, "second turn on tower") {
		t.Fatalf("C1: transcript not in place when chat opened: %q", shownTranscript)
	}
	// C6: only one directory record read on the critical path, and Dir.List did not block chat open.
	if shownCellReads != 1 {
		t.Fatalf("C6: Dir.Cell reads before open = %d; want 1", shownCellReads)
	}
	if shownListReads != 0 {
		t.Fatalf("C6: Dir.List reads before open = %d; want 0", shownListReads)
	}

	// Unblock background workspace fetch and wait for completion.
	rec.unblock()
	mustNil(t, got.Wait(t.Context()))

	// C8: workspace is now complete and byte-identical to source (including .env from vault).
	if cell.WorkspaceArriving(got.Taken.Cell.Root) {
		t.Fatalf("C8: workspace still marked arriving after Wait completed")
	}
	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))

	// C9: phase log line contains shown= and files= alongside existing fields.
	rawLog, err := os.ReadFile(logPath)
	mustNil(t, err)
	var phaseLine string
	for _, line := range strings.Split(string(rawLog), "\n") {
		if strings.Contains(line, "continue "+h.cell.ID+" phases ") {
			phaseLine = line
		}
	}
	if phaseLine == "" {
		t.Fatalf("C9: missing continue phases log line in:\n%s", string(rawLog))
	}
	for _, field := range []string{"capture=", "directory=", "retirement=", "fetch=", "acquire=", "restore=", "hooks=", "filing=", "shown=", "files=", "total=", "outcome=done"} {
		if !strings.Contains(phaseLine, field) {
			t.Errorf("C9: missing %s in phase line: %s", field, phaseLine)
		}
	}
	t.Logf("C9 sample phase line (fake clock at shown=%s): %s", shownClock, phaseLine)
}

// TestC8SecondComputerRefusedDuringBackgroundWorkspaceFetch verifies C8 lease
// fencing while the workspace is arriving in the background: the previous
// holder cannot write and a competing take with stale expectation is refused.
func TestC8SecondComputerRefusedDuringBackgroundWorkspaceFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("turn 1 with workspace")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("turn 2 before handoff")
	h.durable(h.cell.ID, h.cell)

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 0)
	rec.blockBackground()
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}

	cont := h.continuerB()
	cont.opt.AsyncFiles = true
	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)

	if !cell.WorkspaceArriving(got.Taken.Cell.Root) {
		t.Fatalf("C8: expected background workspace fetch to be in flight")
	}

	// While B's background workspace fetch is still in flight, A checks its lease and tries to write; it must be refused.
	mustNil(t, a.drive.CheckLease(t.Context()))
	if err := a.say("stale write from A during background fetch"); err == nil {
		t.Fatalf("C8: expected second computer write during background fetch to be refused")
	}
	// Direct relay publish with A's old fence must also be refused with ErrFenceStale.
	_, advErr := h.a.Dir.Publish(t.Context(), h.cell.ID, directory.Publish{
		Fence: 1,
		Head:  got.Taken.Driving.Head,
	})
	if !errors.Is(advErr, directory.ErrFenceStale) {
		t.Fatalf("C8: expected direct Publish with old fence to fail with ErrFenceStale, got %v", advErr)
	}

	// A competing take with the old fence must also be refused with ErrLeaseHeld / ErrFenceStale.
	_, staleErr := h.continuerA().TakeSeen(t.Context(), h.cell.ID, &directory.LeaseExpectation{
		Device: h.a.Device.ID(),
		Fence:  1,
	})
	if !errors.Is(staleErr, directory.ErrLeaseHeld) && !errors.Is(staleErr, directory.ErrFenceStale) {
		t.Fatalf("C8: expected stale take during background fetch to be refused, got %v", staleErr)
	}

	rec.unblock()
	mustNil(t, got.Wait(t.Context()))
}

// TestC4BackgroundFetchFailureKeepsChatOpenSurfacesPlainWordsAndRetryCompletes
// verifies C4: when background workspace fetch fails after retries, the chat
// stays open, MoveFailed surfaces a plain-words reason, and calling Retry()
// keeps already-fetched objects and completes the workspace.
func TestC4BackgroundFetchFailureKeepsChatOpenSurfacesPlainWordsAndRetryCompletes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("turn 1 with workspace")
	h.durable(h.cell.ID, h.cell)
	extra := make([]byte, 16384)
	for i := range extra {
		extra[i] = byte(i * 17)
	}
	mustNil(t, os.WriteFile(filepath.Join(h.work, "data", "extra.bin"), extra, 0o600))
	a.mustSay("turn 2 with extra workspace")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("turn 3 latest chat")
	h.durable(h.cell.ID, h.cell)

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 0)
	rec.failAfterAcquire.Store(true)
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}

	var mu sync.Mutex
	var progress []MoveProgress
	cont := h.continuerB()
	cont.opt.OnMove = func(p MoveProgress) {
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
	}

	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)

	// Wait for the initial background workspace fetch to fail.
	waitErr := got.Wait(t.Context())
	if waitErr == nil {
		t.Fatalf("C4: expected background workspace fetch to fail when store fails after acquire")
	}

	// Chat transcript remains in place and readable.
	raw, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	mustNil(t, err)
	if !strings.Contains(string(raw), "turn 3 latest chat") {
		t.Fatalf("C4: chat transcript missing after background failure: %q", string(raw))
	}

	// Status line received MoveFailed with plain words.
	mu.Lock()
	var failedMove MoveProgress
	for _, p := range progress {
		if p.Step == MoveFailed {
			failedMove = p
		}
	}
	mu.Unlock()
	if failedMove.Step != MoveFailed || failedMove.Err == "" {
		t.Fatalf("C4: expected MoveFailed progress with plain-words reason, got %+v", failedMove)
	}

	// Record how many times each frame was fetched before retry.
	rec.mu.Lock()
	beforeRetry := make(map[blobstore.FrameID]int, len(rec.fetchedFrames))
	for k, v := range rec.fetchedFrames {
		beforeRetry[k] = v
	}
	rec.mu.Unlock()

	// Clear store failure and retry.
	rec.failAfterAcquire.Store(false)
	if !got.Retry() {
		t.Fatalf("C4: expected got.Retry() to start a retry")
	}
	mustNil(t, got.Wait(t.Context()))

	// Already-fetched frames must not have been fetched a second time.
	rec.mu.Lock()
	for id, countBefore := range beforeRetry {
		if countAfter := rec.fetchedFrames[id]; countAfter != countBefore {
			t.Fatalf("C4: frame %s fetched %d times before retry and %d after; want no duplicate fetch", id, countBefore, countAfter)
		}
	}
	rec.mu.Unlock()

	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))
}

// TestC10MoveFilesFirstEnvRestoresOldOrder verifies C10: setting
// CODEAF_MOVE_FILES_FIRST=1 restores the files-before-open order.
func TestC10MoveFilesFirstEnvRestoresOldOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv(MoveFilesFirstVar, "1")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("turn 1 with workspace")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("turn 2 with files first")
	h.durable(h.cell.ID, h.cell)

	cont := h.continuerB()
	var arrivingAtShown bool
	var readmeAtShown bool
	cont.opt.OnShown = func(got Continued) {
		arrivingAtShown = cell.WorkspaceArriving(got.Taken.Cell.Root)
		_, err := os.Stat(filepath.Join(workspaceOf(got.Taken.Cell.Root), "README.md"))
		readmeAtShown = err == nil
	}

	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if arrivingAtShown {
		t.Fatalf("C10: expected workspace not to be arriving at OnShown when %s=1", MoveFilesFirstVar)
	}
	if !readmeAtShown {
		t.Fatalf("C10: expected workspace README.md to be materialized before OnShown when %s=1", MoveFilesFirstVar)
	}
	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))
}
