package factory

import "encoding/json"

// The words a job file spells a job's end with when the job, in memory, is
// [StateNew] with that end on it. THEY ARE THE FILE'S WORDS ONLY: in memory
// such a job is [StateNew] — with its [Failure], its person's Stop, or its
// [Rejection] on it — which is what every door, the floor, Run again and
// Reopen already read, and the floor derives what to show from those facts.
// Before 2026-11-08 the file said "new" for all of them, so a person or a
// script reading the file saw a job that had never run (live e2e rounds 4 to
// 9: failed jobs 74 and 85, rejected job 312, stopped job 318).
const (
	// StateFailedOnDisk is a job whose run ended on a failure.
	StateFailedOnDisk State = "failed"
	// StateRejectedOnDisk is a job a person rejected: closed, with their
	// words ([Rejection]).
	StateRejectedOnDisk State = "rejected"
	// StateStoppedOnDisk is a job a person stopped ([StoppedByPerson]).
	StateStoppedOnDisk State = "stopped"
)

// diskState is how the item's file spells its state: one of the words above
// when the item is [StateNew] with that end on it, else its own state.
func diskState(it Item) State {
	if it.State != StateNew {
		return it.State
	}
	switch {
	case it.DoneHow == "closed_locally" && it.Rejection != nil:
		return StateRejectedOnDisk
	case it.DoneHow == "" && StoppedByPerson(it):
		return StateStoppedOnDisk
	case it.DoneHow == "" && it.Failure != nil:
		return StateFailedOnDisk
	}
	return it.State
}

// fromDiskState is the in-memory state a file's state word reads as: each of
// the file-only words is [StateNew], and every other state is itself.
func fromDiskState(s State) State {
	switch s {
	case StateFailedOnDisk, StateRejectedOnDisk, StateStoppedOnDisk:
		return StateNew
	}
	return s
}

// MarshalJSON writes the item with its real state ([diskState]), and
// [Item.UnmarshalJSON] reads it back as [StateNew] with its end on it, so a
// write and a read of the same item change nothing.
func (it Item) MarshalJSON() ([]byte, error) {
	type plain Item
	p := plain(it)
	p.State = diskState(it)
	return json.Marshal(p)
}
