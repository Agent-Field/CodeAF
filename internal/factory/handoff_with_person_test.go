package factory

import (
	"errors"
	"testing"
)

// A STOP ALREADY WITH THE PERSON CANNOT BE HANDED TO THEM AGAIN, and the main
// agent is told exactly that. Handing a stop on is the one decision its tool
// says is always allowed, so "does not fit this stop, it takes: another_round"
// read as an invitation to try something else on a stop that is the person's.
func TestHandingOverAStopThatIsAlreadyThePersons(t *testing.T) {
	it := handStop(func() Item { return handRun(AutonomyAuto) }, outOfRounds)
	if err := ManagerDecides(it, DecideHandToPerson); err != nil {
		t.Fatalf("a stop the main agent holds cannot be handed over: %v", err)
	}
	if !EndHandoff(&it, "It is a taste call.", watchT0) {
		t.Fatal("the stop was not handed over")
	}
	if err := ManagerDecides(it, DecideHandToPerson); !errors.Is(err, ErrStopWithPerson) {
		t.Fatalf("handing over a stop that is the person's: %v", err)
	}
	// The other decisions still fit it: the person may ask the main agent to
	// carry one out.
	if err := ManagerDecides(it, DecideAnotherRound); err != nil {
		t.Fatalf("another round on a stop the person holds: %v", err)
	}
	// A job on no stop at all keeps its own sentence.
	if err := ManagerDecides(handStored(handRun(AutonomyAuto)), DecideHandToPerson); !errors.Is(err, ErrNoManagerStop) {
		t.Fatalf("handing over a job on no stop: %v", err)
	}
}
