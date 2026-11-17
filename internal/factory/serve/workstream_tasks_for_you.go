package serve

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── BETWEEN PEOPLE: COMMENTS THAT REACH SOMEONE (Workstreams final spec,
// "Is there chat between people?") ──
//
// A comment reaches the task's owner and anyone it names with @. It shows in
// their Inbox under "Comments for you" until they read the task's thread. The
// @ is resolved when the comment is made, against the workstream's people as
// they are then, and kept on the line (its `mentions`); what each person has
// read is kept per task (store.MarkTaskRead), and a task read for one person
// carries what is theirs as `for_you`.

// forYouWindow is how far back a comment can still be unread: a person who
// joins, or never opened a task, is not handed its whole history.
const forYouWindow = 14 * 24 * time.Hour

// wsPeopleNames is every person in ws, by handle, with the name they go by:
// the people named on it and everyone in its teams.
func (h *Handler) wsPeopleNames(ws factory.OrgWorkstream) map[string]string {
	nameOf := h.personNames()
	out := map[string]string{}
	for _, m := range h.workstreamPeople(ws, nameOf) {
		if m.Kind == api.MemberTeam {
			for _, p := range m.Members {
				out[factory.NormalizeHandle(p)] = nameOf(p)
			}
			continue
		}
		out[factory.NormalizeHandle(m.ID)] = m.Name
	}
	return out
}

// fillForYou writes onto each task what is for v: the unread comments on a
// task v owns, or that name v. A failed read leaves the tasks as they were,
// because this part is the quiet one and the list must still arrive.
func (h *Handler) fillForYou(ws factory.OrgWorkstream, v Viewer, tasks []factory.WorkstreamTask) {
	me := factory.NormalizeHandle(v.Handle)
	if me == "" || len(tasks) == 0 {
		return
	}
	// Handed to you, shown once: a task handed to v that v has not opened.
	if unopened, err := h.config.Store.HandedUnopened(ws.ID, me); err == nil && len(unopened) > 0 {
		for i := range tasks {
			t := &tasks[i]
			t.Unopened = unopened[t.ID] && t.HandedBy != nil && t.State != factory.TaskDone &&
				t.Owner.Kind == factory.TaskOwnerPerson && factory.NormalizeHandle(t.Owner.ID) == me
		}
	}
	since := h.clock().Add(-forYouWindow).UTC().Format(time.RFC3339Nano)
	unread, err := h.config.Store.UnreadComments(ws.ID, me, since)
	if err != nil || len(unread) == 0 {
		return
	}
	at := map[string]int{}
	for i, t := range tasks {
		at[t.ID] = i
	}
	for _, c := range unread {
		i, ok := at[c.Task]
		if !ok {
			continue
		}
		t := &tasks[i]
		named := false
		for _, m := range c.Event.Mentions {
			named = named || factory.NormalizeHandle(m) == me
		}
		owns := t.Owner.Kind == factory.TaskOwnerPerson && factory.NormalizeHandle(t.Owner.ID) == me
		if !owns && !named {
			continue
		}
		if t.ForYou == nil {
			t.ForYou = &factory.TaskForYou{}
		}
		t.ForYou.Comments++
		t.ForYou.Mentioned = t.ForYou.Mentioned || named
		t.ForYou.Last = c.Event
	}
}

// markRead records that v has read task's thread. Nobody else's screen
// changes, so nothing is published; the reader's own Inbox reads again. A
// failed mark leaves the comments unread, which is the safe side.
func (h *Handler) markRead(ws factory.OrgWorkstream, v Viewer, task string) {
	_ = h.config.Store.MarkTaskRead(ws.ID, task, v.Handle)
}
