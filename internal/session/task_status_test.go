package session

import "testing"

// TestProjectTaskPresence walks the readings a surface draws rows from. Each
// case is a situation the runtime actually produces, and the assertion is what
// a person should be told about it — including the four that used to read as
// one word: a stop, a dropped connection, a refused check and a fault.
func TestProjectTaskPresence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facts    TaskFacts
		presence TaskPresence
		on       TaskWaitOn
		reason   string
		fault    bool
		wants    bool // needs a person
	}{{
		name:     "queued with nothing to say",
		facts:    TaskFacts{State: TaskQueued},
		presence: TaskPresenceQueued,
	}, {
		name:     "queued behind the machine is still queued",
		facts:    TaskFacts{State: TaskQueued, Hold: "machine busy"},
		presence: TaskPresenceQueued,
		on:       TaskWaitMachine,
		reason:   "machine busy",
	}, {
		name:     "queued behind other work waits on that work by name",
		facts:    TaskFacts{State: TaskQueued, Hold: "slot", Waits: []string{"Collect sources", "Draft outline"}},
		presence: TaskPresenceWaiting,
		on:       TaskWaitWork,
		reason:   "Collect sources · Draft outline",
	}, {
		name:     "running is working",
		facts:    TaskFacts{State: TaskRunning},
		presence: TaskPresenceWorking,
	}, {
		name:     "a paced run is waiting on the machine, not working",
		facts:    TaskFacts{State: TaskRunning, Hold: "rate limited"},
		presence: TaskPresenceWaiting,
		on:       TaskWaitMachine,
		reason:   "rate limited",
	}, {
		name:     "a named gap outranks the hold",
		facts:    TaskFacts{State: TaskRunning, Gap: "adding amp-labs to the report", Hold: "rate limited"},
		presence: TaskPresenceFinishing,
		reason:   "adding amp-labs to the report",
	}, {
		name:     "a design awaiting a person is not running work",
		facts:    TaskFacts{State: TaskRunning, Kind: TaskKindHarness, Phase: HarnessPhaseAsking},
		presence: TaskPresenceNeedsLook,
		on:       TaskWaitPerson,
		wants:    true,
	}, {
		name:     "another kind's phase is prose, not a state",
		facts:    TaskFacts{State: TaskRunning, Phase: "designing"},
		presence: TaskPresenceWorking,
		reason:   "designing",
	}, {
		name:     "nobody could check it",
		facts:    TaskFacts{State: TaskUnverified},
		presence: TaskPresenceNeedsLook,
		on:       TaskWaitPerson,
		wants:    true,
	}, {
		name:     "a person's stop is a stop and not a failure",
		facts:    TaskFacts{State: TaskFailed, Stopped: true, Ending: TaskEndingStopped},
		presence: TaskPresenceStopped,
	}, {
		name:     "a stop still going through is still running",
		facts:    TaskFacts{State: TaskRunning, Stopped: true},
		presence: TaskPresenceWorking,
	}, {
		name:     "a record row knows the stop from its ending alone",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingStopped},
		presence: TaskPresenceStopped,
	}, {
		name:     "a dropped connection is incomplete and nobody's fault",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingWire},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "a provider refusal says nothing about the work",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingUpstream},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "a check that named gaps is unfinished work",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingRefused},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "a brief whose world moved never started",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingStale},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "an error is the fault",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingError},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
		fault:    true,
	}, {
		name:     "an older row with no ending keeps the fault it always wore",
		facts:    TaskFacts{State: TaskFailed},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
		fault:    true,
	}, {
		name:     "done is done",
		facts:    TaskFacts{State: TaskDone, Merge: mergeMerged},
		presence: TaskPresenceDone,
	}, {
		name:     "an authoritative negative unmakes a claim of running",
		facts:    TaskFacts{State: TaskRunning, Liveness: TaskLivenessUnclaimed},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "and the same for a queued row nothing holds",
		facts:    TaskFacts{State: TaskQueued, Liveness: TaskLivenessUnclaimed},
		presence: TaskPresenceIncomplete,
		on:       TaskWaitPerson,
	}, {
		name:     "an unheld running row keeps its claim while nobody can say",
		facts:    TaskFacts{State: TaskRunning},
		presence: TaskPresenceWorking,
	}, {
		name:     "a state this build does not know claims nothing at all",
		facts:    TaskFacts{State: TaskState("levitating")},
		presence: TaskPresenceUnknown,
	}, {
		name:     "the check reading what a worker left is finishing",
		facts:    TaskFacts{State: TaskRunning, Life: TaskPhaseChecking},
		presence: TaskPresenceFinishing,
	}, {
		name:     "and so is a repair round, gap or no gap",
		facts:    TaskFacts{State: TaskRunning, Life: TaskPhaseRepairing, Gap: "adding amp-labs"},
		presence: TaskPresenceFinishing,
		reason:   "adding amp-labs",
	}, {
		name:     "an ordinary worker's own life is not finishing",
		facts:    TaskFacts{State: TaskRunning, Life: TaskPhaseWorking},
		presence: TaskPresenceWorking,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectTask(tc.facts)
			if got.Presence != tc.presence {
				t.Errorf("presence = %q, want %q", got.Presence, tc.presence)
			}
			if got.On != tc.on {
				t.Errorf("waiting on = %q, want %q", got.On, tc.on)
			}
			if got.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.reason)
			}
			if got.Fault != tc.fault {
				t.Errorf("fault = %v, want %v", got.Fault, tc.fault)
			}
			if got.Attention != tc.wants {
				t.Errorf("attention = %v, want %v", got.Attention, tc.wants)
			}
			if got.State != tc.facts.State {
				t.Errorf("state passed through as %q, want %q", got.State, tc.facts.State)
			}
		})
	}
}

// TestProjectTaskChangesAreTheirOwnAxis holds the separation this file exists
// for: where the work is and where its EDITS went are two answers, and neither
// overwrites the other.
func TestProjectTaskChangesAreTheirOwnAxis(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facts    TaskFacts
		presence TaskPresence
		changes  TaskChangeDisposition
		unlanded bool
	}{{
		name:     "merged edits are home",
		facts:    TaskFacts{State: TaskDone, Merge: mergeMerged, Branch: "task/fix-nil"},
		presence: TaskPresenceDone,
		changes:  TaskChangesMerged,
	}, {
		name:     "done work whose branch conflicted is still done",
		facts:    TaskFacts{State: TaskDone, Merge: mergeConflicted, Branch: "task/fix-nil"},
		presence: TaskPresenceDone,
		changes:  TaskChangesConflicted,
		unlanded: true,
	}, {
		name:     "a stopped run leaves a branch to collect",
		facts:    TaskFacts{State: TaskFailed, Stopped: true, Merge: mergeAborted, Branch: "task/offline"},
		presence: TaskPresenceStopped,
		changes:  TaskChangesKept,
		unlanded: true,
	}, {
		name:     "a kept word with no branch left nothing behind",
		facts:    TaskFacts{State: TaskDone, Merge: mergeKept},
		presence: TaskPresenceDone,
		changes:  TaskChangesKept,
	}, {
		name:     "work done in the ground is already where it was going",
		facts:    TaskFacts{State: TaskDone, Merge: mergeInPlace},
		presence: TaskPresenceDone,
		changes:  TaskChangesInPlace,
	}, {
		name:     "incomplete work with a kept branch is both facts at once",
		facts:    TaskFacts{State: TaskFailed, Ending: TaskEndingRefused, Merge: mergeKept, Branch: "task/import"},
		presence: TaskPresenceIncomplete,
		changes:  TaskChangesKept,
		unlanded: true,
	}, {
		name:     "a merge word this build does not know places nothing",
		facts:    TaskFacts{State: TaskDone, Merge: "teleported", Branch: "task/x"},
		presence: TaskPresenceDone,
		changes:  TaskChangesNone,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectTask(tc.facts)
			if got.Presence != tc.presence {
				t.Errorf("presence = %q, want %q", got.Presence, tc.presence)
			}
			if got.Changes != tc.changes {
				t.Errorf("changes = %q, want %q", got.Changes, tc.changes)
			}
			if got.ChangesUnlanded() != tc.unlanded {
				t.Errorf("unlanded = %v, want %v", got.ChangesUnlanded(), tc.unlanded)
			}
			if got.ChangesUnlanded() && !got.Attention {
				t.Error("edits nobody brought home must ask for a person")
			}
		})
	}
}

// TestProjectTaskSettledIsTheLifecycleAndNotThePresence keeps terminality on the
// state where the runtime keeps it. The case that matters is the design waiting
// to be approved: its presence asks for a person, and its run is still open.
func TestProjectTaskSettledIsTheLifecycleAndNotThePresence(t *testing.T) {
	for _, state := range []TaskState{TaskQueued, TaskRunning, TaskDone, TaskFailed, TaskUnverified} {
		status := ProjectTask(TaskFacts{State: state})
		if status.Settled() != state.settled() {
			t.Errorf("%s: settled = %v, want %v", state, status.Settled(), state.settled())
		}
	}
	asking := ProjectTask(TaskFacts{State: TaskRunning, Kind: TaskKindHarness, Phase: HarnessPhaseAsking})
	if asking.Presence != TaskPresenceNeedsLook {
		t.Fatalf("presence = %q, want %q", asking.Presence, TaskPresenceNeedsLook)
	}
	if asking.Settled() {
		t.Error("a design waiting to be approved is running work: its room stays open")
	}
	unclaimed := ProjectTask(TaskFacts{State: TaskRunning, Liveness: TaskLivenessUnclaimed})
	if unclaimed.Settled() {
		t.Error("an unclaimed row's lifecycle state still says running; the liveness fact is separate")
	}
	if unclaimed.Liveness != TaskLivenessUnclaimed {
		t.Errorf("liveness passed through as %q", unclaimed.Liveness)
	}
}

// TestTaskIndexEntryStatusFacts checks the liveness a record row carries: the
// caller's answer is authoritative for live-looking rows and says nothing at all
// about settled ones.
func TestTaskIndexEntryStatusFacts(t *testing.T) {
	entry := TaskIndexEntry{Status: string(TaskRunning), Kind: TaskKindHarness, Phase: TaskPhaseChecking}
	if held := entry.StatusFacts(true); held.Liveness != TaskLivenessHeld {
		t.Errorf("held row liveness = %q, want %q", held.Liveness, TaskLivenessHeld)
	}
	facts := entry.StatusFacts(false)
	if facts.Liveness != TaskLivenessUnclaimed {
		t.Fatalf("liveness = %q, want %q", facts.Liveness, TaskLivenessUnclaimed)
	}
	if facts.State != TaskRunning || facts.Kind != TaskKindHarness || facts.Life != TaskPhaseChecking {
		t.Errorf("facts lost the row: %+v", facts)
	}
	settled := TaskIndexEntry{Status: string(TaskDone)}
	if got := settled.StatusFacts(false).Liveness; got != TaskLivenessUnknown {
		t.Errorf("a landed row claims nothing about liveness, got %q", got)
	}
}
