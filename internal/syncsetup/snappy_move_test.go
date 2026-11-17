package syncsetup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
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
	time.Sleep(2 * time.Millisecond)
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
	acquired := d.rec.acquireDone
	ch := d.rec.blockAfterAcquire
	d.rec.mu.Unlock()
	if acquired && ch != nil {
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

func (s recordedStore) GetFrameBounded(ctx context.Context, id blobstore.FrameID, maxEncoded int64) ([]byte, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.GetFrameBounded")
	defer s.rec.end()
	var raw []byte
	var err error
	if bs, ok := s.Store.(blobstore.BoundedFrameStore); ok {
		raw, err = bs.GetFrameBounded(ctx, id, maxEncoded)
	} else {
		raw, err = s.Store.GetFrame(ctx, id)
	}
	if err == nil {
		s.rec.mu.Lock()
		s.rec.fetchedFrames[id]++
		s.rec.mu.Unlock()
	}
	return raw, err
}

func (s recordedStore) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.Locate")
	defer s.rec.end()
	return s.Store.Locate(ctx, rids)
}

func (s recordedStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	if err := s.waitIfBlocked(ctx); err != nil {
		return nil, err
	}
	s.rec.begin("Store.Has")
	defer s.rec.end()
	return s.Store.Has(ctx, rids)
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
	cont.opt.AsyncFiles = true
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

func countStoreObjects(t *testing.T, store blobstore.Store, frames []string) int {
	t.Helper()
	seen := map[string]bool{}
	for _, fid := range frames {
		raw, err := store.GetFrame(t.Context(), fid)
		mustNil(t, err)
		hdr, _, err := blobstore.Decode(raw)
		mustNil(t, err)
		for _, obj := range hdr.Objects {
			seen[obj.RID] = true
		}
	}
	return len(seen)
}

// TestR1RealisticColdAndWarmCriticalPathOpensWithinFourRounds verifies R1:
// a realistic chat with >= 300 conversation objects across frames, journal and
// tool output plus >= 2,500 workspace objects opens within 4 serial round trips
// (1.6s fake clock at 400ms/request) in both the warm case (receiver holds a
// stale older copy of the 2,500-file chat) and the cold case, and completes the
// workspace byte-identical to the source.
func TestR1RealisticColdAndWarmCriticalPathOpensWithinFourRounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	// Seed 2,500 workspace files across 25 packages so the workspace has >= 2,500 objects.
	for i := 0; i < 2500; i++ {
		dir := filepath.Join(h.work, "pkg", fmt.Sprintf("mod-%02d", i/100))
		mustNil(t, os.MkdirAll(dir, 0o700))
		body := fmt.Sprintf("package mod%02d\n\n// File %04d content block\nconst Value%04d = %q\n%s\n",
			i/100, i, i, fmt.Sprintf("initial-v1-%04d", i), strings.Repeat("// workspace line padding\n", 12))
		mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", i)), []byte(body), 0o600))
	}

	a := h.openA()
	a.mustSay("initial 2500-file workspace turn")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	// Warm receiver B takes the initial 2,500-file chat so B holds an older copy on disk.
	warmInitial, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if warmInitial.Wait != nil {
		mustNil(t, warmInitial.Wait(t.Context()))
	}

	// A takes the chat back and runs multiple turns that produce >= 300 conversation
	// objects across frames, journal, tool outputs (logs/stubs), job logs (logs/jobs),
	// and task journals (.cell/tasks), plus workspace edits across multiple frames.
	_, err = h.continuerA().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	a = h.openA()

	stubsDir := filepath.Join(h.cell.Root, "logs", "stubs")
	jobsDir := filepath.Join(h.cell.Root, "logs", "jobs")
	tasksDir := filepath.Join(h.cell.Root, ".cell", "tasks")
	mustNil(t, os.MkdirAll(stubsDir, 0o700))
	mustNil(t, os.MkdirAll(jobsDir, 0o700))
	mustNil(t, os.MkdirAll(tasksDir, 0o700))

	for turn := 1; turn <= 6; turn++ {
		for j := 0; j < 30; j++ {
			idx := (turn-1)*30 + j
			stubBody := fmt.Sprintf("tool output stub %d (turn %d)\n%s\n", idx, turn, strings.Repeat(fmt.Sprintf("stub-%03d-line ", idx), 200))
			mustNil(t, os.WriteFile(filepath.Join(stubsDir, fmt.Sprintf("tool-%03d.txt", idx)), []byte(stubBody), 0o600))
			jobBody := fmt.Sprintf("job log %d (turn %d)\n%s\n", idx, turn, strings.Repeat(fmt.Sprintf("job-%03d-output ", idx), 200))
			mustNil(t, os.WriteFile(filepath.Join(jobsDir, fmt.Sprintf("job-%03d.log", idx)), []byte(jobBody), 0o600))
		}
		for k := 0; k < 10; k++ {
			tIdx := (turn-1)*10 + k
			taskBody := fmt.Sprintf("{\"type\":\"task\",\"id\":%d,\"turn\":%d,\"notes\":%q}\n", tIdx, turn, strings.Repeat(fmt.Sprintf("task-%03d-", tIdx), 150))
			mustNil(t, os.WriteFile(filepath.Join(tasksDir, fmt.Sprintf("task-%03d.jsonl", tIdx)), []byte(taskBody), 0o600))
		}
		// Append substantial transcript entries and edit a slice of workspace files each turn.
		for m := 0; m < 15; m++ {
			appendTo(t, transcriptOf(h.cell), fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n",
				fmt.Sprintf("turn %d message %d: %s", turn, m, strings.Repeat("question-detail-", 40))))
		}
		for w := (turn - 1) * 20; w < turn*20; w++ {
			dir := filepath.Join(h.work, "pkg", fmt.Sprintf("mod-%02d", w/100))
			body := fmt.Sprintf("package mod%02d\n\n// Updated in turn %d\nconst Value%04d = %q\n%s\n",
				w/100, turn, w, fmt.Sprintf("updated-turn-%d-%04d", turn, w), strings.Repeat("// updated workspace line\n", 800))
			mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", w)), []byte(body), 0o600))
		}
		a.mustSay(fmt.Sprintf("realistic turn %d final reply", turn))
		h.durable(h.cell.ID, h.cell)
	}
	mustNil(t, a.drive.Close(t.Context()))

	viewAfterA, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	totalObjs := countStoreObjects(t, h.a.Store, viewAfterA.Cell.Frames)
	t.Logf("R1 fixture: frames=%d total_objects=%d", len(viewAfterA.Cell.Frames), totalObjs)
	if totalObjs < 2800 {
		t.Fatalf("R1 fixture too small: got %d total objects across %d frames; want >= 2800 (300 conv + 2500 workspace)", totalObjs, len(viewAfterA.Cell.Frames))
	}

	// 1. WARM CASE: B already has the stale 2,500-file copy from turn 1 and now continues turn 6.
	warmRec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
	warmRec.blockBackground()
	origBDir, origBStore := h.b.Dir, h.b.Store
	h.b.Dir = recordedDir{Client: origBDir, rec: warmRec}
	h.b.Store = recordedStore{Store: origBStore, rec: warmRec}

	warmCont := h.continuerB()
	var warmShownRounds int
	var warmShownClock time.Duration
	var warmShownEvents []string
	var warmShownTranscript string
	warmCont.opt.OnShown = func(got Continued) {
		warmShownRounds, _, _, warmShownClock, warmShownEvents = warmRec.snapshot()
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		warmShownTranscript = string(raw)
	}
	warmGot, err := warmCont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	t.Cleanup(func() {
		warmRec.unblock()
		if warmGot.Wait != nil {
			_ = warmGot.Wait(context.Background())
		}
	})
	t.Logf("R1 warm case: shownRounds=%d shownClock=%s events=%v", warmShownRounds, warmShownClock, warmShownEvents)
	if warmShownRounds == 0 || warmShownRounds > 3 || warmShownClock > 1200*time.Millisecond {
		t.Errorf("R1 warm: chat opened after %d serial rounds (%s), events=%v; want <= 3 rounds (<= 1.2s)", warmShownRounds, warmShownClock, warmShownEvents)
	}
	if !strings.Contains(warmShownTranscript, "realistic turn 6 final reply") {
		t.Errorf("R1 warm: latest transcript missing at shown")
	}
	warmRec.unblock()
	mustNil(t, warmGot.Wait(t.Context()))
	assertSameTree(t, h.work, workspaceOf(warmGot.Taken.Cell.Root))
	h.b.Dir, h.b.Store = origBDir, origBStore

	// 2. COLD CASE: a fresh third computer C with no prior copy continues the chat.
	homeC := t.TempDir()
	_, err = identity.Adopt(homeC, h.a.Identity, false)
	mustNil(t, err)
	syncC := openSync(t, homeC)
	rootsC := t.TempDir()
	engC := h.engB
	engC.DataRoot = t.TempDir()

	coldRec := newRecordedRelay(t, syncC.Dir, syncC.Store, 400*time.Millisecond)
	coldRec.blockBackground()
	syncC.Dir = recordedDir{Client: syncC.Dir, rec: coldRec}
	syncC.Store = recordedStore{Store: syncC.Store, rec: coldRec}

	coldCont := syncC.Continuer(engC, TakeOptions{
		DeviceName: "cold-laptop",
		RootFor:    func(id string) string { return filepath.Join(rootsC, id) },
		Notify:     h.wall.add,
	})
	var coldShownRounds int
	var coldShownClock time.Duration
	var coldShownEvents []string
	var coldShownTranscript string
	coldCont.opt.OnShown = func(got Continued) {
		coldShownRounds, _, _, coldShownClock, coldShownEvents = coldRec.snapshot()
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		coldShownTranscript = string(raw)
	}
	coldGot, err := coldCont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	t.Cleanup(func() {
		coldRec.unblock()
		if coldGot.Wait != nil {
			_ = coldGot.Wait(context.Background())
		}
	})
	t.Logf("R1 cold case: shownRounds=%d shownClock=%s events=%v", coldShownRounds, coldShownClock, coldShownEvents)
	if coldShownRounds == 0 || coldShownRounds > 3 || coldShownClock > 1200*time.Millisecond {
		t.Errorf("R1 cold: chat opened after %d serial rounds (%s), events=%v; want <= 3 rounds (<= 1.2s)", coldShownRounds, coldShownClock, coldShownEvents)
	}
	if !strings.Contains(coldShownTranscript, "realistic turn 6 final reply") {
		t.Errorf("R1 cold: latest transcript missing at shown")
	}
	coldRec.unblock()
	mustNil(t, coldGot.Wait(t.Context()))
	assertSameTree(t, h.work, workspaceOf(coldGot.Taken.Cell.Root))
}

// Contract R3 (takeFree / OpenAvailable idle-open path): the launch/idle
// auto-continue path uses the same early-open split and parallel head-only
// fetch as Take, opening within <= 3 serial rounds while workspace files finish
// in the background.
func TestR3IdleOpenAvailablePathOpensEarlyWithinFourRounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("turn 1 on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, os.WriteFile(filepath.Join(h.work, "r3-idle.go"), []byte("package main\n// r3 idle open\n"), 0o600))
	a.mustSay("realistic turn 6 final reply")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	// Release the lease on A so the chat is idle/free for OpenAvailable (takeFree).
	view, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	mustNil(t, h.a.Dir.Release(t.Context(), h.cell.ID, view.Cell.Lease.Fence))

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
	rec.blockBackground()
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}

	cont := h.continuerB()
	var shownRounds int
	var shownClock time.Duration
	var shownEvents []string
	var shownTranscript string
	cont.opt.OnShown = func(got Continued) {
		shownRounds, _, _, shownClock, shownEvents = rec.snapshot()
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		shownTranscript = string(raw)
	}

	got, err := cont.OpenAvailable(t.Context(), h.cell.ID, nil)
	mustNil(t, err)
	t.Cleanup(func() {
		rec.unblock()
		if got.Wait != nil {
			_ = got.Wait(context.Background())
		}
	})
	if shownRounds == 0 || shownRounds > 3 || shownClock > 1200*time.Millisecond {
		t.Fatalf("R3 OpenAvailable opened after %d serial rounds (%s), events=%v; want <= 3 rounds (<= 1.2s)", shownRounds, shownClock, shownEvents)
	}
	if !strings.Contains(shownTranscript, "realistic turn 6 final reply") {
		t.Fatalf("R3 OpenAvailable transcript missing latest turn at shown")
	}
	rec.unblock()
	mustNil(t, got.Wait(t.Context()))
	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))
}

// Contract Q1: the continue phase line carries open_path= listing the phases
// that ran before shown in order, each with its duration (and fetch with its
// (rX,gY) breakdown). Every relay request on the open path counts toward
// open_gets / open_rounds, and open_path sums to shown within 5%.
func TestQ1OpenPathAccountsForEveryMillisecondBeforeShown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("Q1 initial turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
	mustNil(t, os.WriteFile(filepath.Join(bWork, "q1-change.txt"), []byte("updated on B\n"), 0o600))
	b.mustSay("Q1 reply on B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(origOut) })

	rec := newRecordedRelay(t, h.a.Dir, h.a.Store, 40*time.Millisecond)
	h.a.Dir = recordedDir{Client: h.a.Dir, rec: rec}
	h.a.Store = recordedStore{Store: h.a.Store, rec: rec}

	contA := h.continuerA()
	var shownFired bool
	contA.opt.OnShown = func(Continued) {
		shownFired = true
	}

	got, err := contA.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if !shownFired {
		t.Fatal("Q1: OnShown did not fire")
	}
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}

	var phaseLine string
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "continue "+h.cell.ID+" phases ") {
			phaseLine = line
		}
	}
	if phaseLine == "" {
		t.Fatalf("Q1: missing phase log line in:\n%s", logBuf.String())
	}
	t.Logf("Q1 phase line: %s", phaseLine)

	fields := map[string]string{}
	for _, token := range strings.Fields(phaseLine) {
		if k, v, ok := strings.Cut(token, "="); ok {
			fields[k] = v
		}
	}
	openPath := fields["open_path"]
	if openPath == "" || openPath == "none" {
		t.Fatalf("Q1: missing open_path in phase line: %s", phaseLine)
	}
	shownDur, err := time.ParseDuration(fields["shown"])
	mustNil(t, err)
	openGets, err := strconv.Atoi(fields["open_gets"])
	mustNil(t, err)
	openRounds, err := strconv.Atoi(fields["open_rounds"])
	mustNil(t, err)
	if openGets != 1 || openRounds != 3 {
		t.Fatalf("Q1: got open_gets=%d open_rounds=%d, want open_gets=1 open_rounds=3 (Dir.Cell + Store.GetFrame + Dir.Acquire)", openGets, openRounds)
	}

	var steps []string
	depth := 0
	start := 0
	for i := 0; i < len(openPath); i++ {
		switch openPath[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				steps = append(steps, openPath[start:i])
				start = i + 1
			}
		}
	}
	steps = append(steps, openPath[start:])

	var sum time.Duration
	sawFetch := false
	for _, entry := range steps {
		name, rest, ok := strings.Cut(entry, ":")
		if !ok || name == "" || rest == "" {
			t.Fatalf("Q1: malformed open_path step %q in %q", entry, openPath)
		}
		durStr := rest
		if idx := strings.IndexByte(rest, '('); idx >= 0 {
			durStr = rest[:idx]
			detail := rest[idx:]
			if name == "fetch" && detail == "(r1,g1)" {
				sawFetch = true
			}
		}
		d, err := time.ParseDuration(durStr)
		if err != nil {
			t.Fatalf("Q1: bad duration %q in step %q: %v", durStr, entry, err)
		}
		sum += d
	}
	if !sawFetch {
		t.Fatalf("Q1: expected fetch step with (r1,g1) in open_path=%s", openPath)
	}
	diffRatio := math.Abs(float64(sum-shownDur)) / float64(shownDur)
	if diffRatio > 0.05 {
		t.Fatalf("Q1: open_path sum=%s differs from shown=%s by %.2f%% (> 5%%)", sum, shownDur, diffRatio*100)
	}
}

// Contract Q2: a live local reader holding the journal that takes 1.5s to
// release is asked at the keypress concurrently with the directory read and
// open fetch, and does not delay shown.
func TestQ2LiveLocalReaderTakes1500msWithoutDelayingShown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("Q2 initial turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
	mustNil(t, os.WriteFile(filepath.Join(bWork, "q2-file.txt"), []byte("from B\n"), 0o600))
	b.mustSay("Q2 latest turn from B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	readerFile, err := os.Open(transcriptOf(h.cell))
	mustNil(t, err)
	mustNil(t, filelock.Lock(readerFile, true, true))
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if _, sErr := os.Stat(session.TakeoverPath(h.cell.Root)); sErr == nil {
				time.Sleep(1500 * time.Millisecond)
				session.CancelTakeover(h.cell.Root)
				_ = filelock.Unlock(readerFile)
				_ = readerFile.Close()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	t.Cleanup(func() {
		_ = filelock.Unlock(readerFile)
		_ = readerFile.Close()
	})

	contA := h.continuerA()
	var wallShown time.Duration
	var shownTranscript string
	wallStart := time.Now()
	contA.opt.OnShown = func(got Continued) {
		wallShown = time.Since(wallStart)
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		shownTranscript = string(raw)
	}

	got, err := contA.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if wallShown == 0 || wallShown >= 500*time.Millisecond {
		t.Fatalf("Q2: shown took %s; want < 500ms (must not wait for 1.5s local reader)", wallShown)
	}
	if !strings.Contains(shownTranscript, "Q2 latest turn from B") {
		t.Fatalf("Q2: transcript at shown missing latest turn from B")
	}
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}
	<-readerDone
	assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))
}

// Contract Q3: realistic launch auto-continue path (OpenAvailable / takeFree)
// with the real shape: a 2,500-file workspace, a live local reader holding the
// journal (1.5s release), post-seal pace/usage lines in the local journal, a
// kept-branch map entry, device-name lookup, vault state, and multi-chunk
// conversation history (>64 KB) published from the second computer.
// Must open in <= 3 serial rounds and <= 1.2s fake clock at 400ms/request.
func TestQ3LaunchAutoContinueRealisticShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	for i := 0; i < 2500; i++ {
		dir := filepath.Join(h.work, "pkg", fmt.Sprintf("mod-%02d", i/100))
		mustNil(t, os.MkdirAll(dir, 0o700))
		body := fmt.Sprintf("package mod%02d\n\n// File %04d content block\nconst Value%04d = %q\n%s\n",
			i/100, i, i, fmt.Sprintf("initial-v1-%04d", i), strings.Repeat("// workspace line padding\n", 12))
		mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", i)), []byte(body), 0o600))
	}

	a := h.openA()
	a.mustSay("initial 2500-file workspace turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	// B takes the chat from A and runs 6 turns on B, growing the transcript past
	// 64 KB (multiple chunks) while .cell/cell.json and .cell/env/inventory.json
	// come from earlier frames.
	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)

	stubsDir := filepath.Join(takenB.Taken.Cell.Root, "logs", "stubs")
	jobsDir := filepath.Join(takenB.Taken.Cell.Root, "logs", "jobs")
	tasksDir := filepath.Join(takenB.Taken.Cell.Root, ".cell", "tasks")
	mustNil(t, os.MkdirAll(stubsDir, 0o700))
	mustNil(t, os.MkdirAll(jobsDir, 0o700))
	mustNil(t, os.MkdirAll(tasksDir, 0o700))

	for turn := 1; turn <= 6; turn++ {
		for j := 0; j < 25; j++ {
			idx := (turn-1)*25 + j
			stubBody := fmt.Sprintf("tool output stub %d (turn %d)\n%s\n", idx, turn, strings.Repeat(fmt.Sprintf("stub-%03d-line ", idx), 200))
			mustNil(t, os.WriteFile(filepath.Join(stubsDir, fmt.Sprintf("tool-%03d.txt", idx)), []byte(stubBody), 0o600))
			jobBody := fmt.Sprintf("job log %d (turn %d)\n%s\n", idx, turn, strings.Repeat(fmt.Sprintf("job-%03d-output ", idx), 200))
			mustNil(t, os.WriteFile(filepath.Join(jobsDir, fmt.Sprintf("job-%03d.log", idx)), []byte(jobBody), 0o600))
		}
		for k := 0; k < 10; k++ {
			tIdx := (turn-1)*10 + k
			taskBody := fmt.Sprintf("{\"type\":\"task\",\"id\":%d,\"turn\":%d,\"notes\":%q}\n", tIdx, turn, strings.Repeat(fmt.Sprintf("task-%03d-", tIdx), 150))
			mustNil(t, os.MkdirAll(tasksDir, 0o700))
			mustNil(t, os.WriteFile(filepath.Join(tasksDir, fmt.Sprintf("task-%03d.jsonl", tIdx)), []byte(taskBody), 0o600))
		}
		for m := 0; m < 25; m++ {
			var msgBuf strings.Builder
			for p := 0; p < 15; p++ {
				sum := sha256.Sum256(fmt.Appendf(nil, "turn-%d-msg-%d-part-%d", turn, m, p))
				fmt.Fprintf(&msgBuf, "segment-%x ", sum[:12])
			}
			appendTo(t, transcriptOf(takenB.Taken.Cell), fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n", msgBuf.String()))
		}
		for w := (turn - 1) * 20; w < turn*20; w++ {
			dir := filepath.Join(bWork, "pkg", fmt.Sprintf("mod-%02d", w/100))
			body := fmt.Sprintf("package mod%02d\n\n// Updated on B in turn %d\nconst Value%04d = %q\n%s\n",
				w/100, turn, w, fmt.Sprintf("updated-B-turn-%d-%04d", turn, w), strings.Repeat("// updated workspace line\n", 800))
			mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", w)), []byte(body), 0o600))
		}
		b.mustSay(fmt.Sprintf("realistic B turn %d final reply", turn))
		h.durable(h.cell.ID, takenB.Taken.Cell)
	}
	mustNil(t, b.drive.Close(t.Context()))

	// Push vault state on B and release B's lease so the chat is available for launch auto-continue.
	mustNil(t, h.b.withVault(func(v vaultsync.Syncer) error { return v.Push(t.Context()) }))
	viewB, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	mustNil(t, h.b.Dir.Release(t.Context(), h.cell.ID, viewB.Cell.Lease.Fence))
	_ = os.Remove(filepath.Join(h.a.Home, "send-leases", h.cell.ID))

	// On A (warm receiver):
	// 1. Append post-seal auxiliary pace/usage lines to A's local journal.
	appendTo(t, transcriptOf(h.cell), "{\"type\":\"pace\",\"ms\":420}\n{\"type\":\"usage\",\"input\":100,\"output\":50}\n")
	// 2. Record a pre-existing kept-branch mapping in A's branches.json.
	branchesA, err := cellsync.OpenBranchMap(h.a.Home)
	mustNil(t, err)
	mustNil(t, branchesA.Set(h.cell.ID, "01J00000000000000000000099"))
	// 3. Hold A's journal with a live local reader that takes 1.5s to release after AskTakeover.
	readerFile, err := os.Open(transcriptOf(h.cell))
	mustNil(t, err)
	mustNil(t, filelock.Lock(readerFile, true, true))
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if _, sErr := os.Stat(session.TakeoverPath(h.cell.Root)); sErr == nil {
				time.Sleep(1500 * time.Millisecond)
				session.CancelTakeover(h.cell.Root)
				_ = filelock.Unlock(readerFile)
				_ = readerFile.Close()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	t.Cleanup(func() {
		_ = filelock.Unlock(readerFile)
		_ = readerFile.Close()
	})

	rec := newRecordedRelay(t, h.a.Dir, h.a.Store, 400*time.Millisecond)
	rec.blockBackground()
	origADir, origAStore := h.a.Dir, h.a.Store
	h.a.Dir = recordedDir{Client: origADir, rec: rec}
	h.a.Store = recordedStore{Store: origAStore, rec: rec}

	contA := h.continuerA()
	var shownRounds int
	var shownClock time.Duration
	var shownEvents []string
	var shownTranscript string
	var wallShown time.Duration
	wallStart := time.Now()
	contA.opt.OnShown = func(got Continued) {
		wallShown = time.Since(wallStart)
		shownRounds, _, _, shownClock, shownEvents = rec.snapshot()
		raw, _ := os.ReadFile(transcriptOf(got.Taken.Cell))
		shownTranscript = string(raw)
	}

	seen := &directory.LeaseExpectation{Device: h.b.Device.ID(), Fence: viewB.Cell.Lease.Fence}
	got, err := contA.OpenAvailable(t.Context(), h.cell.ID, seen)
	mustNil(t, err)
	t.Cleanup(func() {
		rec.unblock()
		if got.Wait != nil {
			_ = got.Wait(context.Background())
		}
	})
	t.Logf("Q3 launch auto-continue: shownRounds=%d shownClock=%s wallShown=%s events=%v", shownRounds, shownClock, wallShown, shownEvents)
	if shownRounds == 0 || shownRounds > 3 || shownClock > 1200*time.Millisecond {
		t.Errorf("Q3 launch auto-continue: opened after %d serial rounds (%s), events=%v; want <= 3 rounds (<= 1.2s)", shownRounds, shownClock, shownEvents)
	}
	if wallShown >= 1500*time.Millisecond {
		t.Errorf("Q3 launch auto-continue: wallShown=%s was delayed by 1.5s local reader", wallShown)
	}
	if !strings.Contains(shownTranscript, "realistic B turn 6 final reply") {
		t.Errorf("Q3 launch auto-continue: transcript missing latest turn at shown")
	}
	rec.unblock()
	mustNil(t, got.Wait(t.Context()))
	<-readerDone
	assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))
}

// Contract Q4: nothing local and slow runs before shown. On a 2,500-file warm
// workspace in t.TempDir() (with post-seal pace/usage lines and a kept-branch
// entry), local work before shown is <= 150ms on the real clock while
// preserving local edits and kept branches.
func TestQ4WarmWorkspaceLocalWorkBeforeShownUnder150ms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 2,500-file real-clock local-work budget test in -short mode")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	for i := 0; i < 2500; i++ {
		dir := filepath.Join(h.work, "pkg", fmt.Sprintf("mod-%02d", i/100))
		mustNil(t, os.MkdirAll(dir, 0o700))
		body := fmt.Sprintf("package mod%02d\n\nconst Value%04d = %q\n%s\n",
			i/100, i, fmt.Sprintf("v1-%04d", i), strings.Repeat("// workspace line padding\n", 10))
		mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", i)), []byte(body), 0o600))
	}

	a := h.openA()
	a.mustSay("Q4 initial 2500-file turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
	for w := 0; w < 40; w++ {
		dir := filepath.Join(bWork, "pkg", fmt.Sprintf("mod-%02d", w/100))
		body := fmt.Sprintf("package mod%02d\n\nconst Value%04d = %q\n%s\n",
			w/100, w, fmt.Sprintf("v2-from-B-%04d", w), strings.Repeat("// updated workspace line\n", 200))
		mustNil(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%04d.go", w)), []byte(body), 0o600))
	}
	b.mustSay("Q4 updated reply on B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	// Append post-seal pace/usage lines and a kept-branch entry on A.
	appendTo(t, transcriptOf(h.cell), "{\"type\":\"pace\",\"ms\":310}\n{\"type\":\"usage\",\"input\":80,\"output\":40}\n")
	branchesA, err := cellsync.OpenBranchMap(h.a.Home)
	mustNil(t, err)
	mustNil(t, branchesA.Set(h.cell.ID, "01J00000000000000000000099"))

	contA := h.continuerA()
	var wallShown time.Duration
	wallStart := time.Now()
	contA.opt.OnShown = func(Continued) {
		wallShown = time.Since(wallStart)
	}

	got, err := contA.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	t.Logf("Q4 2500-file warm workspace: wallShown=%s got.Shown=%s", wallShown, got.Shown)
	if wallShown == 0 || wallShown > 150*time.Millisecond {
		t.Fatalf("Q4: real-clock time before shown on 2,500-file warm workspace was %s (got.Shown=%s); want <= 150ms", wallShown, got.Shown)
	}
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}
	assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))
}
