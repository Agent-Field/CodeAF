package session

import (
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// AppTaskOutcome is what became of a person's words sent to a task, as the road
// that took them reported it. It is read off the producer's own values — the
// node's receipt, the engine's sentinels — and never off a sentence.
type AppTaskOutcome string

const (
	// AppTaskDelivered: the words were handed to the task's reader, or committed
	// where its worker reads between steps.
	AppTaskDelivered AppTaskOutcome = "delivered"
	// AppTaskHeld: the words are on the task's record and nobody is reading yet
	// ([SteerReceipt.Held]); the work answers for them before it lands.
	AppTaskHeld AppTaskOutcome = "held"
	// AppTaskRefused: the engine refused BEFORE any effect. Nothing was
	// delivered and the words are still the person's.
	AppTaskRefused AppTaskOutcome = "refused"
	// AppTaskUnknown: the effect may have happened ([ErrSendUnanswered]). A
	// caller MUST NOT send the words again blind; it reads the page first.
	AppTaskUnknown AppTaskOutcome = "unknown"
)

// AppTaskReceipt is the typed account of one note or brief sent through
// [Agent.AppTaskActionReceipt].
type AppTaskReceipt struct {
	Outcome AppTaskOutcome `json:"outcome"`
	// Reason is the engine's own sentence for a refusal, an unknown, or a brief
	// that was not kept. It is for a person to read and is never classified.
	Reason string `json:"reason,omitempty"`
	// BriefNotKept says a brief's words were delivered or held and the page's
	// own brief could not be written ([ErrBriefNotKept]). Sending again would
	// deliver the words twice.
	BriefNotKept bool `json:"brief_not_kept,omitempty"`
	// ClientID is the key that went to the producer ([AppNoteClientID] of the
	// intent's): the one the page and the read events carry. Empty means none
	// was sent, and the note's read stays unknown. A brief's key is echoed only;
	// a plan task's amendment stores none, so no surface may watch for it.
	ClientID string `json:"client_id,omitempty"`
}

// errNoReceipt refuses, before any effect, an action that carries no words.
var errNoReceipt = errors.New("only a note or a brief has a receipt; that action was not taken")

// AppTaskActionReceipt is [Agent.AppTaskAction] for a note or a brief, with
// the outcome TYPED rather than flattened into one error.
//
// THE FOUR OUTCOMES ARE FOUR DIFFERENT THINGS FOR A SURFACE TO DO. A delivered
// or held note waits for its read; a refused one goes back to the person; an
// unknown one is kept and looked for on the page, and is never sent again,
// because a note carries no retry identity and a second send is a second note.
//
// Every error the note roads raise other than the two sentinels below is raised
// before the effect — the node's state, a disk that would not take the words,
// nobody in the room with the rollback written, the store's lookups and its one
// transaction — which is what makes "refused" a fact here rather than a guess.
func (a *Agent) AppTaskActionReceipt(intent AppTaskIntent) AppTaskReceipt {
	key := AppNoteClientID(intent.ClientID)
	if !appWordAction(intent.Action) {
		return AppTaskReceipt{Outcome: AppTaskRefused, Reason: errNoReceipt.Error()}
	}
	said, err := a.appTaskAct(intent)
	return appTaskReceiptOf(said, err, key)
}

// appTaskReceiptOf types one note's answer. It reads the receipt and the
// sentinels only.
//
// A PLAN-STORE COMMIT THAT FAILED IS UNKNOWN ON EVERY ROAD, not only the ones
// that already map it ([commitUncertainAsUnanswered]): a plan task's brief, its
// note, and any word action added later all end in the same transaction, and the
// change can be on the disk under that error. Typing it here is what keeps a road
// that forgot the mapping from answering "refused" and inviting a second send.
func appTaskReceiptOf(said SteerReceipt, err error, key string) AppTaskReceipt {
	kept := AppTaskDelivered
	if said.Held {
		kept = AppTaskHeld
	}
	switch {
	case err == nil:
		return AppTaskReceipt{Outcome: kept, ClientID: key}
	case errors.Is(err, ErrBriefNotKept):
		return AppTaskReceipt{Outcome: kept, BriefNotKept: true, Reason: err.Error(), ClientID: key}
	case errors.Is(err, ErrSendUnanswered), errors.Is(err, plandb.ErrCommitUncertain):
		return AppTaskReceipt{Outcome: AppTaskUnknown, Reason: err.Error(), ClientID: key}
	}
	return AppTaskReceipt{Outcome: AppTaskRefused, Reason: err.Error(), ClientID: key}
}

// commitUncertainAsUnanswered keeps a plan-store commit that failed from being
// read as a refusal. The sync after the write can fail with the note already
// durable, so the answer is the one a send nobody answered gets
// ([ErrSendUnanswered]): kept under its key and never sent again blind. Every
// other error passes through untouched, so the store's pre-commit refusals stay
// refusals, and nothing here reads a sentence.
func commitUncertainAsUnanswered(err error) error {
	if errors.Is(err, plandb.ErrCommitUncertain) {
		return fmt.Errorf("%w: %w", ErrSendUnanswered, err)
	}
	return err
}
