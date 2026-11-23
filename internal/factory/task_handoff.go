package factory

import "maps"

// A TASK FROM THE MAIN AGENT ON A STOP IT MAY DECIDE IS ONE MORE ROUND. A task
// sent to a job that stands at a stop puts the step back to work after it
// (run's taskBefore), exactly as "another round" does, so it counts against
// the same limit ([ManagerRoundsMost]). Without this a main agent that had
// used its two rounds could go on sending tasks for ever, and the stop would
// never reach the person.

// ManagerTaskRefusal says why the job's main agent may not send a task now,
// nil when it may: the job is on a stop the main agent may decide and it has
// already put that step back to work as often as it may.
func ManagerTaskRefusal(it Item) error {
	if kind := ManagerStop(it); kind != "" && managerRoundsUsed(it, kind) {
		return ErrManagerRoundsUsed
	}
	return nil
}

// NoteManagerTask counts a task the main agent sent to a job that stands at a
// stop it may decide. It is called before the task is added, while the job
// still reads that stop. A task sent to a job on no such stop counts nothing.
func NoteManagerTask(it *Item) {
	kind := ManagerStop(*it)
	if kind == "" {
		return
	}
	h := Handoff{}
	if it.Handoff != nil {
		h = *it.Handoff
		h.Rounds = maps.Clone(it.Handoff.Rounds)
	}
	if h.Rounds == nil {
		h.Rounds = map[string]int{}
	}
	h.Rounds[stopStep(*it, kind)]++
	it.Handoff = &h
}
