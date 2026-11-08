package factory

import "encoding/json"

// StateFailedOnDisk is how a job file spells a job whose run ended on a
// failure. IT IS THE FILE'S WORD ONLY: in memory such a job is [StateNew]
// with its [Failure] on it, which is what every door, the floor and Run again
// already read, and the floor derives "failed" from that failure. Before
// 2026-11-08 the file said "new" as well, so a person or a script reading a
// failed job's file saw a job that had never run (live e2e rounds 4 to 7).
const StateFailedOnDisk State = "failed"

// failedOnDisk says the item's file spells its state [StateFailedOnDisk]: a
// run that ended on a failure that is not a person's Stop, not closed since.
func failedOnDisk(it Item) bool {
	return it.State == StateNew && it.Failure != nil && it.DoneHow == "" && !StoppedByPerson(it)
}

// MarshalJSON writes the item with its real state: a failed job's file says
// [StateFailedOnDisk], and [Item.UnmarshalJSON] reads it back as [StateNew]
// with its failure, so a write and a read of the same item change nothing.
func (it Item) MarshalJSON() ([]byte, error) {
	type plain Item
	p := plain(it)
	if failedOnDisk(it) {
		p.State = StateFailedOnDisk
	}
	return json.Marshal(p)
}
