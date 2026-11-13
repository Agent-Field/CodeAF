package serve

import (
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A WORKSTREAM'S DRIVER HAND ─────────────────────────────────────────────
//
// While the organization runs agents on its drivers' laptops
// (factory.AgentRunnerDriver), a workstream's agent runs on exactly ONE
// laptop at a time: the one whose person holds the workstream's driver hand.
// Two laptops never run the same workstream's agent at once, which is the
// canvas frame's hand rule applied to the workstream (WAVE2.md §2.4, §4.3).
//
//   - Anyone who may write the workstream takes a free hand with PUT, and the
//     holder keeps it by sending PUT again before it lapses.
//   - A hand someone else holds answers 409 naming them, so the desk can say
//     "santosh is driving" instead of starting a second agent.
//   - The holder lets go with DELETE. Its owner and the operator may also take
//     it back from someone who walked away; nobody else may.
//
// HANDS ARE KEPT IN MEMORY, NOT IN THE STORE. A hand is a promise about a
// laptop that is open right now; a server restart forgets every one, and the
// next renewal takes it again, which is the same thing a lapse does.

type driverHand struct {
	handle string
	since  time.Time
	until  time.Time
}

type driverHands struct {
	mu   sync.Mutex
	held map[string]driverHand
}

// current is id's live hand, if anyone holds one.
func (d *driverHands) current(id string, now time.Time) (driverHand, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	hand, ok := d.held[id]
	if !ok || !now.Before(hand.until) {
		return driverHand{}, false
	}
	return hand, true
}

// take gives id's hand to handle, or renews it when handle holds it. It
// answers the hand as it is now, whether it changed hands, and false when
// someone else holds a live one (whose hand is answered).
func (d *driverHands) take(id, handle string, now time.Time) (hand driverHand, changed, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held == nil {
		d.held = map[string]driverHand{}
	}
	cur, held := d.held[id]
	live := held && now.Before(cur.until)
	if live && cur.handle != handle {
		return cur, false, false
	}
	if !live || cur.handle != handle {
		cur = driverHand{handle: handle, since: now}
		changed = true
	}
	cur.until = now.Add(api.DriverHandSeconds * time.Second)
	d.held[id] = cur
	return cur, changed, true
}

// drop lets go of id's hand, whoever holds it, and says whether a live one
// was let go.
func (d *driverHands) drop(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, held := d.held[id]
	delete(d.held, id)
	return held && now.Before(cur.until)
}

// dropAll lets go of every hand: the organization stopped running agents on
// its drivers' laptops.
func (d *driverHands) dropAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.held = nil
}

func driverWire(hand driverHand) api.WorkstreamDriver {
	return api.WorkstreamDriver{
		Handle: hand.handle,
		Since:  hand.since.UTC().Format(time.RFC3339Nano),
		Until:  hand.until.UTC().Format(time.RFC3339Nano),
		TTL:    api.DriverHandSeconds,
	}
}

// workstreamDriver is the wire's driver for id, nil when nobody drives it.
func (h *Handler) workstreamDriver(id string) *api.WorkstreamDriver {
	hand, ok := h.drivers.current(id, h.clock())
	if !ok {
		return nil
	}
	out := driverWire(hand)
	return &out
}

// takeWorkstreamDriver is PUT /v1/workstreams/{id}/driver.
func (h *Handler) takeWorkstreamDriver(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and the people it is shared with can drive this workstream's agent")
		return
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, errWorkstreamArchived.Error())
		return
	}
	if h.agentRunner() != factory.AgentRunnerDriver {
		h.fail(w, http.StatusConflict, "this organization runs its workstreams' agents on its server, so nobody drives one from a laptop")
		return
	}
	hand, changed, ok := h.drivers.take(ws.ID, v.Handle, h.clock())
	if !ok {
		who := hand.handle
		if who == "" {
			who = "someone else"
		}
		h.write(w, http.StatusConflict, api.WorkstreamDriverHeld{Error: who + " is driving this workstream's agent", Driver: driverWire(hand)})
		return
	}
	if changed {
		h.publishWorkstreams(api.EventWorkstreamChanged, v.Handle, nil, ws)
	}
	h.write(w, http.StatusOK, driverWire(hand))
}

// dropWorkstreamDriver is DELETE /v1/workstreams/{id}/driver.
func (h *Handler) dropWorkstreamDriver(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	now := h.clock()
	hand, held := h.drivers.current(ws.ID, now)
	if held && hand.handle != v.Handle && !v.canGovern(ws) {
		h.fail(w, http.StatusForbidden, "only "+hand.handle+", the workstream's owner or the operator can take the wheel from "+hand.handle)
		return
	}
	if h.drivers.drop(ws.ID, now) {
		h.publishWorkstreams(api.EventWorkstreamChanged, v.Handle, nil, ws)
	}
	w.WriteHeader(http.StatusNoContent)
}
