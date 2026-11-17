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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
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
	ch := d.rec.blockAfterAcquire
	d.rec.mu.Unlock()
	if cell.IsArrivalOwner(ctx) && ch != nil {
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

func (d recordedDir) Acquire(ctx context.Context, id string, opts directory.AcquireOpts) (view directory.CellView, err error) {
	d.rec.begin("Dir.Acquire")
	defer func() {
		d.rec.end()
		if err == nil {
			d.rec.mu.Lock()
			d.rec.acquireDone = true
			d.rec.mu.Unlock()
		}
	}()
	return d.Client.Acquire(ctx, id, opts)
}

type recordedStore struct {
	blobstore.Store
	rec *recordedRelay
}

func (s recordedStore) waitIfBlocked(ctx context.Context) error {
	s.rec.mu.Lock()
	ch := s.rec.blockAfterAcquire
	s.rec.mu.Unlock()
	if cell.IsArrivalOwner(ctx) && ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if cell.IsArrivalOwner(ctx) && s.rec.failAfterAcquire.Load() {
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

// Contract F1: when a take starts from a view that is already in hand (Home's
// row, the launch/idle walk's record, the alt+c resume band, or the app's
// agents.open), open-state fetch and lease acquire run concurrently from that
// view's head and fence without a redundant directory re-read. At 400ms/request
// on the fake clock, a fresh view shows in <=2 serial rounds (<=0.8s fake) on
// every path (1 round for the take itself after the view is in hand, <=2 rounds
// even including the listing that supplied the view).
func TestF1FreshViewOpensInAtMostTwoRoundsAcrossPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	for _, pathName := range []string{
		"home_take",
		"launch_idle_auto_continue",
		"alt_c_resume_band",
		"agents_open",
	} {
		t.Run(pathName, func(t *testing.T) {
			h := newTwoHomes(t)
			seedTree(t, h.work)
			a := h.openA()
			a.mustSay("F1 initial turn on A for " + pathName)
			h.durable(h.cell.ID, h.cell)
			mustNil(t, a.drive.Close(t.Context()))

			takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
			mustNil(t, err)
			if takenB.Wait != nil {
				mustNil(t, takenB.Wait(t.Context()))
			}
			bWork := workspaceOf(takenB.Taken.Cell.Root)
			b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
			mustNil(t, os.WriteFile(filepath.Join(bWork, "src", "f1.go"), []byte("package src\nconst F1 = \""+pathName+"\"\n"), 0o600))
			b.mustSay("F1 turn on B for " + pathName)
			h.durable(h.cell.ID, takenB.Taken.Cell)
			mustNil(t, b.drive.Close(t.Context()))

			viewB, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
			mustNil(t, err)
			if pathName == "launch_idle_auto_continue" || pathName == "agents_open" {
				mustNil(t, h.b.Dir.Release(t.Context(), h.cell.ID, viewB.Cell.Lease.Fence))
				_ = os.Remove(filepath.Join(h.a.Home, "send-leases", h.cell.ID))
			}

			rows, _, err := h.a.RowsAt(t.Context())
			mustNil(t, err)
			if len(rows) != 1 {
				t.Fatalf("RowsAt returned %d rows, want 1", len(rows))
			}
			row := rows[0]
			if pathName == "alt_c_resume_band" {
				// Resume band / supplied cell view with full cell record in hand.
				h.a.SupplyCellView(row.Cell, viewB)
			}

			rec := newRecordedRelay(t, h.a.Dir, h.a.Store, 400*time.Millisecond)
			rec.blockBackground()
			origADir, origAStore := h.a.Dir, h.a.Store
			h.a.Dir = recordedDir{Client: origADir, rec: rec}
			h.a.Store = recordedStore{Store: origAStore, rec: rec}

			contA := h.continuerA()
			var shownRounds, shownDirReads int
			var shownClock time.Duration
			var shownEvents []string
			contA.opt.OnShown = func(Continued) {
				shownRounds, shownDirReads, _, shownClock, shownEvents = rec.snapshot()
			}

			seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
			var got Continued
			switch pathName {
			case "home_take", "alt_c_resume_band":
				got, err = contA.TakeSeen(t.Context(), row.Cell, seen)
			case "launch_idle_auto_continue", "agents_open":
				got, err = contA.OpenAvailable(t.Context(), row.Cell, seen)
			}
			mustNil(t, err)
			t.Cleanup(func() {
				rec.unblock()
				if got.Wait != nil {
					_ = got.Wait(context.Background())
				}
			})

			t.Logf("F1 %s: shownRounds=%d (%s) shownDirReads=%d events=%v",
				pathName, shownRounds, shownClock, shownDirReads, shownEvents)
			if shownRounds == 0 || shownRounds > 2 || shownClock > 800*time.Millisecond || shownDirReads != 0 {
				t.Fatalf("F1 %s: take with fresh view in hand took %d rounds (%s) and %d Dir.Cell reads, want <=2 rounds (<=0.8s) and 0 Dir.Cell reads; events=%v",
					pathName, shownRounds, shownClock, shownDirReads, shownEvents)
			}
			if pathName == "alt_c_resume_band" && (shownRounds != 1 || shownClock != 400*time.Millisecond) {
				t.Fatalf("F1 %s: take with frame-bearing view took %d rounds (%s), want 1 round (400ms); events=%v",
					pathName, shownRounds, shownClock, shownEvents)
			}
			rec.unblock()
			mustNil(t, got.Wait(t.Context()))
			assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))
		})
	}
}

// Contract F1 (stale fallback & racing refusal):
//   - When the view in hand is stale (another turn was published after the listing),
//     the optimistic concurrent acquire is refused, what was fetched for the stale
//     head is discarded, and the take falls back to re-reading, fetching, and
//     acquiring the newest head with byte-identical history.
//   - When a racing second computer takes the lease first, the take is still
//     refused and never creates two writers.
func TestF1StaleViewFallsBackAndPreservesHistoryAndRefusesRacer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")

	t.Run("stale_view_fallback", func(t *testing.T) {
		h := newTwoHomes(t)
		seedTree(t, h.work)
		a := h.openA()
		a.mustSay("F1 stale-fallback initial turn on A")
		h.durable(h.cell.ID, h.cell)
		mustNil(t, a.drive.Close(t.Context()))

		takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
		mustNil(t, err)
		if takenB.Wait != nil {
			mustNil(t, takenB.Wait(t.Context()))
		}
		bWork := workspaceOf(takenB.Taken.Cell.Root)
		b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
		b.mustSay("F1 turn H1 on B")
		h.durable(h.cell.ID, takenB.Taken.Cell)

		// A lists while B is at H1.
		rows, _, err := h.a.RowsAt(t.Context())
		mustNil(t, err)
		row := rows[0]
		staleHead := row.Head

		// Before A takes, B publishes H2 under the same fence with new history and files.
		mustNil(t, os.WriteFile(filepath.Join(bWork, "src", "part0.go"), []byte("package src\nconst Part0 = \"H2-from-B\"\n"), 0o600))
		b.mustSay("F1 newest turn H2 on B after listing")
		h.durable(h.cell.ID, takenB.Taken.Cell)
		mustNil(t, b.drive.Close(t.Context()))

		viewB2, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
		mustNil(t, err)
		if viewB2.Cell.Head == staleHead {
			t.Fatalf("expected B head to advance from %s", staleHead)
		}
		wantTranscript, err := os.ReadFile(transcriptOf(takenB.Taken.Cell))
		mustNil(t, err)

		contA := h.continuerA()
		var shownTranscript []byte
		contA.opt.OnShown = func(got Continued) {
			shownTranscript, _ = os.ReadFile(transcriptOf(got.Taken.Cell))
		}
		seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
		got, err := contA.TakeSeen(t.Context(), row.Cell, seen)
		mustNil(t, err)
		if got.Taken.Driving.Head != viewB2.Cell.Head {
			t.Fatalf("fallback opened head %s, want newest head %s", got.Taken.Driving.Head, viewB2.Cell.Head)
		}
		if !bytes.Equal(shownTranscript, wantTranscript) {
			t.Fatalf("transcript at shown is not byte-identical to B's newest H2 transcript:\ngot:\n%s\nwant:\n%s", shownTranscript, wantTranscript)
		}
		if got.Wait != nil {
			mustNil(t, got.Wait(t.Context()))
		}
		assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))
	})

	t.Run("racing_second_computer_refused", func(t *testing.T) {
		h := newTwoHomes(t)
		seedTree(t, h.work)
		a := h.openA()
		a.mustSay("F1 race initial turn on A")
		h.durable(h.cell.ID, h.cell)
		mustNil(t, a.drive.Close(t.Context()))

		takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
		mustNil(t, err)
		if takenB.Wait != nil {
			mustNil(t, takenB.Wait(t.Context()))
		}
		bWork := workspaceOf(takenB.Taken.Cell.Root)
		b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
		b.mustSay("F1 race turn on B")
		h.durable(h.cell.ID, takenB.Taken.Cell)
		mustNil(t, b.drive.Close(t.Context()))

		// A lists while B holds the lease.
		rows, _, err := h.a.RowsAt(t.Context())
		mustNil(t, err)
		row := rows[0]

		// A racing third computer C acquires the lease before A's take runs.
		homeC := t.TempDir()
		_, err = identity.Adopt(homeC, h.a.Identity, false)
		mustNil(t, err)
		syncC := openSync(t, homeC)
		claimedC, err := syncC.Dir.Acquire(t.Context(), h.cell.ID, directory.AcquireOpts{})
		mustNil(t, err)

		seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
		_, err = h.continuerA().TakeSeen(t.Context(), row.Cell, seen)
		if !errors.Is(err, directory.ErrLeaseHeld) {
			t.Fatalf("racing TakeSeen err = %v, want ErrLeaseHeld", err)
		}
		after, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
		mustNil(t, err)
		if after.Cell.Lease.Device != syncC.Device.ID() || after.Cell.Lease.Fence != claimedC.Cell.Lease.Fence {
			t.Fatalf("lease after refused race = %+v, want racer C at fence %d", after.Cell.Lease, claimedC.Cell.Lease.Fence)
		}
	})
}

// Contract F2: a warm 2,500-file copy with NO stat-index.json shows within the
// F1 budget with <=150ms of local work before shown, and writes stat-index.json
// at the end of materialize so the next move is fast.
func TestF2WarmCopyWithoutStatIndexOpensUnder150msAndWritesStatIndex(t *testing.T) {
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
	a.mustSay("F2 initial 2500-file turn on A")
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
	b.mustSay("F2 updated reply on B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	// Remove stat-index.json on A so A has NO stat index before the move.
	statIdx := h.engine.StatIndexPath(h.cell)
	_ = os.Remove(statIdx)
	if _, err := os.Stat(statIdx); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat-index.json still exists before take: %v", err)
	}

	rows, _, err := h.a.RowsAt(t.Context())
	mustNil(t, err)
	row := rows[0]

	rec := newRecordedRelay(t, h.a.Dir, h.a.Store, 400*time.Millisecond)
	rec.blockBackground()
	origADir, origAStore := h.a.Dir, h.a.Store
	h.a.Dir = recordedDir{Client: origADir, rec: rec}
	h.a.Store = recordedStore{Store: origAStore, rec: rec}

	contA := h.continuerA()
	var shownRounds int
	var shownClock time.Duration
	var wallShown time.Duration
	wallStart := time.Now()
	contA.opt.OnShown = func(Continued) {
		wallShown = time.Since(wallStart)
		shownRounds, _, _, shownClock, _ = rec.snapshot()
	}

	seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
	got, err := contA.TakeSeen(t.Context(), row.Cell, seen)
	mustNil(t, err)
	t.Cleanup(func() {
		rec.unblock()
		if got.Wait != nil {
			_ = got.Wait(context.Background())
		}
	})

	// Subtract the simulated 2ms/round sleep inside recordedRelay to measure real local work.
	localBeforeShown := wallShown - time.Duration(shownRounds)*2*time.Millisecond
	t.Logf("F2 warm 2500-file without stat-index: shownRounds=%d shownClock=%s localBeforeShown=%s", shownRounds, shownClock, localBeforeShown)
	if shownRounds > 2 || shownClock > 800*time.Millisecond {
		t.Fatalf("F2: shownRounds=%d (%s), want within F1 budget (<=2 rounds, <=800ms)", shownRounds, shownClock)
	}
	if localBeforeShown > 150*time.Millisecond {
		t.Fatalf("F2: local work before shown without stat-index was %s, want <=150ms", localBeforeShown)
	}

	rec.unblock()
	mustNil(t, got.Wait(t.Context()))
	assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))

	// Verify stat-index.json was written at the end of materialize.
	if _, err := os.Stat(h.engine.StatIndexPath(got.Taken.Cell)); err != nil {
		t.Fatalf("F2: stat-index.json was not written after materialize: %v", err)
	}
}

// Contract F2 (local edit preservation without stat index): when stat-index.json
// is missing and the local workspace has a real edit, the critical path stays
// fast and the background full check before workspace swap seals and keeps the
// local edit on a branch and tells the person as today.
func TestF2CopyWithLocalEditAndNoStatIndexKeepsEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("F2 local-edit initial turn on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	takenB, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if takenB.Wait != nil {
		mustNil(t, takenB.Wait(t.Context()))
	}
	bWork := workspaceOf(takenB.Taken.Cell.Root)
	b := h.openOn(h.b, h.engB, takenB.Taken.Cell, bWork, nameB)
	mustNil(t, os.WriteFile(filepath.Join(bWork, "src", "part0.go"), []byte("package src\nconst Part0 = \"remote-from-B\"\n"), 0o600))
	b.mustSay("F2 remote turn on B")
	h.durable(h.cell.ID, takenB.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))

	// Remove stat-index.json on A AND make a real local edit in A's workspace.
	_ = os.Remove(h.engine.StatIndexPath(h.cell))
	localEditContent := "package src\nconst Part0 = \"unsaved-local-edit-on-A-without-stat-index\"\n"
	mustNil(t, os.WriteFile(filepath.Join(h.work, "src", "part0.go"), []byte(localEditContent), 0o600))

	rows, _, err := h.a.RowsAt(t.Context())
	mustNil(t, err)
	row := rows[0]

	contA := h.continuerA()
	var wallShown time.Duration
	wallStart := time.Now()
	contA.opt.OnShown = func(Continued) {
		wallShown = time.Since(wallStart)
	}

	seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
	got, err := contA.TakeSeen(t.Context(), row.Cell, seen)
	mustNil(t, err)
	if wallShown > 150*time.Millisecond {
		t.Fatalf("F2: local work before shown was %s, want <=150ms", wallShown)
	}
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}

	keptID, keptTurns := got.Taken.Kept, got.KeptTurns
	if got.KeptBranch != nil && keptID == "" {
		keptID, keptTurns = got.KeptBranch()
	}
	if keptID == "" || keptTurns == 0 {
		t.Fatalf("F2: expected local edit without stat-index to be kept as a branch, got keptID=%q keptTurns=%d", keptID, keptTurns)
	}
	wallJoined := strings.Join(h.wall.all(), "\n")
	if !strings.Contains(wallJoined, "kept as 1 turn") {
		t.Fatalf("F2: expected person to be notified of kept branch, wall=%q", wallJoined)
	}

	// Main workspace on A must have B's remote version after materialize.
	assertSameTree(t, bWork, workspaceOf(got.Taken.Cell.Root))

	// Opening the kept branch must contain A's local edit intact.
	branchRoot := filepath.Join(t.TempDir(), keptID)
	opener := h.a.Continuer(h.engine, TakeOptions{RootFor: func(string) string { return branchRoot }})
	branchGot, err := opener.OpenBranch(t.Context(), keptID)
	mustNil(t, err)
	if branchGot.Wait != nil {
		mustNil(t, branchGot.Wait(t.Context()))
	}
	keptFile, err := os.ReadFile(filepath.Join(workspaceOf(branchGot.Taken.Cell.Root), "src", "part0.go"))
	mustNil(t, err)
	if string(keptFile) != localEditContent {
		t.Fatalf("F2: kept branch part0.go = %q, want %q", string(keptFile), localEditContent)
	}
}

// Contract F3: nothing reads workspace files before they arrive. On a move
// where the workspace files (and withheld .env files) are still arriving in the
// background, starting the session's warm vault capture (SeatOver WithWarmCapture)
// and Drive waits on workspace arrival, reads the arrived files once ready, and
// emits no "could not keep … in the key vault" error.
func TestF3VaultReadsWaitForWorkspaceArrivalWithNoError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	envFiles := map[string]string{
		"control-plane/.env.dev":                   "DEV_SECRET=alpha123\n",
		"control-plane/web/client/.env":            "CLIENT_KEY=beta456\n",
		"control-plane/web/client/.env.production": "PROD_KEY=gamma789\n",
	}
	for rel, content := range envFiles {
		full := filepath.Join(h.work, filepath.FromSlash(rel))
		mustNil(t, os.MkdirAll(filepath.Dir(full), 0o700))
		mustNil(t, os.WriteFile(full, []byte(content), 0o600))
	}

	a := h.openA()
	a.mustSay("F3 turn with dotenv files on A")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))
	mustNil(t, h.a.withVault(func(v vaultsync.Syncer) error { return v.Push(t.Context()) }))

	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 20*time.Millisecond)
	rec.blockBackground()
	origBDir, origBStore := h.b.Dir, h.b.Store
	h.b.Dir = recordedDir{Client: origBDir, rec: rec}
	h.b.Store = recordedStore{Store: origBStore, rec: rec}

	var noticesMu sync.Mutex
	var notices []string
	recordNotice := func(msg string) {
		noticesMu.Lock()
		notices = append(notices, msg)
		noticesMu.Unlock()
	}

	contB := h.continuerB()
	contB.opt.Notify = recordNotice
	var seat executor.Seat
	var drive *Drive
	var seatErr, driveErr error
	startedAtShown := make(chan struct{})

	contB.opt.OnShown = func(got Continued) {
		bWork := workspaceOf(got.Taken.Cell.Root)
		eng := h.engB
		eng.Workspace = bWork
		// Start Drive and SeatOver (WithWarmCapture) immediately at shown, while
		// background workspace arrival is still blocked.
		go func() {
			defer close(startedAtShown)
			drive, driveErr = h.b.Drive(t.Context(), eng, got.Taken.Cell, DriveOptions{
				DeviceName: nameB,
				OnNotice:   recordNotice,
			})
			if driveErr != nil {
				return
			}
			seat, seatErr = cellstore.SeatOver(
				executor.HostBound,
				got.Taken.Cell,
				bWork,
				nil,
				nil,
				func(cellstore.Engine) cellstore.Store { return drive.Store(eng) },
				cellstore.WithWarmCapture(),
				cellstore.WithNotices(recordNotice),
			)
		}()
	}

	got, err := contB.Take(t.Context(), h.cell.ID)
	mustNil(t, err)

	// Give the concurrent SeatOver / Drive goroutine a moment to hit the arrival gate
	// while files are still arriving, then unblock the background file arrival.
	time.Sleep(50 * time.Millisecond)
	rec.unblock()

	mustNil(t, got.Wait(t.Context()))
	<-startedAtShown
	mustNil(t, driveErr)
	mustNil(t, seatErr)
	if waitable, ok := seat.(interface{ Await(context.Context) error }); ok {
		mustNil(t, waitable.Await(t.Context()))
	}
	mustNil(t, drive.Close(t.Context()))

	noticesMu.Lock()
	allNotices := append([]string(nil), notices...)
	noticesMu.Unlock()
	for _, n := range allNotices {
		if strings.Contains(n, "could not keep") || strings.Contains(n, "key vault") {
			t.Fatalf("F3: unexpected vault error notice during arrival: %q (all notices: %v)", n, allNotices)
		}
	}

	bWork := workspaceOf(got.Taken.Cell.Root)
	for rel, want := range envFiles {
		raw, err := os.ReadFile(filepath.Join(bWork, filepath.FromSlash(rel)))
		if err != nil || string(raw) != want {
			t.Fatalf("F3: restored %s = %q, err = %v; want %q", rel, string(raw), err, want)
		}
	}
}

// Round 5b (1): the cross-process arrival marker must never be sealed, captured
// or published, and must never reach another computer.
func TestF3bArrivalMarkerNeverSealedCapturedOrPublished(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	a.mustSay("initial turn on A")
	h.durable(h.cell.ID, h.cell)

	// Begin an arrival on A and keep it active while sealing a turn.
	arr := cell.BeginWorkspaceArrival(h.cell.Root, h.work, nil, nil)
	defer arr.Complete()

	marker := cell.ArrivalMarkerPath(h.cell.Root)
	if marker == "" {
		t.Fatal("expected non-empty ArrivalMarkerPath")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected arrival marker at %s while arrival is active: %v", marker, err)
	}
	if strings.HasPrefix(marker, h.cell.Root+string(filepath.Separator)) || strings.HasPrefix(marker, h.work+string(filepath.Separator)) {
		t.Fatalf("arrival marker %q must live outside cell root %q and workspace %q", marker, h.cell.Root, h.work)
	}

	assertNoArrivingInTree := func(where, root string) {
		t.Helper()
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if strings.Contains(d.Name(), ".arriving") {
				t.Fatalf("%s: unexpected arrival marker entry %s inside %s", where, p, root)
			}
			return nil
		})
		mustNil(t, err)
	}

	assertNoArrivingInTree("A cell root during arrival", h.cell.Root)
	assertNoArrivingInTree("A workspace during arrival", h.work)

	// Person sends a message while files arrive: transcript grows, Settle is
	// called while arrival is active, and a turn seal runs through Drive's store
	// while the marker file is on disk.
	engA := h.engine
	engA.Workspace = h.work
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"message sent while files arrive"}`+"\n")
	sealedDuringArrival, err := a.drive.Store(engA).Seal(cell.WithArrivalOwner(t.Context()), h.cell, cellstore.TurnInfo{
		Changed: []string{cell.StateDir},
	})
	mustNil(t, err)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected arrival marker to remain on disk during turn seal: %v", err)
	}

	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"assistant","content":"reply while files arrive"}`+"\n")
	if settler, ok := a.seat.(executor.Settler); ok {
		settler.Settle(t.Context())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected arrival marker to remain on disk during Settle: %v", err)
	}

	// Complete the arrival (waking the scheduled Settle seal) and publish to the relay.
	arr.Complete()
	mustNil(t, cellstore.Await(t.Context(), h.cell))
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))

	// 1. Assert no published frame in the store contains .arriving.
	view, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if len(view.Cell.Frames) == 0 {
		t.Fatal("expected published frames on relay")
	}
	cellKey := h.a.Identity.CellKey()
	for _, frameID := range view.Cell.Frames {
		raw, err := h.a.Store.GetFrame(t.Context(), frameID)
		mustNil(t, err)
		if bytes.Contains(raw, []byte(".arriving")) {
			t.Fatalf("frame %s raw bytes contain .arriving", frameID)
		}
		_, objs, err := blobstore.Decode(raw)
		mustNil(t, err)
		if match, found := cellsync.ObjectContainsEntryName(cellKey, objs, ".arriving"); found {
			t.Fatalf("frame %s contains .arriving in decrypted object/entry %q", frameID, match)
		}
	}

	// 2. Assert no open-state record in the open-state cache contains .arriving.
	syncA := h.engine.Sync(cellstore.SyncKeys{CellKey: cellKey, Dedup: h.a.Identity.DedupSecret()}, h.a.Ledger)
	openCache := syncA.OpenCache(h.cell)
	if entries, err := os.ReadDir(openCache); err == nil {
		var openObjs []blobstore.Object
		for _, e := range entries {
			if strings.Contains(e.Name(), ".arriving") {
				t.Fatalf("open-state cache %s contains file %s", openCache, e.Name())
			}
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(openCache, e.Name()))
			mustNil(t, err)
			openObjs = append(openObjs, blobstore.Object{RID: e.Name(), Bytes: b})
		}
		if match, found := cellsync.ObjectContainsEntryName(cellKey, openObjs, ".arriving"); found {
			t.Fatalf("open-state cache %s contains .arriving in %q", openCache, match)
		}
	}

	// 3. Assert no sealed snapshot contains .arriving when materialized.
	headAfter, err := cellstore.Head(h.cell)
	mustNil(t, err)
	var snaps []string
	if sealedDuringArrival.Turn.ID != "" {
		snaps = append(snaps, sealedDuringArrival.Turn.ID)
	}
	if headAfter != nil && headAfter.Turn.ID != "" {
		snaps = append(snaps, headAfter.Turn.ID)
	}
	for _, snap := range snaps {
		if snap == "" {
			continue
		}
		checkRoot := filepath.Join(t.TempDir(), "snap-check")
		checkWork := filepath.Join(t.TempDir(), "snap-work")
		mustNil(t, os.MkdirAll(filepath.Join(checkRoot, cell.StateDir), 0o700))
		mustNil(t, os.MkdirAll(checkWork, 0o700))
		checkEng := h.engine
		checkEng.Workspace = checkWork
		checkSync := checkEng.Sync(cellstore.SyncKeys{CellKey: cellKey, Dedup: h.a.Identity.DedupSecret()}, h.a.Ledger)
		mustNil(t, checkSync.Materialize(t.Context(), cell.Cell{ID: h.cell.ID, Root: checkRoot}, snap))
		assertNoArrivingInTree("materialized snapshot "+snap+" cell root", checkRoot)
		assertNoArrivingInTree("materialized snapshot "+snap+" workspace", checkWork)
	}

	// 4. Assert no materialized receiver tree on B (at shown or after full
	// materialization) contains .arriving.
	contB := h.continuerB()
	contB.opt.OnShown = func(got Continued) {
		assertNoArrivingInTree("B cell root at shown", got.Taken.Cell.Root)
		assertNoArrivingInTree("B workspace at shown", workspaceOf(got.Taken.Cell.Root))
	}
	gotB, err := contB.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	mustNil(t, gotB.Wait(t.Context()))
	assertNoArrivingInTree("B cell root after materialize", gotB.Taken.Cell.Root)
	assertNoArrivingInTree("B workspace after materialize", workspaceOf(gotB.Taken.Cell.Root))
}

// Round 5b (2): a crashed arrival owner in another process must not stall tools
// or captures in a surviving or new process; waiters ignore and remove the dead
// owner's marker instead of waiting out the 2-minute stall bound.
func TestF3bCrashedArrivalOwnerDoesNotStallTools(t *testing.T) {
	if cellRoot := os.Getenv("CODEAF_TEST_F3B_CRASH_CELL"); cellRoot != "" {
		workDir := os.Getenv("CODEAF_TEST_F3B_CRASH_WORK")
		_ = cell.BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
		os.Exit(0)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	targetFile := filepath.Join(h.work, "hello.txt")
	mustNil(t, os.WriteFile(targetFile, []byte("survived owner crash\n"), 0o600))

	// Spawn a child process that begins workspace arrival and exits without Complete().
	cmd := exec.Command(os.Args[0], "-test.run=^TestF3bCrashedArrivalOwnerDoesNotStallTools$")
	cmd.Env = append(os.Environ(),
		"CODEAF_TEST_F3B_CRASH_CELL="+h.cell.Root,
		"CODEAF_TEST_F3B_CRASH_WORK="+h.work,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}

	marker := cell.ArrivalMarkerPath(h.cell.Root)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected crashed child to leave marker at %s: %v", marker, err)
	}

	start := time.Now()
	mustNil(t, cell.WaitWorkspace(t.Context(), h.cell.Root, h.work))
	_, err := h.engine.Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("WaitWorkspace/Seal stalled for %s after arrival owner crashed; expected immediate execution", elapsed)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected dead owner's arrival marker %s to be removed, stat err = %v", marker, err)
	}
}
