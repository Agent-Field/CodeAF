package clientproto

import "errors"

// The outcome slot (design 3.6, widened 2026-11-27 to every queued call). A queued
// call has no reply on the wire: the phone learns whether it ran from the outcome a
// computer publishes in `outcomes.<writer>`, a slot like any other (slotbox.go)
// whose payload is the last MaxOutcomes entries below. A computer publishes one for
// EVERY call it acknowledges (create, send, answer, stop, inbox.answer, tasks.start),
// including a deterministic refusal, because the call is acknowledged and gone from
// the queue and the phone would otherwise wait on it forever. A transient failure
// publishes nothing: the call stays queued and is tried again.

// Outcome statuses.
const (
	OutcomeDone    = "done"
	OutcomeRefused = "refused"
	OutcomeExpired = "expired"
)

// Outcome reason codes, for the refused and expired statuses. The phone maps each
// to a sentence; one it does not know it shows as a plain refusal.
const (
	ReasonNotHolder    = "not_holder"
	ReasonNeedsFaceID  = "needs_face_id"
	ReasonExpired      = "expired"
	ReasonMethodDenied = "method_denied"
	ReasonBadTarget    = "bad_target"
	ReasonBadCall      = "bad_call"
)

// ErrOutcome says an outcome list is malformed.
var ErrOutcome = errors.New("clientproto: outcomes do not decode")

// Outcome is one entry of the outcome slot. Number is the queue number of the
// call it answers. Agent is the chat the call made or acted on (always present on
// a created or acted-on chat, absent when the call had none). Reason is present on
// refused and expired only.
type Outcome struct {
	Number int64  `json:"number"`
	Status string `json:"status"`
	Agent  string `json:"agent,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Valid says an outcome is well formed: a positive number, a known status, and a
// reason exactly when the status is not done.
func (o Outcome) Valid() bool {
	switch o.Status {
	case OutcomeDone:
		return o.Number > 0 && o.Reason == ""
	case OutcomeRefused, OutcomeExpired:
		return o.Number > 0 && o.Reason != "" && (o.Status != OutcomeExpired || o.Reason == ReasonExpired)
	}
	return false
}

// EncodeOutcomes is the slot payload for the newest outcomes: at most MaxOutcomes,
// oldest first, the earliest dropped. It answers ErrOutcome for an entry that is
// not well formed.
func EncodeOutcomes(list []Outcome) ([]byte, error) {
	if len(list) > MaxOutcomes {
		list = list[len(list)-MaxOutcomes:]
	}
	if list == nil {
		list = []Outcome{}
	}
	for _, o := range list {
		if !o.Valid() {
			return nil, ErrOutcome
		}
	}
	return jsonMarshal(list)
}

// DecodeOutcomes reads a slot payload strictly: unknown keys, trailing data, more
// than MaxOutcomes entries or one that is not well formed are ErrOutcome. (The
// phone drops a bad entry and keeps the rest; this is the computer's own check of
// what it is about to publish and what it published before.)
func DecodeOutcomes(payload []byte) ([]Outcome, error) {
	var list []Outcome
	if jsonUnmarshalStrict(payload, &list) != nil || len(list) > MaxOutcomes {
		return nil, ErrOutcome
	}
	for _, o := range list {
		if !o.Valid() {
			return nil, ErrOutcome
		}
	}
	return list, nil
}
