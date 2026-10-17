package run

// The one road a worker reads its task's notes by. Before it, every reader
// asked the store for its oldest fifty (or two hundred) notes and filtered
// those, so a task whose first fifty had all been handed over never handed over
// the fifty-first, and a listening program never heard the two-hundred-first.
// The store now pages a task's notes by an opaque note id ([plandb.Store.
// NoteEnvelopesPage]); this file is the worker's side of that: where it has read
// up to, and the rules for moving that position. THE READ IS THE ENVELOPE ONLY
// — identity, author, direction, words and the sender's key. A worker reads one
// task's notes by task id, so the project and chat a note was tagged with, and
// its time, are never read and can never hold the inbox.

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// notesExaminedPerDecision is the most notes one decision — one step boundary,
// one forwarding tick, one page of the unread report — may examine, handed
// over or passed by. It is a bound on one decision's work, not on the
// channel: a decision that stops at it settles what it examined, so the next
// decision carries on from there and nothing is passed over.
const notesExaminedPerDecision = 500

// notePager is the store's paged read of a task's note envelopes. [plandb.Store] is
// one; a test hands in one that answers busy, which is the only way a test in
// this package can meet a lock the store keeps to itself.
type notePager interface {
	NoteEnvelopesPage(ctx context.Context, ask plandb.NoteEnvelopeAsk) (plandb.NoteEnvelopePage, error)
}

// taskInbox is one reader's position in one task's notes.
//
// AFTER IS A LOW-WATER MARK: EVERY NOTE AT OR BEFORE IT IS SETTLED — passed by
// the reader's own skip, or in a batch the caller said it settled
// ([taskInbox.settle]). It is a note id exactly as the store returned it,
// never a count and never a position the reader computed, because the store
// renumbers its rows on every save. It lives only as long as
// the reader: what a task has had is kept as note ids in its record
// ([notesAlreadyHad]), and a new reader starts from the first note and passes
// the had ones by its skip.
type taskInbox struct {
	pages  notePager
	taskID string
	after  string
}

// inboxBatch is one decision's notes and where it would leave the mark.
type inboxBatch struct {
	notes []plandb.NoteEnvelope
	// through is the last note the page examined, handed over or skipped: when
	// every note in notes is settled, every note up to through is.
	through string
	// end is true when the page reached the task's last note, so every note
	// after the mark was examined by this decision's skip.
	end bool
}

func newTaskInbox(pages notePager, taskID string) *taskInbox {
	return &taskInbox{pages: pages, taskID: taskID}
}

// next reads one decision's batch: ONE page of the store's, up to limit notes
// after the mark that skip does not pass by, examining at most
// [notesExaminedPerDecision] of them. That page is the decision's whole
// budget; there is no second read.
//
// EVERY ERROR IS A DECISION WITH NO BATCH, NEVER AN EMPTY INBOX. A busy store,
// a cancelled context, a closed store, a deleted task, a damaged cursor or a
// note too large to read answers the error and moves nothing, so nothing is
// marked read, heard, finished or skipped on it, and the next decision asks
// again from the same mark. There is no retry and no wait in here: the
// caller's own next boundary or tick is the retry.
//
// A NOTE TOO LARGE TO READ HOLDS THE MARK WHERE IT IS. The store never
// truncates or skips it, and neither does this: the notes before it arrive as
// usual, and then every decision answers its refusal, so the notes after it
// wait behind it. NOTHING IN THE PRODUCT EDITS OR DELETES ONE NOTE, so the hold
// lasts until the task is gone or the store is changed by hand. The writer
// refuses an over-long author, client key or body per note, and the envelope
// does not read the plan's project or chat, so a long plan name (a long first
// task title, or a `plandb init` name) holds nothing; only a note written
// before the writer's bounds, or by something outside codeaf's writers, can
// reach a refusal. That is the honest price of
// never consuming a note nobody could read. The bash worker has no report to
// say it is held; the delegate's end-of-run report does.
//
// One error moves the mark, because moving it is what is true: a cursor the
// store no longer knows (and that is not a duplicate) puts it back at the
// first note. The reader's own record of what it has had still passes every
// settled note by, so nothing is handed over twice.
func (b *taskInbox) next(ctx context.Context, limit int, skip func(plandb.NoteEnvelope) bool) (inboxBatch, error) {
	page, err := b.pages.NoteEnvelopesPage(ctx, plandb.NoteEnvelopeAsk{
		TaskID: b.taskID, After: b.after, Limit: limit, Skip: skip, ScanMax: notesExaminedPerDecision,
	})
	if err != nil {
		if errors.Is(err, plandb.ErrNoteCursor) && !errors.Is(err, plandb.ErrDuplicateNoteID) {
			b.after = ""
		}
		return inboxBatch{}, err
	}
	return inboxBatch{notes: page.Notes, through: page.Cursor, end: !page.More}, nil
}

// settle moves the mark to the end of a batch. THE CALLER SETTLES A BATCH ONLY
// WHEN EVERY NOTE IN IT IS SETTLED — handed over, published, or passed by —
// or when it carries none, because the mark says everything before it is done
// with.
func (b *taskInbox) settle(batch inboxBatch) {
	if batch.through != "" {
		b.after = batch.through
	}
}
