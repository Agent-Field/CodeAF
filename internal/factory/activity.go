package factory

import (
	"context"
	"strings"
	"time"
)

// ActivityMost is how many events an item keeps. The activity is a glance at
// what happened lately, not a log, so the oldest is dropped first.
const ActivityMost = 20

// DefaultReadCost is what one triage read is taken to cost, in dollars, when
// no read has been priced yet on this machine. [Seam.RefreshAll] multiplies
// it by the items it would read again, so the estimate is never blank.
const DefaultReadCost = 0.0005

// The activity words, spelled once so a writer and a test cannot drift.
const (
	EventArrived = "arrived from github"
	EventChanged = "changed on github"
	EventRead    = "read"
	EventTalked  = "talked"
)

// Note appends one event to the item's activity and keeps at most
// [ActivityMost], dropping the oldest. An empty what is not an event.
func (it *Item) Note(at time.Time, what string) {
	what = strings.TrimSpace(what)
	if what == "" {
		return
	}
	it.Activity = append(append([]Event(nil), it.Activity...), Event{At: at, What: what})
	if over := len(it.Activity) - ActivityMost; over > 0 {
		it.Activity = append([]Event(nil), it.Activity[over:]...)
	}
}

// NoteChanges appends the events a write to an item implies, by comparing the
// item before the write with the item after it: a read the triage made
// (TriagedAt went from never to a time), a conversation made for it (Talk went
// from none to one), and stages the plan changed (Adapted grew), which is
// written `stages changed by <who>`, who being the first word of the first new
// record line (`plan`).
//
// THE STORE CALLS THIS ON EVERY WRITE (internal/factory/store's update), so a
// writer never has to remember to: the triage worker, the Talk door and the
// adapt door all write through the store and all get their event. It only
// compares, so a write that changed none of the three adds nothing.
func NoteChanges(before Item, after *Item, at time.Time) {
	if before.Triage.TriagedAt.IsZero() && !after.Triage.TriagedAt.IsZero() {
		after.Note(at, EventRead)
	}
	if strings.TrimSpace(before.Talk) == "" && strings.TrimSpace(after.Talk) != "" {
		after.Note(at, EventTalked)
	}
	if len(after.Adapted) > len(before.Adapted) {
		who := "plan"
		if f := strings.Fields(after.Adapted[len(before.Adapted)]); len(f) > 0 && f[0] != "why:" {
			who = f[0]
		}
		after.Note(at, "stages changed by "+who)
	}
}

// ClearRead takes the triage's read off so the worker reads the item again:
// the sentence, the estimate, the risk, the duplicate, the priority, its
// reason and the stamp. EVERY FIELD THE MODEL WROTE GOES, not only the stamp,
// because the read fills only what is empty (triage.Apply) and would
// otherwise keep yesterday's estimate under today's sentence. The type and the
// size stay: they came from the source's labels and lines first.
func ClearRead(t *Triage) {
	t.Read = ""
	t.Est = 0
	t.Risk = nil
	t.Dup = ""
	t.DupOf = 0
	t.Priority = 0
	t.Reason = ""
	t.Questions = nil
	t.TriagedAt = time.Time{}
}

type dryRunKey struct{}

// DryRun is ctx marked as a dry run: a door handed it answers what it would
// do and does none of it. [Seam.RefreshAll] reads it, so the floor can show
// the count and the cost before `U` spends anything.
func DryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

// IsDryRun says whether ctx was marked by [DryRun].
func IsDryRun(ctx context.Context) bool {
	v, _ := ctx.Value(dryRunKey{}).(bool)
	return v
}
