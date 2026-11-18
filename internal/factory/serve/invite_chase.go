package serve

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// ── INVITED PEOPLE WHO NEVER JOIN ─────────────────────────────────────────
//
// A task given to someone invited waits for them to join. If they have not
// after three days, the task's owner gets one Ask, "Hand to someone else?"
// (owner decisions §13; factory.ChaseInvite), so the work does not sit
// silently behind an email nobody opened. The server looks once an hour; the
// Ask is raised by Afi, waits in the owner's Inbox like any other, and Keep
// waiting asks again three days later.

// inviteChaseEvery is how often the server looks for tasks to chase.
const inviteChaseEvery = time.Hour

// scheduleInviteChase sets the next look, and keeps setting it until Close.
func (h *Handler) scheduleInviteChase() {
	if h.config.Store == nil {
		return
	}
	h.ordersMu.Lock()
	defer h.ordersMu.Unlock()
	if h.ordersClosed {
		return
	}
	h.chaseTimer = time.AfterFunc(inviteChaseEvery, func() {
		func() {
			defer guard.Recover("factory invite chase")
			h.chaseInvites(h.clock())
		}()
		h.scheduleInviteChase()
	})
}

// chaseInvites puts the chase Ask on every open task that has waited
// [factory.InviteChaseAfter] on an invite still open at now. An invite that
// was withdrawn or has expired is chased too: the person is no more likely to
// arrive.
func (h *Handler) chaseInvites(now time.Time) {
	if h.config.Store == nil {
		return
	}
	invited := map[string]time.Time{}
	for _, row := range h.tokenRows() {
		if row.Invite == nil || !row.Invite.Settled.IsZero() {
			continue
		}
		invited[tokens.InviteRef(row.ID)] = row.Created
	}
	if len(invited) == 0 {
		return
	}
	all, err := h.config.Store.Workstreams()
	if err != nil {
		return
	}
	names := h.personNames()
	for id, ws := range all {
		if ws.ArchivedAt != nil {
			continue
		}
		var asked []factory.WorkstreamTask
		_ = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
			kept, err := tw.Tasks(id)
			if err != nil {
				return err
			}
			for _, t := range kept {
				held := factory.InviteHeld(t)
				at, ok := invited[held]
				if !ok || !factory.InviteChaseDue(t, at, now) {
					continue
				}
				decider := t.Owner.ID
				if t.Owner.Kind != factory.TaskOwnerPerson || factory.InvitedPerson(decider) {
					decider = ws.StewardHandle()
				}
				if decider == "" || factory.InvitedPerson(decider) {
					continue
				}
				next, ev := factory.ChaseInvite(t, names(held), factory.PersonRef{ID: decider, Name: names(decider)}, h.newTaskID("te_"), now)
				if err := tw.Put(next, t.Rev); err != nil {
					return err
				}
				if err := tw.Append(id, ev); err != nil {
					return err
				}
				asked = append(asked, next)
			}
			return nil
		})
		if len(asked) > 0 {
			h.publishTasks(ws, "", asked...)
		}
	}
}
