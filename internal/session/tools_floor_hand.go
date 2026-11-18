package session

// THE FOREMAN'S HAND: A PRESS FOR THE PERSON, AND THEIR WORDS PASSED ON.
//
// The owner's answer 8 (2026-11-17): the person runs their factory by talking
// to its manager alone. So `factory_floor` reads every job's wait
// (tools_floor.go), and on the person's word it does two more things here:
//
//   - `press` presses, as the person, ONE action the job's stored offers list
//     for them now: approve, answer, merge, and the rest of [floorPresses].
//   - `tell` gives the job's main agent one turn of its own conversation with
//     the person's words, and answers what it said.
//
// BOTH NEED THE PERSON'S OWN WORDS IN THE RUNNING TURN, read from the turn
// ([Agent.turnPersonWords]) and never from the model's arguments. A turn that
// carries none (a team's note, a wake) presses nothing and passes nothing on,
// whatever the model writes. The words ride to the door as Said, and the door
// refuses without them too, so the rule does not rest on this file alone.
//
// THE DOOR DECIDES THE REST: whether the job offers the action now, what body
// its verb takes, and what the factory answers (cmd/codeaf's
// factory_foreman_press.go).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FloorHand is a [FloorDoor] that may also press a job's offered action as
// the person and pass their words to a job's main agent. said is the person's
// own words in the running turn; each answers the line the model reads back,
// or a refusal in words.
type FloorHand interface {
	Press(ctx context.Context, id int, p FloorPress, said string) (string, error)
	Tell(ctx context.Context, id int, words, said string) (string, error)
}

// FloorPress is one press the foreman makes for the person.
type FloorPress struct {
	// Action is one of [floorPresses], the job's own word for the offer.
	Action string
	// Words are the person's words that go with the press: an answer, a
	// task's instructions, what to change in a plan, why they decline.
	Words string
	// Choice is the one of a question's listed choices the person chose.
	Choice string
	// Budget is the new budget in USD, for raise_budget.
	Budget float64
	// TurnAt is when the running turn began. The door uses it to tell acts the
	// person has had a chance to read from acts proposed since their words.
	// It is not part of what the person asked for, so it is never printed
	// beside it.
	TurnAt time.Time `json:"-"`
}

// floorPresses is every action a job can offer its person, in the order the
// schema and the description say them. The names are the offers' own
// ([factory.Offer]), so the word a row lists is the word to press.
var floorPresses = []string{
	factory.DoApprove, factory.DoAnswer, factory.DoRejectPlan, factory.DoAcceptStep,
	factory.DoActsApprove, factory.DoActsDecline, factory.DoMerge, factory.DoMarkReady,
	factory.DoRetryPublish, factory.DoResume, factory.DoResumePipeline, factory.DoResumeAuto,
	factory.DoRerunStep, factory.DoStart, factory.DoTask, factory.DoRaiseBudget,
	factory.DoSpendAnswer, factory.DoPublishAnyway,
}

// FloorPressElsewhere are the offers a press does not make: each is a choice
// between two outcomes that the job page shows side by side.
var FloorPressElsewhere = []string{factory.DoSpendAnswer, factory.DoPublishAnyway}

// floorPressList says the presses the door makes, for the description, and
// floorPressElsewhere the sentence about the two it does not.
var (
	floorPressList      = strings.Join(floorPresses[:len(floorPresses)-len(FloorPressElsewhere)], ", ")
	floorPressElsewhere = "The person does " + strings.Join(FloorPressElsewhere, " and ") + " on the job page; you cannot press them."
)

// The refusals said here, once, so the tests and the manual quote the same
// words.
const (
	// FloorPressNoWords is a press asked on a turn the person did not speak in.
	FloorPressNoWords = "Nothing changed: you press a job's action only when the person asks for it, and nothing in this turn came from them."
	// FloorTellNoWords is words for a main agent on such a turn.
	FloorTellNoWords = "Nothing changed: you pass words to a job's main agent only when the person asks, and nothing in this turn came from them."
	floorNoHand      = "Nothing changed: this window cannot press a job's action or reach its main agent; the person can on the job's page."
	// FloorTellMost is the most characters the words passed on may be.
	FloorTellMost = 4000
)

// floorOne finds the one job #n names, or says why there is not one.
func floorOne(ctx context.Context, door FloorDoor, n int) (factory.Item, string) {
	snap, err := floorSnapshot(ctx, door)
	if err != nil {
		return factory.Item{}, "the factory could not be read: " + oneLine(err.Error())
	}
	found := floorFind(snap, n)
	switch {
	case len(found) == 0:
		return factory.Item{}, fmt.Sprintf("Nothing changed: there is no #%d in the factory", n)
	case len(found) > 1:
		return factory.Item{}, fmt.Sprintf("Nothing changed: #%d is on %d repositories (%s)", n, len(found), floorRepos(found))
	}
	return found[0], ""
}

// floorPress presses one offered action of one job, as the person.
func (a *Agent) floorPress(ctx context.Context, item int, p FloorPress) (string, bool, error) {
	hand, ok := a.config.Floor.(FloorHand)
	if !ok {
		return floorNoHand, true, nil
	}
	said := a.turnPersonWords()
	if said == "" {
		return FloorPressNoWords, true, nil
	}
	it, refusal := floorOne(ctx, a.config.Floor, item)
	if refusal != "" {
		return refusal, true, nil
	}
	p.TurnAt = a.managerTurnAt()
	line, err := floorWithin(ctx, floorActWait, func(ctx context.Context) (string, error) {
		return hand.Press(ctx, it.ID, p, said)
	})
	if errors.Is(err, errFloorLate) {
		// NOT "Nothing changed": the press was sent and its answer never
		// came, so whether it landed is exactly what is not known.
		return fmt.Sprintf("%s for %s. It may still take effect: read %s again before asking twice.", floorLateWords(floorActWait), it.Ref(), it.Ref()), true, nil
	}
	if err != nil {
		return oneLine(err.Error()), true, nil
	}
	return line, false, nil
}

// floorTell passes the person's words to one job's main agent and answers
// what it said.
func (a *Agent) floorTell(ctx context.Context, item int, words string) (string, bool, error) {
	hand, ok := a.config.Floor.(FloorHand)
	if !ok {
		return floorNoHand, true, nil
	}
	said := a.turnPersonWords()
	if said == "" {
		return FloorTellNoWords, true, nil
	}
	if len([]rune(words)) > FloorTellMost {
		return fmt.Sprintf("Nothing changed: words for a main agent are at most %d characters.", FloorTellMost), true, nil
	}
	it, refusal := floorOne(ctx, a.config.Floor, item)
	if refusal != "" {
		return refusal, true, nil
	}
	line, err := floorWithin(ctx, FloorTellWait+floorTellGrace, func(ctx context.Context) (string, error) {
		return hand.Tell(ctx, it.ID, words, said)
	})
	if errors.Is(err, errFloorLate) {
		return FloorTellLate(it.Ref()), true, nil
	}
	if err != nil {
		return oneLine(err.Error()), true, nil
	}
	return line, false, nil
}

// FloorTellLate is what the foreman reads when a main agent's turn did not
// end inside [FloorTellWait]. The turn was started, so what it did before it
// was stopped stands.
func FloorTellLate(ref string) string {
	return fmt.Sprintf("%s's main agent did not finish within %d minutes, so its turn was stopped. What it did before that stands: read %s again, and tell the person it did not answer.", ref, int(FloorTellWait/time.Minute), ref)
}
