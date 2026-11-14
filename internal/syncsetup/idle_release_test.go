package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

func idleContractChat(t *testing.T, h *twoHomes) *openChat {
	t.Helper()
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	mustNil(t, h.b.putDevice(t.Context(), nameB))
	c := h.openA()
	// Knowledge capture needs a complete timestamp to advance past this seed
	// instead of retrying an invalid journal record during idle certification.
	appendTo(t, transcriptOf(h.cell), fmt.Sprintf(`{"type":"message","role":"user","content":"save this chat","timestamp":%q}`+"\n", h.clock.Now().UTC().Format(time.RFC3339Nano)))
	_, err := c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)
	return c
}

const uploadedIdleReleaseWithin = 8 * time.Second

func idleContractReleased(t *testing.T, h *twoHomes, d *Drive, within ...time.Duration) directory.CellView {
	t.Helper()
	// Functional cases include native capture and publication. C1 supplies its
	// already-uploaded release budget explicitly.
	budget := 30 * time.Second
	if len(within) > 0 {
		budget = within[0]
	}
	d.Idle()
	var view directory.CellView
	deadline := time.Now().Add(budget)
	for {
		var err error
		view, err = h.a.Dir.Cell(t.Context(), h.cell.ID)
		if err == nil && view.Cell.Head == h.head() && view.Cell.Lease.Expires == 0 {
			return view
		}
		if time.Now().After(deadline) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if lockDrive(ctx, &d.sealMu) == nil {
				proof, saveErr := d.engine.SavedState(ctx, d.cell, d.profileFiles()...)
				d.sealMu.Unlock()
				if proof != nil {
					t.Logf("save check: head=%s changes=%v err=%v", proof.Head, proof.Changes, saveErr)
				} else {
					t.Logf("save check: %v", saveErr)
				}
			}
			if d.idleCheck != nil {
				t.Logf("idle readiness: %v", d.idleCheck())
			}
			// Capturing diagnostics can outlast the last directory poll.
			freshCtx, freshCancel := context.WithTimeout(t.Context(), time.Second)
			fresh, freshErr := h.a.Dir.Cell(freshCtx, h.cell.ID)
			freshCancel()
			t.Logf("latest relay: %+v, %v; pending: %d; released locally: %v", fresh, freshErr, d.batcher.Pending(), d.IdleReleased())
			t.Fatalf("idle chat still held: %+v, %v; notices: %v", view, err, h.wall.all())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIdleRelease_C1_PairedIdleReleasesUploadedTurns(t *testing.T) {
	r := newTwoHomes(t)
	c := idleContractChat(t, r)
	idleContractReleased(t, r, c.drive, uploadedIdleReleaseWithin)
	if _, _, err := r.a.SendLease(r.cell.ID); !os.IsNotExist(err) {
		t.Fatalf("C1 released writer retains send permission: %v", err)
	}
}

func TestIdleRelease_DefaultOff_RetainsHoldAndRequiresContinueHere(t *testing.T) {
	h := newTwoHomes(t)
	mustNil(t, h.b.putDevice(t.Context(), nameB))
	c := h.openA()
	appendTo(t, transcriptOf(h.cell), fmt.Sprintf(`{"type":"message","role":"user","content":"default off retains hold","timestamp":%q}`+"\n", h.clock.Now().UTC().Format(time.RFC3339Nano)))
	_, err := c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)

	// With idle handoff unset (default false), Idle() must be a no-op and retain the lease.
	c.drive.Idle()
	finished, err := c.drive.releaseIdle(t.Context(), c.drive.activity.Load())
	if err != nil || !finished || c.drive.IdleReleased() {
		t.Fatalf("default-off releaseIdle = %v, %v, released=%v", finished, err, c.drive.IdleReleased())
	}
	v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Expires <= v.Now || c.drive.Gate() != nil {
		t.Fatalf("default-off idle chat released its hold: %+v, gate=%v", v.Cell.Lease, c.drive.Gate())
	}

	// Even when the chat is explicitly released on the relay, B's Rows() and RowsForSend()
	// must report AutoOpen: false when idle handoff is off by default.
	mustNil(t, h.a.Dir.Release(t.Context(), h.cell.ID, v.Cell.Lease.Fence))
	rows, err := h.b.Rows(t.Context())
	mustNil(t, err)
	found := false
	for _, row := range rows {
		if row.Cell == h.cell.ID {
			found = true
			if row.AutoOpen {
				t.Fatalf("default-off Rows() set AutoOpen=true on %+v", row)
			}
		}
	}
	if !found {
		t.Fatalf("did not find cell %s in B's rows", h.cell.ID)
	}
}

func TestIdleRelease_StorageOff_RefusesReleaseAndAutoOpen(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	h := newTwoHomes(t)
	mustNil(t, h.b.putDevice(t.Context(), nameB))
	c := h.openA()
	appendTo(t, transcriptOf(h.cell), fmt.Sprintf(`{"type":"message","role":"user","content":"storage off guard","timestamp":%q}`+"\n", h.clock.Now().UTC().Format(time.RFC3339Nano)))
	_, err := c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)

	mustNil(t, SaveStorage(h.homeA, h.a.Relay, h.a.Identity.ID(), "off"))
	mustNil(t, SaveStorage(h.homeB, h.b.Relay, h.b.Identity.ID(), "off"))
	confirmed, err := c.drive.ConfirmWork(t.Context())
	if err != nil || !confirmed {
		t.Fatalf("ConfirmWork before storage-off check = %v, %v", confirmed, err)
	}
	c.drive.Idle()
	finished, err := c.drive.releaseIdle(t.Context(), c.drive.activity.Load())
	if err != nil || !finished || c.drive.IdleReleased() {
		t.Fatalf("storage-off releaseIdle = %v, %v, released=%v", finished, err, c.drive.IdleReleased())
	}
	v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Expires <= v.Now {
		t.Fatalf("storage-off idle chat released its hold: %+v", v.Cell.Lease)
	}
	mustNil(t, h.a.Dir.Release(t.Context(), h.cell.ID, v.Cell.Lease.Fence))
	rows, err := h.b.Rows(t.Context())
	mustNil(t, err)
	for _, row := range rows {
		if row.Cell == h.cell.ID && row.AutoOpen {
			t.Fatalf("storage-off Rows() set AutoOpen=true on %+v", row)
		}
	}
}

func TestIdleRelease_P9X2_DisplacedBranchReopensAndReusesKeptCopy(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	d := c.drive
	d.cancel()
	<-d.done
	origView, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)

	// Seal an unpublished turn locally on A while A's hold expires, then let Displaced
	// branch it into a kept copy on the relay and record J -> B in branches.json.
	h.block.set(true)
	c.mustSay("orphaned turn kept in branch")
	h.clock.advance(directory.LeaseTTL + time.Second)
	mustNil(t, d.CheckLease(t.Context()))
	h.block.set(false)
	mustNil(t, d.batcher.Close(t.Context()))

	branches, err := branchMapOf(h.homeA)
	mustNil(t, err)
	keptID := branches.Resolve(h.cell.ID)
	if keptID == h.cell.ID {
		t.Fatal("Displaced did not record a kept branch in branches.json")
	}

	// Reopening the original chat J via TakeSeen must reuse keptID without creating
	// a second branch on the relay, clear J -> keptID from branches.json, and restore J.
	seen, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	got, err := h.continuerA().TakeSeen(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	if got.Taken.Kept != keptID {
		t.Fatalf("TakeSeen Kept = %q, want reused %q", got.Taken.Kept, keptID)
	}
	if got.KeptTurns != 0 {
		t.Fatalf("TakeSeen re-announced KeptTurns = %d for already-branched copy", got.KeptTurns)
	}
	if got.Taken.Driving.Head != origView.Cell.Head || got.Taken.Driving.Fence != origView.Cell.Lease.Fence+1 {
		t.Fatalf("TakeSeen restored head=%s fence=%d, want head=%s fence=%d", got.Taken.Driving.Head, got.Taken.Driving.Fence, origView.Cell.Head, origView.Cell.Lease.Fence+1)
	}
	listing, err := h.a.Dir.List(t.Context())
	mustNil(t, err)
	branchCount := 0
	for _, cell := range listing.Cells {
		if cell.ParentCell == h.cell.ID {
			branchCount++
		}
	}
	if branchCount != 1 {
		t.Fatalf("relay has %d branches for %s, want 1", branchCount, h.cell.ID)
	}
	branches, err = branchMapOf(h.homeA)
	mustNil(t, err)
	if branches.Resolve(h.cell.ID) != h.cell.ID {
		t.Fatalf("TakeSeen left %s mapped to %s in branches.json", h.cell.ID, branches.Resolve(h.cell.ID))
	}
}

func TestIdleRelease_P9X3_RunningTurnReacquiresLapsedOwnHoldInPlace(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	d := c.drive
	d.SetIdleCheck(func() bool { return false })
	d.Working(true)
	defer d.Working(false)

	var before directory.CellView
	for {
		var err error
		before, err = h.a.Dir.Cell(t.Context(), h.cell.ID)
		mustNil(t, err)
		if before.Cell.Lease.Working {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Simulate relay outage + restart / clock jump so the running turn's own hold
	// is observed as lapsed with no other computer holding the chat.
	h.clock.advance(directory.LeaseTTL + time.Second)
	mustNil(t, d.CheckLease(t.Context()))

	if d.Gate() != nil {
		t.Fatalf("running turn lost admission instead of reacquiring in place: %v", d.Gate())
	}
	after, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if after.Cell.Lease.Device != h.a.Device.ID() || after.Cell.Lease.Fence != before.Cell.Lease.Fence+1 || after.Cell.Lease.Expires <= after.Now {
		t.Fatalf("running turn did not conditionally reacquire Fence+1 in place: before=%+v after=%+v", before.Cell.Lease, after.Cell.Lease)
	}
	if _, receiptFence, err := h.a.SendLease(h.cell.ID); err != nil || receiptFence != before.Cell.Lease.Fence+1 {
		t.Fatalf("SendLease after in-place reacquire = %d, %v; want %d", receiptFence, err, before.Cell.Lease.Fence+1)
	}
	// Finish the turn and verify it publishes on the original chat with zero copy branches.
	c.mustSay("turn completed after in-place reacquire")
	h.durable(h.cell.ID, h.cell)
	published, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if published.Cell.Head != h.head() {
		t.Fatalf("turn after in-place reacquire did not publish on original chat: relay=%s local=%s", published.Cell.Head, h.head())
	}
	listing, err := h.a.Dir.List(t.Context())
	mustNil(t, err)
	for _, cell := range listing.Cells {
		if cell.ParentCell == h.cell.ID {
			t.Fatalf("in-place reacquire unexpectedly created a copy branch: %+v", cell)
		}
	}
}

func TestIdleRelease_C3_NewWorkReacquiresBeforeWriting(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	before := idleContractReleased(t, h, c.drive)
	confirmed, err := c.drive.ConfirmWork(t.Context())
	if err != nil || !confirmed || c.drive.Gate() != nil {
		t.Fatalf("C3 take-back: %v, %v, gate=%v", confirmed, err, c.drive.Gate())
	}
	held, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if held.Cell.Lease.Device != h.a.Device.ID() || held.Cell.Lease.Fence != before.Cell.Lease.Fence+1 {
		t.Fatalf("C3 no new hold before writing: %+v", held)
	}
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"new work"}`+"\n")
	_, err = c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)
}

func TestIdleRelease_C4_TakenChatRefusesWithoutWriting(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	seen := idleContractReleased(t, h, c.drive)
	_, err := h.b.Dir.Acquire(t.Context(), h.cell.ID, directory.AcquireOpts{RequireConditional: true, Expect: &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence}})
	mustNil(t, err)
	before, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	head := h.head()
	confirmed, err := c.drive.ConfirmWork(t.Context())
	if err != nil || confirmed || c.drive.Gate() == nil {
		t.Fatalf("C4 new work was admitted: %v, %v", confirmed, err)
	}
	after, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if !bytes.Equal(before, after) || h.head() != head {
		t.Fatal("C4 refusal wrote local conversation work")
	}
	waitFor(t, "C4 moved notice", func() bool { return h.wall.has(chatlist.Superseded(nameB)) })
}

func TestIdleRelease_C4_NewerReleasedChatDoesNotReviveTheOldWindow(t *testing.T) {
	h := newTwoHomes(t)
	a := idleContractChat(t, h)
	seen := idleContractReleased(t, h, a.drive)
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	b := h.openOn(h.b, h.engB, got.Taken.Cell, workspaceOf(got.Taken.Cell.Root), nameB)
	appendTo(t, transcriptOf(got.Taken.Cell), `{"type":"message","role":"assistant","content":"newer work on B"}`+"\n")
	_, err = b.drive.Store(h.engB).Seal(t.Context(), got.Taken.Cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, got.Taken.Cell)
	mustNil(t, b.drive.Close(t.Context()))
	before, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	confirmed, err := a.drive.ConfirmWork(t.Context())
	if err != nil || confirmed || a.drive.Gate() == nil {
		t.Fatalf("C4 older window revived over B's released work: %v, %v", confirmed, err)
	}
	after, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("C4 stale window wrote locally")
	}
	waitFor(t, "C4 last computer's moved notice", func() bool { return h.wall.has(chatlist.Superseded(nameB)) })
}

func TestIdleRelease_C2_C5_C6_C8_C9_UploadBeforeReleaseAndRestore(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	first := h.head()
	h.block.set(true)
	mustNil(t, os.WriteFile(filepath.Join(h.work, ".gitignore"), []byte("node_modules/\n"), 0600))
	mustNil(t, os.WriteFile(filepath.Join(h.work, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0600))
	mustNil(t, os.WriteFile(filepath.Join(h.work, "handoff.txt"), []byte("saved workspace"), 0600))
	mustNil(t, os.MkdirAll(filepath.Join(h.work, "node_modules", "fixture"), 0700))
	mustNil(t, os.WriteFile(filepath.Join(h.work, "node_modules", "fixture", "index.js"), []byte("rebuild me"), 0600))
	brain, err := store.Open(filepath.Join(h.homeA, "graph.db"))
	mustNil(t, err)
	memory, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "Keep the handoff receipt.", Origin: store.OriginPerson})
	mustNil(t, err)
	mustNil(t, brain.Close())
	mustNil(t, os.WriteFile(filepath.Join(h.homeA, "config.json"), []byte(`{"ui.icons":"ascii","daily_budget_usd":7}`), 0600))
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"assistant","content":"final saved answer"}`+"\n")
	_, err = c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	// These saves deliberately happen after the last explicit seal. Idle must
	// capture both the metadata-only change and the workspace edit itself.
	if !a.SetConversationEffort("high") {
		t.Fatal("could not save effort")
	}
	a.SettleWrites()
	mustNil(t, os.WriteFile(filepath.Join(h.work, "handoff.txt"), []byte("workspace saved after seal"), 0600))
	c.drive.Idle()
	time.Sleep(3 * time.Second)
	v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Expires <= v.Now || v.Cell.Head != first {
		t.Fatalf("C2/C5 unsent work released the hold: %+v", v)
	}
	h.block.set(false)
	h.durable(h.cell.ID, h.cell)
	seen := idleContractReleased(t, h, c.drive)
	mustNil(t, c.drive.Close(t.Context()))
	// B's ordinary continuer also removes A's recorded workspace link. No
	// source drive is running; everything below must come from the relay.
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	work := workspaceOf(got.Taken.Cell.Root)
	raw, err := os.ReadFile(filepath.Join(work, "handoff.txt"))
	mustNil(t, err)
	if string(raw) != "workspace saved after seal" {
		t.Fatalf("C5/C6 restored workspace=%q", raw)
	}
	if _, err := os.Stat(filepath.Join(work, "node_modules", "fixture", "index.js")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("C9 rebuildable file transferred: %v", err)
	}
	raw, err = os.ReadFile(transcriptOf(got.Taken.Cell))
	mustNil(t, err)
	if !strings.Contains(string(raw), "final saved answer") || got.Taken.Driving.Head != seen.Cell.Head {
		t.Fatal("C5/C6 journal or saved head was lost")
	}
	restored, err := session.LoadMeta(got.Taken.Cell.Root)
	mustNil(t, err)
	if restored.Effort != "high" {
		t.Fatalf("C5/C6 restored effort=%q", restored.Effort)
	}
	other, err := store.Open(filepath.Join(h.homeB, "graph.db"))
	mustNil(t, err)
	defer other.Close()
	kept, found, err := other.MemoryRecord(memory.ID)
	mustNil(t, err)
	if !found || kept.Text != memory.Text {
		t.Fatal("C5 shared memory did not precede release")
	}
	prefs, err := config.ReadPreferences(h.homeB)
	mustNil(t, err)
	if prefs[config.KeyIcons] != `"ascii"` {
		t.Fatalf("C5 setting did not precede release: %v", prefs)
	}
}

// idleRequests records every HTTP request, including unexpected endpoints.
// Looking only at list/release would miss cell reads, uploads and acquisitions.
type idleRequests struct {
	mu    sync.Mutex
	calls []idleRequest
}
type idleRequest struct {
	key string
	at  time.Time
}

func (c *idleRequests) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.calls = append(c.calls, idleRequest{r.Method + " " + r.URL.Path, time.Now()})
		c.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
func (c *idleRequests) since(n int) []idleRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]idleRequest(nil), c.calls[n:]...)
}

func TestIdleRelease_C10_SingleDeviceAddsNoRequests(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	var idle, control idleRequests
	h := newTwoHomes(t, idle.wrap)
	baseline := newTwoHomes(t, control.wrap)
	open := func(h *twoHomes) (*openChat, time.Time) {
		c := h.openA()
		started := time.Now()
		appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"single device"}`+"\n")
		_, err := c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
		mustNil(t, err)
		h.durable(h.cell.ID, h.cell)
		waitFor(t, "initial vault exchange", func() bool {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			return c.drive.syncVault(ctx) == nil
		})
		return c, started
	}
	c, started := open(h)
	_, controlStarted := open(baseline)
	before, controlBefore := len(idle.since(0)), len(control.since(0))
	c.drive.Idle()
	// A publish covers the first 30-second tick. Observe the next tick too,
	// using real time and the untouched heartbeat loop in a no-Idle control.
	until := controlStarted.Add(2*directory.HeartbeatEvery + 2*time.Second)
	timer := time.NewTimer(time.Until(until))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	for _, one := range []struct {
		name  string
		calls []idleRequest
		start time.Time
		id    string
	}{
		{"idle", idle.since(before), started, h.cell.ID},
		{"control", control.since(controlBefore), controlStarted, baseline.cell.ID},
	} {
		want := "/cells/" + one.id + "/heartbeat"
		if len(one.calls) != 1 || !strings.HasPrefix(one.calls[0].key, "POST ") || !strings.HasSuffix(one.calls[0].key, want) {
			t.Fatalf("C10 %s requests=%v; want exactly one ordinary heartbeat (%s)", one.name, one.calls, want)
		}
		elapsed := one.calls[0].at.Sub(one.start)
		if elapsed < 2*directory.HeartbeatEvery-time.Second || elapsed > 2*directory.HeartbeatEvery+time.Second {
			t.Errorf("C10 %s renewal arrived at %s; want the ordinary second 30s tick", one.name, elapsed)
		}
		t.Logf("C10 %s: all requests=1 (heartbeat), first tick covered by publication, next renewal at %s", one.name, elapsed)
	}
	v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if v.Cell.Lease.Expires <= v.Now || c.drive.Gate() != nil {
		t.Fatal("C10 single computer stopped holding its chat")
	}
}
