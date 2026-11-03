package factory

import "errors"

// PersonStopReason is the same refusal at every execution and publication door.
const PersonStopReason = "the person stopped this job; this job is stopped; a person presses Start to run it"

var ErrPersonStopped = errors.New(PersonStopReason)

// StoppedByPerson honours the explicit record and unrevoked legacy evidence.
// Old records with no surviving evidence retain their previous behaviour.
func StoppedByPerson(it Item) bool {
 return it.PersonStopped || it.StopUndo != nil || it.Failure != nil && it.Failure.Code == CodeStopped
}

// CheckPersonStop is the shared execution gate. Call it on the locked job
// before granting work, never on a cached manager snapshot.
func CheckPersonStop(it Item) error {
 if StoppedByPerson(it) { return ErrPersonStopped }
 return nil
}

// ClearPersonStop belongs only to explicit person controls, before admission.
func ClearPersonStop(it *Item) {
 it.PersonStopped = false
 it.StopUndo = nil
 if it.Failure != nil && it.Failure.Code == CodeStopped { it.Failure = nil }
}
