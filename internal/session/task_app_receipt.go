package session

import "errors"

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
	// was sent, and the note's read stays unknown.
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
	case errors.Is(err, ErrSendUnanswered):
		return AppTaskReceipt{Outcome: AppTaskUnknown, Reason: err.Error(), ClientID: key}
	}
	return AppTaskReceipt{Outcome: AppTaskRefused, Reason: err.Error(), ClientID: key}
}
