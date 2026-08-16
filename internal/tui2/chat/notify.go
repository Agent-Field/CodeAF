package chat

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The terminal hook call sites (10.5.27, shell.go's five-member contract).
//
// internal/tui2 owns the protocols, the probes, the focus gate and the storm
// guards; it does not own a single fact worth telling a terminal about. This
// file is the other half: the four moments this surface knows about, and
// nothing else in this package speaks to that contract.
//
// THE INTERRUPTION BUDGET IS THREE EVENTS AND IT IS SPENT HERE. 10.5.27 closed
// the door on a fourth kind and on notifying about progress, and the temptation
// this surface has is not a fourth KIND — it is a fourth OCCASION for one of
// the three. So each of the three is raised at exactly one place below:
//
//	needs-input  a journaled row arrived carrying a question
//	delivery     a task this window is watching reached settled
//	failure      a task reached failed or cancelled
//
// Progress is the other channel and it is two states wide: SetBusy while a turn
// streams, cleared when it stops. It never becomes a notification.

// appName titles a desktop notification. It is the product's name and not the
// room's, because a notification is read on another workspace where "aforge" is
// the only word that identifies which program wants something.
const appName = "aforge"

// raise is [tui2.Shell.Notify] behind one name.
//
// The indirection buys the one property this file most needs to be able to
// prove: that the budget is spent on exactly three OCCASIONS. The shell's own
// notifier is right to suppress almost everything — an unnegotiated terminal,
// a focused window, a repeat inside the storm guard — but that suppression
// makes "did this surface decide to interrupt" unobservable from outside, and
// an interruption budget nobody can test is one that grows a fourth occasion
// the first time somebody is in a hurry.
func (a *App) raiseNotice(kind tui2.AttentionKind, title, body string) {
	if a.notify == nil {
		return
	}
	a.hook(a.notify(kind, title, body))
}

// noticeAttention republishes the count of things waiting on a human.
//
// It is a COUNT and not a list because the title is glanced at from another
// workspace and "is anything waiting" is the only question a glance can ask.
// The number is the one the footer already paints amber, read from the same
// place, so the title and the footer can never disagree.
//
// SetAttention returns no command: the count reaches the terminal through the
// title, which Bubble Tea diffs, so an unchanged count costs no bytes.
func (a *App) noticeAttention(open int) { a.shell.SetAttention(open) }

// noticeBusy is the progress channel's whole surface: a turn is streaming in
// this room, or it is not.
func (a *App) noticeBusy(running bool) { a.hook(a.shell.SetBusy(running)) }

// noticePrompt marks the point a user message was committed to the transcript,
// so the terminal's own "jump to previous prompt" walks the conversation.
//
// Once per COMMITTED message, which is why it is called from the post result
// and not from the composer's send: a draft that failed to journal is not a
// prompt, and a mark for it would put a navigation anchor on a turn that never
// happened.
func (a *App) noticePrompt() { a.hook(a.shell.MarkPrompt()) }

// noticeQuestion raises the needs-input event for one journaled ask.
//
// The body is the question's own first line, cut by the protocol layer. It is
// the row's headline rather than a count, because the whole value of this
// notification is deciding whether to come back — and "the charter wants a
// budget" earns a different answer from "may I delete the branch".
func (a *App) noticeQuestion(headline string) {
	headline = strings.TrimSpace(firstLine(headline))
	if headline == "" {
		headline = "a turn is waiting on you"
	}
	a.raiseNotice(tui2.AttentionNeedsInput, appName, headline)
}

// noticeWork raises delivery and failure off the rail's own lifecycle, by
// comparing what each task WAS against what it now is.
//
// It reads the scope source rather than the journal because the source is
// already rebuilt exactly once per journal move and already answers "what
// lifecycle is this task in" — the same reading the card paints. A second walk
// of the graph to answer the same question would be a second opinion, and two
// opinions about whether a job failed is how a surface ends up notifying about
// one that did not.
//
// The first pass after a cold open notifies about nothing: every task is new to
// this window, and a window that announced the whole board on attach would have
// spent the interruption budget on history.
func (a *App) noticeWork() {
	if a.source == nil || !a.source.ready {
		return
	}
	rows := a.source.home.Rows
	seen := make(map[string]rail.Lifecycle, len(rows))
	first := a.lives == nil
	for i := range rows {
		row := rows[i]
		if !strings.HasPrefix(row.ID, rowTaskPrefix) {
			continue
		}
		seen[row.ID] = row.Life
		if first {
			continue
		}
		was, known := a.lives[row.ID]
		if !known || was == row.Life || !row.Life.Terminal() {
			continue
		}
		a.raiseNotice(workEvent(row.Life), appName, workHeadline(row))
	}
	a.lives = seen
}

// workEvent maps a settled lifecycle onto the budget's two non-question events.
// A cancellation is a failure by the same argument the awaiting line makes for
// an interrupt: it is work that stopped without landing, and a person watching
// from another workspace needs to know for the same reason.
func workEvent(life rail.Lifecycle) tui2.AttentionKind {
	if life == rail.LifeSettled {
		return tui2.AttentionDelivery
	}
	return tui2.AttentionFailure
}

// workHeadline names the task and what became of it, in the words the rail
// already uses. It is never an id (5.14).
func workHeadline(row rail.Row) string {
	name := strings.TrimSpace(row.Name)
	if name == "" {
		name = "a task"
	}
	switch row.Life {
	case rail.LifeSettled:
		return name + " finished"
	case rail.LifeFailed:
		return name + " failed"
	case rail.LifeCancelled:
		return name + " was cancelled"
	}
	return name
}
