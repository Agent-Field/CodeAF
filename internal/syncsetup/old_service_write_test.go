package syncsetup

import (
	"context"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// oldService is a sync service deployed before conditional takes: every route
// answers, but no response carries the promise.
func oldService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(withoutTakePromise{w}, r)
	})
}

// TestOldServiceStillLetsTheOnlyComputerWriteItsChat pins #2072's lockout: a
// chat whose lease has lapsed, and that no other device holds, opens as this
// computer's to write even when the service makes no conditional-take promise.
// The turn is published, so the other computers still see it.
func TestOldServiceStillLetsTheOnlyComputerWriteItsChat(t *testing.T) {
	h := newTwoHomes(t, oldService)
	ctx := context.Background()
	a := h.openA()
	a.mustSay("first words")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h.lapse()

	again := h.openA()
	if err := again.drive.Gate(); err != nil {
		t.Fatalf("the only computer reopened its chat as a viewer: %v", err)
	}
	again.mustSay("second words")
	h.durable(h.cell.ID, h.cell)
}

// TestOldServiceLetsLocalTurnsCatchUp pins the lockout's second step: turns
// this computer saved while it could not publish, on top of the head the
// service has, are this computer's own work. Reopening the chat publishes them
// rather than opening it read-only behind "Continue this chat here?".
func TestOldServiceLetsLocalTurnsCatchUp(t *testing.T) {
	h := newTwoHomes(t, oldService)
	ctx := context.Background()
	a := h.openA()
	a.mustSay("published words")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h.lapse()

	// A window that ran unsynced seals on the engine alone.
	seat, err := cellstore.SeatOver(executor.HostBound, h.cell, h.work, nil, nil,
		func(cellstore.Engine) cellstore.Store { return h.engine })
	if err != nil {
		t.Fatal(err)
	}
	local := &openChat{t: t, seat: seat, cell: h.cell, work: h.work, calls: 10}
	local.mustSay("words saved only here")
	if h.directoryHead(h.cell.ID) == headOf(t, h.cell) {
		t.Fatal("the unsynced turn reached the service")
	}

	again := h.openA()
	if err := again.drive.Gate(); err != nil {
		t.Fatalf("this computer's own newer turns opened read-only: %v", err)
	}
	h.durable(h.cell.ID, h.cell)
	again.mustSay("third words")
	h.durable(h.cell.ID, h.cell)
}
