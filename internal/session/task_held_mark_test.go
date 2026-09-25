package session

import "testing"

// A HELD LANDING OFFERS AN ANSWER AND DEMANDS NOTHING.
//
// The work was turned back and what it produced is still on the record, so
// taking it anyway is a real thing to do and the card has to say so. Nothing is
// WAITING on the answer: a landing question is raised for [TaskUnverified] and
// retired for every other state (task_landing_question.go), so a failed node
// has no question object, no turn is parked on it, and no conversation holds
// it. [Agent.handToModelOnAuto] says the same thing from the owner's side, in
// its own words: a node that did not land unverified "is not waiting on
// anybody's word, and writing an owner onto it would invent a question nobody
// is asking".
//
// This reading was inventing that question out of the ending's shape alone.
// Four such rows stood under `needs you` for a whole session while nothing in
// flight was waiting on any of them (#1328).
func TestAHeldLandingOffersTheAnswerAndDemandsNothing(t *testing.T) {
	held := TaskFacts{
		State: TaskFailed, Ending: TaskEndingRefused, Held: true,
		Branch: "task/revenue-figures",
		Report: incompleteLead + "the revenue figure is missing",
	}
	status := ProjectTask(held)

	// THE CARD IS OWED IN FULL. A person who goes looking at this row gets the
	// word, the reason and both answers, because the branch is on disk.
	if status.Tier != TaskTierYourCall {
		t.Fatalf("a held landing lost its tier: %q", status.Tier)
	}
	if status.Ask.Kind != TaskAskHeld {
		t.Fatalf("a held landing lost its question: %q", status.Ask.Kind)
	}
	if status.Ask.Yes == "" || status.Ask.No == "" {
		t.Fatalf("a held landing lost its answers: yes=%q no=%q", status.Ask.Yes, status.Ask.No)
	}
	if status.Reason == "" {
		t.Fatalf("a held landing lost the reason the check gave")
	}

	// AND THE DEMAND IS NOT. Attention is what files a row under `needs you`.
	if status.Attention {
		t.Fatalf("a held landing stands in `needs you` with nothing waiting on the answer: %+v", status.Ask)
	}
}

// AND THE ROWS THAT REALLY ARE WAITING STILL SAY SO, which is what keeps the
// test above from passing on a reading that simply stopped demanding anything.
// Each of these has something on the other side of the answer: a conflict is
// two versions of the person's own files and only they can say which survives,
// and a landing nobody could check is the third tier's whole content. Work
// nothing is driving is not among them any more: nothing a person can press
// carries it on yet, so it raises no mark (run_lifecycle_test.go).
func TestTheRowsSomethingIsWaitingOnStillDemand(t *testing.T) {
	for _, one := range []struct {
		name  string
		facts TaskFacts
	}{{
		name: "a conflict only the person can settle",
		facts: TaskFacts{
			State: TaskUnverified, Merge: mergeConflicted,
			Branch: "task/parser", Conflicts: []string{"sheet.md"},
		},
	}, {
		name:  "a landing nobody could check",
		facts: TaskFacts{State: TaskUnverified, Branch: "task/parser"},
	}} {
		t.Run(one.name, func(t *testing.T) {
			if status := ProjectTask(one.facts); !status.Attention {
				t.Fatalf("%s no longer asks for a person: %+v", one.name, status)
			}
		})
	}
}

// A PERSON'S OWN STOP IS NOT A HELD LANDING, and it is worth pinning because
// both settle `failed`. A stopped node is over: it wears no question at all, so
// the reading above must not be what is keeping its mark down.
func TestAStopIsOverRatherThanAnOfferWithNoMark(t *testing.T) {
	status := ProjectTask(TaskFacts{
		State: TaskFailed, Stopped: true, Ending: TaskEndingStopped, Held: true,
	})
	if status.Tier != TaskTierOver {
		t.Fatalf("a person's stop is not over: %q", status.Tier)
	}
	if status.Ask.Kind != "" {
		t.Fatalf("a person's stop grew a question: %q", status.Ask.Kind)
	}
}
