package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

func idleContractSession(t *testing.T, h *twoHomes, c *openChat, change ...func(*session.Config)) (*session.Agent, *cellstore.SealWatch) {
	t.Helper()
	raw, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	mustNil(t, os.WriteFile(transcriptOf(h.cell), bytes.Replace(raw, []byte(`"version":3`), []byte(`"version":1`), 1), 0600))
	watch := &cellstore.SealWatch{OnTurnEnd: c.drive.Idle, OnMetadataWrite: c.drive.SavedMetadata}
	watch.OnTail = func() { executor.Settle(context.Background(), c.seat); c.drive.Idle() }
	watch.Waiting()
	work := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
		defer cancel()
		confirmed, err := c.drive.ConfirmWork(ctx)
		if err != nil {
			return err
		}
		if !confirmed {
			return cellstore.ErrReadOnly
		}
		return c.drive.Gate()
	}
	prepared := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
		defer cancel()
		return c.drive.ConfirmPreparedWork(ctx)
	}
	cfg := session.Config{Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work},
		SessionFile: transcriptOf(h.cell), Seat: executor.Gated(c.seat, c.drive.Gate, work, prepared, func() error {
			ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			return c.drive.ConfirmSettingsWork(ctx)
		}), Seals: watch,
		Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1", System: "SYSTEM", ProfileDir: h.homeA}
	for _, apply := range change {
		apply(&cfg)
	}
	a, err := session.New(cfg)
	mustNil(t, err)
	t.Cleanup(func() { _ = a.Close() })
	c.drive.SetIdleCheck(watch.IdleReady)
	a.SettleWrites()
	_, err = c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)
	return a, watch
}

func TestIdleRelease_C5_C6_SettingSavedAfterSealSurvivesOpen(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	before := h.head()
	if !a.SetConversationEffort("high") {
		t.Fatal("could not save effort")
	}
	a.SettleWrites()
	// C1 times release after upload. Idle must first capture and publish this
	// metadata-only save, without an explicit seal hiding the original defect.
	c.drive.Idle()
	waitFor(t, "the metadata-only save to reach the relay", func() bool {
		head := h.head()
		return head != before && h.directoryHead(h.cell.ID) == head
	})
	seen := idleContractReleased(t, h, c.drive, uploadedIdleReleaseWithin)
	if seen.Cell.Head == before {
		t.Fatal("C5 metadata-only save was not sealed")
	}
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	restored, err := session.LoadMeta(got.Taken.Cell.Root)
	mustNil(t, err)
	if restored.Effort != "high" {
		t.Fatalf("C5/C6 restored effort=%q", restored.Effort)
	}
}

type idleStartedEngine struct{ started chan struct{} }

func (e *idleStartedEngine) Start(context.Context, session.RunSpec) session.RunSummary {
	close(e.started)
	return session.RunSummary{Outcome: "fixture ended", KeepCopy: true}
}
func (e *idleStartedEngine) Land(context.Context, *plandb.Store, string, string, string) (session.RunLanding, error) {
	return session.RunLanding{}, nil
}

func TestIdleRelease_C2_C4_TaskStartupRechecksOwnership(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	engine := &idleStartedEngine{started: make(chan struct{})}
	session.RegisterRunEngine(engine)
	t.Cleanup(func() { session.RegisterRunEngine(nil) })
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	a, watch := idleContractSession(t, h, c, func(cfg *session.Config) {
		cfg.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) {
			close(entered)
			<-unblock
			return crewroute.Decision{Mode: crewroute.ModeOne}, nil
		}
	})
	t.Cleanup(release)
	done := make(chan error, 1)
	go func() { _, _, _, err := a.StartTask(t.Context(), "repair fixtures", true); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("setup did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("setup did not start")
	}
	watch.Wrote()
	time.Sleep(cellstore.TailQuiet + time.Second)
	view, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Expires <= view.Now {
		t.Error("C2 source released during task startup")
	}
	// An explicit move can displace even busy work. Slow setup must not retain
	// the admission it received before that move.
	_, err = h.b.Dir.Acquire(t.Context(), h.cell.ID, directory.AcquireOpts{Force: true, RequireConditional: true, Expect: &directory.LeaseExpectation{Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence}})
	mustNil(t, err)
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task setup did not finish")
	}
	select {
	case <-engine.started:
		t.Fatal("C4 source entered its engine after ownership moved")
	case <-time.After(100 * time.Millisecond):
	}
}

type idleStateMarker struct{}
type idleWaitingStore struct {
	blobstore.Store
	entered chan struct{}
	all     bool
	ended   chan struct{}
}

func (b *idleWaitingStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if !b.all && ctx.Value(idleStateMarker{}) != true {
		return b.Store.PutFrame(ctx, frame)
	}
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	if b.ended != nil {
		b.ended <- struct{}{}
	}
	return "", ctx.Err()
}

func settleIdleFixture(t *testing.T, h *twoHomes, d *Drive) {
	t.Helper()
	// Head visibility precedes the publisher's vault callback. Stop earlier
	// idle preparation and drain that callback before arming a store fault.
	d.BeginWork()
	joined := make(chan struct{})
	go func() { d.idleWorkers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("startup idle preparation did not stop")
	}
	ready := errors.New("startup publication finished")
	waitFor(t, "the startup publisher's vault callback", func() bool {
		// Refuse the release at its callback: taking the publisher lock proves
		// the preceding flush completed, and changes no ownership state.
		_, err := d.batcher.ReleaseIdle(t.Context(), h.head(), func() error { return ready })
		return errors.Is(err, ready)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	defer cancel()
	confirmed, err := d.ConfirmWork(ctx)
	if err != nil || !confirmed {
		t.Fatalf("fixture could not resume its own hold: %v, %v", confirmed, err)
	}
}

func TestIdleRelease_C3_C5_IdleUploadIsCanceledAndJoined(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	_, watch := idleContractSession(t, h, c)
	waitFor(t, "saved session", watch.IdleReady)
	settleIdleFixture(t, h, c.drive)
	waiting := &idleWaitingStore{Store: h.a.Store, entered: make(chan struct{}, 2), ended: make(chan struct{}, 4), all: true}
	// A visible head can precede its publisher's vault callback.
	stateMu.Lock()
	h.a.Store = waiting
	stateMu.Unlock()
	mustNil(t, config.WriteAPIKey(h.homeA, fakeKeyOne))
	start := func() {
		c.drive.Idle()
		select {
		case <-waiting.entered:
		case <-time.After(8 * time.Second):
			t.Fatal("idle upload did not start")
		}
	}
	start()
	ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	defer cancel()
	confirmed, err := c.drive.ConfirmWork(ctx)
	if err != nil || !confirmed {
		t.Fatalf("C3 work admission: %v, %v", confirmed, err)
	}
	select {
	case <-waiting.ended:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("new work did not cancel idle upload")
	}
	start()
	ctx, stop := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer stop()
	closed := make(chan error, 1)
	go func() { closed <- c.drive.Close(ctx) }()
	select {
	case err := <-closed:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close did not cancel and join its idle attempt")
	}
	joined := make(chan struct{})
	go func() { c.drive.idleWorkers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close left an idle attempt running")
	}
}

func TestIdleRelease_C3_C5_StalledVaultDoesNotTrapSendOrClose(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, watch := idleContractSession(t, h, c)
	waitFor(t, "session to settle before the stalled exchange", watch.IdleReady)
	settleIdleFixture(t, h, c.drive)
	waiting := &idleWaitingStore{Store: h.a.Store, entered: make(chan struct{}, 1)}
	stateMu.Lock()
	h.a.Store = waiting
	stateMu.Unlock()
	mustNil(t, config.WriteAPIKey(h.homeA, fakeKeyOne))
	stateCtx, stopState := context.WithCancel(context.WithValue(t.Context(), idleStateMarker{}, true))
	stateDone := make(chan struct{})
	go func() { defer close(stateDone); _ = h.a.SyncState(stateCtx) }()
	defer func() { stopState(); <-stateDone }()
	select {
	case <-waiting.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("home-state upload did not start")
	}
	c.drive.Idle()
	time.Sleep(cellstore.TailQuiet + 300*time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	confirmed := make(chan struct{})
	go func() { defer close(confirmed); _, _ = c.drive.ConfirmWork(ctx) }()
	select {
	case <-confirmed:
	case <-time.After(150 * time.Millisecond):
		t.Error("25ms ownership admission blocked behind a home-state upload")
	}
	sent := make(chan error, 1)
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		events, err := a.SubmitBash(t.Context(), "! true")
		sent <- err
		if err == nil {
			for range events {
			}
		}
	}()
	select {
	case <-sent:
	case <-time.After(900 * time.Millisecond):
		t.Error("normal send admission exceeded its ownership deadline")
	}
	closed := make(chan error, 1)
	closeCtx, stopClose := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer stopClose()
	go func() { closed <- c.drive.Close(closeCtx) }()
	select {
	case err := <-closed:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("close: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Close waited for the unrelated stalled upload")
	}
	select {
	case <-stateDone:
		t.Error("fixture's stalled exchange ended before its cancellation")
	default:
	}
	stopState()
	<-confirmed
	<-sendDone
}
