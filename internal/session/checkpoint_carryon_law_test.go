package session

// THE LAW THIS FILE HOLDS: A TURN LEAVES ONLY WHEN IT CANNOT CONTINUE.
//
// The ceiling used to be an unconditional hand-over, which priced a turn in
// ROUNDS and then spent a CONTEXT — two currencies that were never compared. The
// rung in front of it (checkpoint.go's [Agent.checkpointCeiling]) is what closed
// that, and the three things it rests on are each a rule somebody can break by
// accident, so each has a test here rather than only a comment.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// TestTheCeilingAsksWhetherTheTurnCanCarryOnBeforeItMoves is the STRUCTURAL half:
// the ceiling may not reach the mover without having asked.
//
// IT READS THE TREE rather than driving a turn because what it is protecting is
// an ORDER. A future edit that moves the hand-over above the question, or drops
// the question entirely, would leave every behavioural test green — the ceiling
// would still move a turn, which is what those assert — and would put the
// measured defect straight back.
func TestTheCeilingAsksWhetherTheTurnCanCarryOnBeforeItMoves(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "checkpoint.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing checkpoint.go: %v", err)
	}
	var ceiling *ast.FuncDecl
	ast.Inspect(file, func(node ast.Node) bool {
		if function, ok := node.(*ast.FuncDecl); ok && function.Name.Name == "checkpointCeiling" {
			ceiling = function
		}
		return true
	})
	if ceiling == nil {
		t.Fatal("checkpointCeiling is gone from checkpoint.go — the law is reading the wrong tree")
	}
	askedAt, movedAt := -1, -1
	ast.Inspect(ceiling, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "checkpointCanCarryOn":
			if askedAt < 0 {
				askedAt = set.Position(call.Pos()).Offset
			}
		case "handOverRunningTurn":
			if movedAt < 0 {
				movedAt = set.Position(call.Pos()).Offset
			}
		}
		return true
	})
	if askedAt < 0 {
		t.Fatal("the ceiling no longer asks whether the turn can carry on: a turn is being moved " +
			"on a count of rounds alone, which is the defect this rung was built to close")
	}
	if movedAt >= 0 && askedAt > movedAt {
		t.Error("the ceiling moves the turn before asking whether it can carry on — " +
			"the question has to stand in front of the hand-over or it decides nothing")
	}
}

// TestTheCeilingMakesNoModelCallOfItsOwn is the other half of the same law, and
// it is what lets the rung stand in the middle of a running turn at all.
//
// A question asked at every ceiling that cost a model call would be a wait the
// person feels, on a road whose usual answer is now "carry on" — which is the
// shape sidecar_law_test.go forbids everywhere else. The pass this rung runs is
// [Agent.compact], which has said in its own doc comment since it was written
// that it MAKES NO MODEL CALL AT ALL.
func TestTheCeilingMakesNoModelCallOfItsOwn(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "checkpoint.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing checkpoint.go: %v", err)
	}
	// The two verbs that reach a provider from this package's auxiliary seam. A
	// third would be a third door, which auxiliary.go's own law forbids.
	asking := map[string]bool{"callRole": true, "callRoleChecked": true, "readMark": true}
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Name.Name != "checkpointCanCarryOn" {
			return true
		}
		ast.Inspect(function, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && asking[selector.Sel.Name] {
				t.Errorf("the ceiling's carry-on question calls %s: it stands in front of a running "+
					"turn and its usual answer is to carry on, so it may not cost a model call",
					selector.Sel.Name)
			}
			return true
		})
		return false
	})
}

// TestAMeterThatBoughtRoomClimbsTheSameLadderFromWhereItBoughtIt is the meter's
// own half, and it is here because the re-arm is the thing that keeps the rung
// BOUNDED. A turn that compacts must pay the whole ladder again — the price, then
// the doubling — or the ceiling becomes a rung that lets a turn run forever.
func TestAMeterThatBoughtRoomClimbsTheSameLadderFromWhereItBoughtIt(t *testing.T) {
	meter := &checkpointMeter{}
	for meter.marks < checkpointMarks {
		if meter.round(true) == checkpointMarks {
			break
		}
	}
	ceiling := meter.rounds
	if ceiling != checkpointMarkAt(checkpointMarks) {
		t.Fatalf("the ceiling fired at round %d, not at the ladder's own last rung %d",
			ceiling, checkpointMarkAt(checkpointMarks))
	}
	meter.compacted()
	// THE NEXT MARK IS A FULL PRICE AWAY, counted from where the room was bought.
	for round := 1; round < checkpointPrice; round++ {
		if mark := meter.round(true); mark != 0 {
			t.Fatalf("mark %d fired %d rounds after the compaction, inside the price of %d",
				mark, round, checkpointPrice)
		}
	}
	if mark := meter.round(true); mark != 1 {
		t.Fatalf("the first rung after a compaction answered %d, not 1 — the ladder did not "+
			"go back to its foot", mark)
	}
	if meter.rounds != ceiling+checkpointPrice {
		t.Fatalf("the first rung after a compaction stood at round %d, not at %d",
			meter.rounds, ceiling+checkpointPrice)
	}
	// AND THE CEILING IS REACHABLE AGAIN, at the same span it always was.
	for meter.round(true) != checkpointMarks {
		if meter.rounds > ceiling+checkpointMarkAt(checkpointMarks)+1 {
			t.Fatal("the ceiling never fired again after a compaction — a turn that bought room " +
				"is a turn nothing meets any more")
		}
	}
	if meter.rounds != ceiling+checkpointMarkAt(checkpointMarks) {
		t.Fatalf("the second ceiling stood at round %d, not a whole ladder past the first (%d)",
			meter.rounds, ceiling+checkpointMarkAt(checkpointMarks))
	}
}

// TestTheMastermindTierRefusesAModelTheCrewClassesBelowIt is the fitness law.
//
// IT IS DRIVEN FROM THE CREW TABLE ITSELF and names no model, which is the whole
// point of deriving the rule from that table: a preset that moves takes this test
// with it, where a test spelling an id would have to be found and edited.
func TestTheMastermindTierRefusesAModelTheCrewClassesBelowIt(t *testing.T) {
	worker, ok := crewSeatFor(t, config.ModelTierWorker)
	if !ok {
		t.Skip("no preset seats a worker, so there is nothing to refuse")
	}
	if roles.TierFits(worker, roles.TierMastermind) {
		t.Errorf("the mastermind tier accepts %q, which every shipped preset seats at worker "+
			"or below — a row that exists is not a model that can answer", worker)
	}
	if !roles.TierFits(worker, roles.TierWorker) {
		t.Errorf("the worker tier refuses %q, which is the model it is seated at", worker)
	}
	// AND A MODEL NOBODY SHIPS IS NOT REFUSED. This package has an opinion about
	// what it knows and none at all about what it does not.
	if !roles.TierFits("some-vendor/a-model-nobody-here-ships", roles.TierMastermind) {
		t.Error("a model no preset names was refused: the rule is deriving a verdict " +
			"from silence, which is a list in another form")
	}
}

// crewSeatFor answers a model the shipped crew seats at exactly this class and
// never higher — read out of the table rather than written down here.
func crewSeatFor(t *testing.T, want string) (string, bool) {
	t.Helper()
	for _, preset := range config.CrewPresets {
		models, ok := config.CrewModels(preset)
		if !ok {
			continue
		}
		candidate := strings.TrimSpace(models[want])
		if candidate == "" {
			continue
		}
		if seat, known := config.CrewSeat(candidate); known && seat == want {
			return candidate, true
		}
	}
	return "", false
}

// ── and the rung, driven ────────────────────────────────────────────────────

// TestAnAnswerWithRoomLeftCompactsAtTheCeilingAndCarriesOn is the measured
// defect, turned into a test.
//
// THE SHAPE IS THE 2026-09-11 CENSUS EXACTLY: an answer that runs the whole
// ladder on a window with room left in it. Before the rung in front of the move
// existed, this ended with a cold worker started on a request the conversation
// had already half answered.
func TestAnAnswerWithRoomLeftCompactsAtTheCeilingAndCarriesOn(t *testing.T) {
	const asked = "add a hover effect on the tasks-table column labels"
	const answered = "the column labels dim on hover now"

	rounds := checkpointMarkAt(checkpointMarks)
	steps := append(grindingSteps(rounds+checkpointSlack, checkpointChainSketch, checkpointNothingLeft),
		finalAnswer(answered))
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps}, checkpointRoomToCarryOn,
		func(config *Config) { config.SessionFile = path })
	graph := stubbedGraph(agent, func(node *TaskNode) { node.finish("done", nil, "", "") })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// NOBODY ELSE WAS GIVEN THE WORK.
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started out of an answer that had room to carry on", count)
	}
	// AND THE PERSON WAS NOT TOLD THEIR ANSWER WAS MOVING, because it was not.
	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Errorf("the person was told their answer was being moved; notices were %q",
			noticeTexts(collected))
	}
	// AND THE ANSWER IS THE ONE THEY ASKED FOR.
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
		t.Errorf("the turn ended as a %s saying %q, want the answer it was about to give",
			last.Role, messageText(last))
	}
	// AND THE FILE SAYS WHAT HAPPENED. A rung that let a turn carry on silently
	// would re-open the hole the ceiling row was added to close: a run that moved
	// and a run that never fired read identically.
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 1 || ceilings[0].Decision != checkpointCeilingCompacted {
		t.Fatalf("the ceiling journaled %+v, want one row saying %q",
			ceilings, checkpointCeilingCompacted)
	}
	if ceilings[0].TaskID != 0 {
		t.Errorf("a ceiling that carried on named task %d", ceilings[0].TaskID)
	}
}

// TestAWideSketchStillMovesAnAnswerThatHasRoomLeft is the other half of the same
// rung, and it is the one that keeps room from becoming an excuse.
//
// ROOM IS ONLY EVER A REASON TO STAY. A drawing with parts that do not wait on
// each other is work more than one pair of hands can hold, and no amount of free
// window changes that — so the split road has to survive a turn that could
// perfectly well have carried on.
func TestAWideSketchStillMovesAnAnswerThatHasRoomLeft(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const brief = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack+2, checkpointSplitSketch, brief)}
	agent := checkpointAgent(t, completer, checkpointRoomToCarryOn,
		func(config *Config) { config.Divide = true })
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted out of a wide drawing, want exactly one — a free "+
			"window is a reason to stay and never a reason to refuse width", count)
	}
}
