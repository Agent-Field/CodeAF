package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type reviewR5OfflineDirectory struct {
	directory.Client
	down atomic.Bool
}

func (d *reviewR5OfflineDirectory) Cell(ctx context.Context, id string) (directory.CellView, error) {
	if d.down.Load() {
		return directory.CellView{}, directory.ErrUnreachable
	}
	return d.Client.Cell(ctx, id)
}

type reviewR5Engine struct{ started chan struct{} }

func (e *reviewR5Engine) Start(context.Context, session.RunSpec) session.RunSummary {
	close(e.started)
	return session.RunSummary{Outcome: "fixture complete", KeepCopy: true}
}
func (e *reviewR5Engine) Land(context.Context, *plandb.Store, string, string, string) (session.RunLanding, error) {
	return session.RunLanding{}, nil
}
func TestIdleRelease_R5_2_HeldTaskStartsDuringRelayOutage(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	h := newTwoHomes(t)
	mustNil(t, h.b.putDevice(t.Context(), nameB))
	dir := &reviewR5OfflineDirectory{Client: h.a.Dir}
	h.a.Dir = dir
	c := h.openA()
	raw, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	mustNil(t, os.WriteFile(transcriptOf(h.cell), bytes.Replace(raw, []byte(`"version":3`), []byte(`"version":1`), 1), 0600))
	appendTo(t, transcriptOf(h.cell), fmt.Sprintf(`{"type":"message","role":"user","content":"seed held chat","timestamp":%q}`+"\n", h.clock.Now().UTC().Format(time.RFC3339Nano)))
	engine := &reviewR5Engine{started: make(chan struct{})}
	session.RegisterRunEngine(engine)
	t.Cleanup(func() { session.RegisterRunEngine(nil) })
	work := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
		defer cancel()
		_, _ = c.drive.ConfirmWork(ctx)
		return c.drive.Gate()
	}
	prepared := func() error {
		if gate, ok := any(c.drive).(interface{ ConfirmPreparedWork(context.Context) error }); ok {
			ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			return gate.ConfirmPreparedWork(ctx)
		}
		return c.drive.Gate()
	}
	a, err := session.New(session.Config{Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), Seat: executor.Gated(c.seat, c.drive.Gate, work, prepared), Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1", System: "SYSTEM", ProfileDir: h.homeA, TaskMaxLoad: 0, TaskMinFreeMB: 0, RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) {
		return crewroute.Decision{Mode: crewroute.ModeOne}, nil
	}})
	mustNil(t, err)
	t.Cleanup(func() { dir.down.Store(false); _ = a.Close() })
	a.SettleWrites()
	_, err = c.drive.Store(h.engine).Seal(t.Context(), h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	h.durable(h.cell.ID, h.cell)
	before, err := dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	dir.down.Store(true)
	id, _, _, err := a.StartTask(t.Context(), "repair fixtures", true)
	mustNil(t, err)
	select {
	case <-engine.started:
		t.Logf("task %d started with retained hold fence %d", id, before.Cell.Lease.Fence)
	case <-time.After(2 * time.Second):
		t.Errorf("accepted task %d never entered its run engine; gate=%v; index=%+v", id, c.drive.Gate(), a.TaskIndex())
	}
	dir.down.Store(false)
}

func TestIdleRelease_R5_2_ReadFailureDoesNotAdmitReleasedOrTakenWriter(t *testing.T) {
	for _, taken := range []bool{false, true} {
		t.Run(map[bool]string{false: "released", true: "taken"}[taken], func(t *testing.T) {
			h := newTwoHomes(t)
			dir := &reviewR5OfflineDirectory{Client: h.a.Dir}
			h.a.Dir = dir
			c := idleContractChat(t, h)
			idleContractReleased(t, h, c.drive)
			if taken {
				_, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, nil)
				mustNil(t, err)
				_, err = c.drive.ConfirmWork(t.Context())
				mustNil(t, err)
			}
			dir.down.Store(true)
			if err := c.drive.ConfirmPreparedWork(t.Context()); err == nil {
				t.Fatal("unreachable relay admitted a released or taken writer")
			}
			var refusal *executor.WorkRefusal
			if err := c.drive.ConfirmSettingsWork(t.Context()); !errors.As(err, &refusal) {
				t.Fatalf("setting admission lost its typed refusal: %v", err)
			}
		})
	}
}
