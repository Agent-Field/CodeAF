package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// ── the custody law, read on its own ────────────────────────────────────────
//
// THE LAW UNDER EVERY TEST IN THIS FILE: WORK THIS CONVERSATION IS STILL HOLDING
// NEVER LEAVES IT. These are the pure readings — a drawing, a ledger of what is
// out, and the two answers the harness takes off them (checkpoint_custody.go).
// The road-level proof that a converted worker is never briefed to own its own
// siblings is `taskcustody_test.go`'s.

// heldPieces is the ledger these tests hand the readings, spelled once so that a
// row of a table says what it is about and nothing about how it is built.
func heldPieces(pieces ...heldPiece) []heldPiece { return pieces }

// theProductionDrawing is the shape the incident actually produced (#567), with
// tasks 4 and 8 out under the conversation. It is kept as two constants because
// both halves are asserted separately: the half that could be handed to somebody
// and the half that could not.
const (
	productionHandable = "finish the tree-7 copy > build and test the merged worktree"
	productionOwn      = "(tasks 4 and 8 still out) > integrate all branches into feat/folder-picker-v2 > " +
		"fire the parallel review propose_tasks > gh PR to origin/dev > make build for the binary"
	productionDrawing = productionHandable + " | " + productionOwn
)

// A DRAWING TRAVELS WITHOUT THE PARTS THAT ARE ABOUT WORK ALREADY OUT.
//
// Every row here is a drawing beside a ledger of what the conversation is
// holding, and what is asserted is the two answers together: the shape that goes
// to a worker, and the remainder that stays. Asserting only the first would pass
// for a harness that quietly dropped half of what the person asked for.
func TestADrawingTravelsWithoutTheWorkThisConversationIsStillHolding(t *testing.T) {
	for _, one := range []struct {
		name      string
		shape     string
		held      []heldPiece
		kept      string
		remainder string
		handsBack bool
	}{
		{
			// THE INCIDENT ITSELF. The first part is a local job any worker in its
			// own copy can finish; the second waits for two pieces it will never be
			// able to see, and then integrates, reviews, opens a pull request and
			// builds — all of it over those two pieces.
			name:      "the drawing the incident produced",
			shape:     productionDrawing,
			held:      heldPieces(heldPiece{4, "alpha branch groundwork"}, heldPiece{8, "beta branch groundwork"}),
			kept:      productionHandable,
			remainder: productionOwn,
		},
		{
			name:      "the scripted drawing the issue names",
			shape:     "finish the local edit > test | (tasks 1 and 2 still out) > integrate their branches > review",
			held:      heldPieces(heldPiece{1, "the first piece"}, heldPiece{2, "the second piece"}),
			kept:      "finish the local edit > test",
			remainder: "(tasks 1 and 2 still out) > integrate their branches > review",
		},
		{
			// A DRAWING THAT IS ENTIRELY THE CONVERSATION'S OWN COMES BACK A
			// HAND-BACK, and the remainder is the whole of what was drawn — the
			// steps behind a single part are not in [readShape]'s reading, so the
			// drawing itself is the only lossless answer.
			name:      "a drawing that is all the conversation's own",
			shape:     "(tasks 1 and 2 still out) > integrate their branches > review",
			held:      heldPieces(heldPiece{1, "the first piece"}, heldPiece{2, "the second piece"}),
			kept:      "",
			remainder: "(tasks 1 and 2 still out) > integrate their branches > review",
			handsBack: true,
		},
		{
			// THE GATHERING STEP GOES WHERE THE PARTS WENT. `land them all` waits on
			// both parts, and one of them is this conversation's, so the step is over
			// work only this conversation can see.
			name:      "a gathering step behind a part that is held",
			shape:     "(finish the local edit | tasks 1 and 2 still out) > land them all",
			held:      heldPieces(heldPiece{1, "the first piece"}, heldPiece{2, "the second piece"}),
			kept:      "finish the local edit",
			remainder: "tasks 1 and 2 still out > land them all",
		},
		{
			// A STAGE READ ON ITS OWN TERMS. No part was withheld, so the trailing
			// step is judged for itself — and it names a piece that is out.
			name:      "only the gathering step is the conversation's",
			shape:     "(write the changelog | write the docs) > integrate task 4",
			held:      heldPieces(heldPiece{4, "alpha branch groundwork"}),
			kept:      "write the changelog | write the docs",
			remainder: "integrate task 4",
		},
		{
			// AND THE STAGES ARE A CHAIN. Keeping `open the PR` while withholding
			// the integration in front of it would hand a worker the end of a
			// sequence whose middle this conversation has not done.
			name:      "a stage standing behind a withheld stage",
			shape:     "(write the docs | write the tests) > integrate task 4 > open the PR",
			held:      heldPieces(heldPiece{4, "alpha branch groundwork"}),
			kept:      "write the docs | write the tests",
			remainder: "integrate task 4 > open the PR",
		},
		{
			// BY NAME AND NOT BY NUMBER. A drawing that quotes the whole of a
			// piece's name is naming that piece as surely as its number does.
			name:      "a piece named by its title",
			shape:     "write the changelog | ship the folder picker rewrite once it lands",
			held:      heldPieces(heldPiece{4, "the folder picker rewrite"}),
			kept:      "write the changelog",
			remainder: "ship the folder picker rewrite once it lands",
		},
		{
			// A BARE NUMBER IS A QUANTITY. `integrate 4 branches` is work, and a
			// reading that withheld it would cost the road the turns it exists for.
			name:  "a number that is a quantity and not a reference",
			shape: "integrate 4 branches | write the docs",
			held:  heldPieces(heldPiece{4, "alpha branch groundwork"}),
			kept:  "integrate 4 branches | write the docs",
		},
		{
			// A WAIT BESIDE REAL WORK, WITH SOMETHING TO BE WAITING FOR. This is
			// #304's pinned control with the ledger no longer empty, and it is the
			// one row where the two files answer differently on purpose.
			name:      "one wait beside real work, with a piece out",
			shape:     "wait for the second | write the summary",
			held:      heldPieces(heldPiece{2, "the second piece"}),
			kept:      "write the summary",
			remainder: "wait for the second",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			drawn := parseCheckpointSketch(one.shape + "\nA sentence naming the letters.")
			got, remainder := drawn.withoutHeldWork(one.held)
			if got.shape != one.kept {
				t.Errorf("the drawing that travels is\n\t%q\nwant\n\t%q", got.shape, one.kept)
			}
			if remainder != one.remainder {
				t.Errorf("the remainder that stays here is\n\t%q\nwant\n\t%q", remainder, one.remainder)
			}
			if got.handsBack != one.handsBack {
				t.Errorf("the reduced drawing reads as hands-back %v, want %v", got.handsBack, one.handsBack)
			}
			// AND THE COUNT AGREES WITH THE SHAPE THAT IS ACTUALLY TRAVELLING. A
			// count carried over from the drawing that was read would split a turn
			// on parts that are no longer in it.
			if got.parts != topLevelParts(got.shape) {
				t.Errorf("the reduced drawing carries %d parts while its shape reads %d",
					got.parts, topLevelParts(got.shape))
			}
			// AND WHAT WAS WITHHELD IS NEVER ALSO HANDED OUT. The two answers
			// partition the drawing; anything in both would be a duty given to a
			// worker AND kept here.
			if one.remainder != "" && got.shape != "" && strings.Contains(got.shape, one.remainder) {
				t.Errorf("the remainder is still inside the drawing that travels:\n\t%q", got.shape)
			}
		})
	}
}

// THE WITHHELD PART IS NOT DROPPED — IT IS THE HEAD'S OWN REMAINDER.
//
// "wait for 4 and 8, integrate, review, open the PR, build" is still exactly what
// the person asked for. It is not work anybody else can take, and it is not work
// that stops being owed: it comes back whole, in the order it was drawn, and the
// transcript the next turn opens on carries it (checkpoint.go's
// [Agent.handOverRunningTurn]).
func TestTheWithheldPartComesBackWholeAsTheConversationsOwnRemainder(t *testing.T) {
	drawn := parseCheckpointSketch(productionDrawing + "\nThe first finishes the copy; the second gathers the pieces.")
	_, remainder := drawn.withoutHeldWork(
		heldPieces(heldPiece{4, "alpha branch groundwork"}, heldPiece{8, "beta branch groundwork"}))
	if remainder != productionOwn {
		t.Fatalf("the remainder is\n\t%q\nwant the coordination part whole and in order\n\t%q",
			remainder, productionOwn)
	}
	// EVERY STEP OF IT, in the order it was drawn. A remainder that kept the wait
	// and lost the pull request would be the harness dropping the tail of what was
	// asked for while looking like it had kept it.
	for _, step := range []string{
		"tasks 4 and 8 still out",
		"integrate all branches into feat/folder-picker-v2",
		"fire the parallel review propose_tasks",
		"gh PR to origin/dev",
		"make build for the binary",
	} {
		if !strings.Contains(remainder, step) {
			t.Errorf("the remainder lost %q:\n\t%s", step, remainder)
		}
	}
	// AND IT REACHES THE TRANSCRIPT UNDER A HEADING, so the turn woken by tasks 4
	// and 8 landing opens on the thing it still owes.
	record := heldRestRecord(remainder)
	if !strings.Contains(record, checkpointOwnRemainderHead) || !strings.Contains(record, productionOwn) {
		t.Errorf("the transcript block does not carry the remainder under its heading:\n\t%s", record)
	}
	// AND NOTHING IS WRITTEN FOR AN EMPTY ONE, which is the emptiness law in the
	// one place where breaking it puts a heading with nothing under it at the top
	// of the next turn's context.
	if got := heldRestRecord("   "); got != "" {
		t.Errorf("a mark that withheld nothing wrote %q into the transcript", got)
	}
}

// A CONVERSATION HOLDING NOTHING IS UNTOUCHED, BYTE FOR BYTE.
//
// This is [TestADrawingOfTheConversationsOwnCoordinationIsNotADivision]'s whole
// table run back through the reduction with an empty ledger. #304's readings are
// the readings this change may not move: without a piece out there is no custody
// to protect, and a drawing rewritten for no reason is a harness editing what it
// did not draw.
func TestADrawingIsUntouchedWhenThisConversationIsHoldingNothing(t *testing.T) {
	for _, one := range []struct {
		name      string
		shape     string
		handsBack bool
		split     bool
	}{
		{"the incident's first drawing", "(the second report > review) | (the third report > review) | (the fourth report > review)", true, false},
		{"the incident's second drawing", "the second > accept | the third > accept | the fourth > accept", true, false},
		{"waiting on each piece", "wait for the second | wait for the third | wait for the fourth", true, false},
		{"the token the ask teaches", "(waiting)", true, false},
		{"awaiting, in the other tense", "awaiting the second | awaiting the third", true, false},
		{"three pieces of work", "A | B | C", false, true},
		{"a verb with something after it", "review the manuscript | write the summary | check the figures against the source", false, true},
		{"one wait beside real work", "wait for the second | write the summary", false, true},
		{"the shape that says nothing is left", "(done)", false, false},
		{"a chain", "A > B > C", false, false},
	} {
		t.Run(one.name, func(t *testing.T) {
			drawn := parseCheckpointSketch(one.shape + "\nA sentence naming the letters.")
			for _, ledger := range [][]heldPiece{nil, {}} {
				got, remainder := drawn.withoutHeldWork(ledger)
				if got != drawn {
					t.Errorf("an empty ledger moved the drawing:\n\tgot  %#v\n\twant %#v", got, drawn)
				}
				if remainder != "" {
					t.Errorf("an empty ledger withheld %q", remainder)
				}
				if got.handsBack != one.handsBack || got.split() != one.split {
					t.Errorf("%q reads as hands-back %v / split %v, want %v / %v",
						one.shape, got.handsBack, got.split(), one.handsBack, one.split)
				}
			}
		})
	}
}

// A NUMBER IS A REFERENCE ONLY WHERE IT STANDS BESIDE THE WORD THIS PROGRAM USES
// FOR THE THING; ANYWHERE ELSE IT IS A QUANTITY.
//
// The reading is asked of two very different texts — one clause of a drawing, and
// a brief of two thousand characters — and the second is why this is positional.
// Ids are small numbers: a document that says `task` once and `run the 4 tests`
// elsewhere would otherwise be blanked, and the carry ladder would drop a rung for
// a brief nobody could fault.
func TestANumberNamesAPieceOnlyWhereItStandsBesideTheNoun(t *testing.T) {
	out := heldPieces(
		heldPiece{4, "the folder picker rewrite"},
		heldPiece{8, "trees"},
	)
	for _, one := range []struct {
		name  string
		text  string
		held  []heldPiece
		names bool
	}{
		{"a number beside the noun", "tasks 4 and 8 still out", out, true},
		{"one number beside the singular noun", "wait for task 4 to land", out, true},
		{"a list held together by joiners", "tasks 1, 4 and 9 are still running", out, true},
		{"a quantity, with the noun elsewhere in the text", "run the 4 tests, then finish the task", out, false},
		{"a joiner that opens the run names nothing", "the task and 4 tests", out, false},
		{"a bare number with no noun at all", "integrate 4 branches", out, false},
		{"a title quoted whole", "ship the folder picker rewrite once it lands", out, true},
		{"a title quoted in part", "ship the folder picker", out, false},
		{"a one-word title matches nothing", "walk through the trees", out, false},
		{"an empty ledger names nothing", "tasks 4 and 8 still out", nil, false},
		{"empty text names nothing", "   ", out, false},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := namesHeldWork(one.text, one.held); got != one.names {
				t.Errorf("%q names held work %v, want %v", one.text, got, one.names)
			}
		})
	}
}

// THE LEDGER IS ROOTS THIS CONVERSATION IS STILL WAITING ON, AND NOTHING ELSE.
//
// A nested piece belongs to the worker that commissioned it and is already
// visible to it — putting it here would withhold work over a piece nobody in this
// conversation ever handed out. A settled piece has reported, and a drawing that
// names it is naming a fact rather than asking anybody to wait for one.
func TestTheLedgerIsRootsThisConversationIsStillWaitingOn(t *testing.T) {
	graph := newTaskGraph()
	add := func(id, parent uint64, title string, state TaskState) {
		graph.nodes[id] = &TaskNode{
			graph:  graph,
			id:     id,
			parent: parent,
			state:  state,
			spec:   taskSpec{title: title},
		}
		graph.order = append(graph.order, id)
	}
	add(4, 0, "alpha branch groundwork", TaskRunning)
	add(8, 0, "beta branch groundwork", TaskRunning)
	add(9, 4, "a piece task 4 commissioned itself", TaskRunning)
	add(2, 0, "the piece that already reported", TaskDone)

	got := graph.piecesStillOut()
	want := []heldPiece{{4, "alpha branch groundwork"}, {8, "beta branch groundwork"}}
	if len(got) != len(want) {
		t.Fatalf("the ledger holds %v, want %v", got, want)
	}
	for index, piece := range want {
		if got[index] != piece {
			t.Errorf("the ledger's piece %d is %v, want %v", index, got[index], piece)
		}
	}
	// AND A CONVERSATION THAT NEVER GROOMED A TASK HOLDS NOTHING, rather than
	// building a graph to answer one question.
	var missing *TaskGraph
	if held := missing.piecesStillOut(); held != nil {
		t.Errorf("a graph that never existed holds %v", held)
	}
}

// A READING WRITTEN BACK OUT IS READ AS THE SAME READING.
//
// It is the property everything downstream rests on. The reduced drawing is
// counted, read for coordination and handed to a division through [readShape], so
// a render that came back reading differently from what it was built from would
// mean the harness split a turn on one set of parts and handed out another.
func TestARenderedReadingIsReadBackAsTheSameReading(t *testing.T) {
	for _, shape := range []string{
		"A | B | C",
		"A > B > C",
		"A > (B | C)",
		"A > B | C > D",
		"(A | B | C) > D",
		"(A | B) > C",
		"one job and no separator at all",
		"(waiting)",
		productionDrawing,
		"finish the local edit > test | (tasks 1 and 2 still out) > integrate their branches > review",
	} {
		t.Run(shape, func(t *testing.T) {
			reading := readShape(shape)
			again := readShape(reading.render())
			if !equalStrings(again.parts, reading.parts) {
				t.Errorf("%q renders to %q, which reads as parts %q, want %q",
					shape, reading.render(), again.parts, reading.parts)
			}
			if !equalStrings(again.after, reading.after) {
				t.Errorf("%q renders to %q, whose stages read %q, want %q",
					shape, reading.render(), again.after, reading.after)
			}
		})
	}
	// AND A READING WITH NOTHING IN IT RENDERS AS NOTHING, which is what makes an
	// entirely-withheld drawing read as a hand-back everywhere downstream.
	if got := (shapeReading{}).render(); got != "" {
		t.Errorf("an empty reading renders as %q", got)
	}
	// AND THE CLAIM ABOVE COVERS ONLY READINGS [readShape] PRODUCED, which is the
	// narrow version and the true one. Stages with NO part in front of them are a
	// reading nobody drew: [checkpointSketch.withoutHeldWork] makes one for the
	// conversation's OWN remainder, it renders as bare text, and that text reads
	// back as a PART. Nothing downstream re-parses a remainder — it is a sentence
	// for a person and a line in a file — so this is pinned as what it is rather
	// than claimed as a round trip.
	own := shapeReading{after: []string{"integrate task 4", "open the PR"}}
	if got := own.render(); got != "integrate task 4 > open the PR" {
		t.Errorf("the conversation's own remainder renders as %q", got)
	}
	back := readShape(own.render())
	if len(back.parts) != 1 || len(back.after) != 0 {
		t.Errorf("an after-only reading reads back as %d parts and %d stages; "+
			"if that has changed, the narrowed claim in render() can be widened",
			len(back.parts), len(back.after))
	}
}

// A HANDOVER STARTS NOTHING WHEN EVERYTHING IT COULD CARRY IS ABOUT WORK ALREADY
// OUT.
//
// THE FLOOR OF THE BRIEF LADDER IS THE HOLE THIS CLOSES. The two rungs above it
// are written by models and are blanked when they assign work this conversation
// is holding; the floor is the PERSON'S OWN SENTENCE, and nobody on this road may
// edit that. In the measured incident the sentence WAS the coordination — so the
// ladder blanked both upper rungs, came to rest on the floor, and started a worker
// on exactly the duty the reduction had just taken away.
//
// So the road declines instead. Nothing is edited, nothing is admitted, and the
// whole of what was drawn stays here for the turn that wakes when those pieces
// land. The fixture drives the real write seam with two pieces genuinely out, so
// the ledger it answers to is the engine's own.
func TestAHandoverStartsNothingWhenEveryRungIsAboutWorkAlreadyOut(t *testing.T) {
	// THE TWO DOORS INTO THE SAME REFUSAL, and the first row is the incident's own
	// sentence. `land everything once the other two report` names neither piece —
	// no number beside the noun, no title quoted whole — so nothing that READS
	// English closes it. What closes it is a fact the harness already holds: the
	// drawing out of this same turn had to be divided, and a sentence typed before
	// that reading is by construction about the mixture.
	for _, one := range []struct {
		name  string
		asked string
	}{
		{"the sentence the incident was actually typed with", "land everything once the other two report"},
		{"a sentence that names the pieces outright", "land everything once tasks 1 and 2 report"},
	} {
		t.Run(one.name, func(t *testing.T) {
			const draft = "Wait for tasks 1 and 2 to report, then land everything together."
			const written = "Wait for tasks 1 and 2 to land, then integrate their branches and open the pull request."

			path := filepath.Join(t.TempDir(), "session.jsonl")
			completer := &scriptedCompleter{steps: custodyWritingSteps(12, custodyMixedSketch, draft, written)}
			agent := custodyAgent(t, completer, func(config *Config) { config.SessionFile = path })
			graph := stubbedGraph(agent, custodyRunner(t))
			first := handOutPiece(t, graph, 1, "the folder picker rail")
			second := handOutPiece(t, graph, 2, "the settings pane copy")

			events, err := agent.Submit(context.Background(), one.asked)
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)

			// NOTHING WAS ADMITTED. The two pieces that were out are still the only
			// roots in the graph, and no third one was started to coordinate them.
			graph.mu.Lock()
			var admitted []uint64
			for _, id := range graph.order {
				if node := graph.nodes[id]; node != nil && node.parent == 0 &&
					id != first.id && id != second.id {
					admitted = append(admitted, id)
				}
			}
			graph.mu.Unlock()
			if len(admitted) != 0 {
				t.Fatalf("a task was admitted on a brief about work already out: %v", admitted)
			}

			// AND THE PERSON IS TOLD, in the register of the note that stands where
			// this one does.
			var told bool
			for _, event := range collected {
				if event.Kind != EventNotice {
					continue
				}
				if strings.Contains(event.Text, checkpointHeldWholeNote) {
					told = true
				}
				if strings.Contains(event.Text, writeSeamNote) {
					t.Errorf("the person was told the work was moving, and it was not: %q", event.Text)
				}
			}
			if !told {
				t.Error("nothing said why no task started")
			}

			// AND NOTHING WAS WRITTEN INTO THE TRANSCRIPT, which is what tells this
			// ending apart from the moved one. The turn carries on and the model
			// still holds the work; an assistant message it did not write, appearing
			// in the middle of its own context, is a thing this road has never done
			// — and the two other non-moving endings record nothing for the same
			// reason. What stayed is in the file, below.
			kept := agentTranscript(agent)
			for _, line := range []string{checkpointOwnRemainderHead, checkpointHeldWholeNote} {
				if strings.Contains(kept, line) {
					t.Errorf("a turn that was not sealed had %q written into its own context:\n%s",
						line, kept)
				}
			}

			// AND THE FILE RECORDS WHAT WAS DRAWN AND WHAT STAYED, which is the pair
			// that tells a reader afterwards that a worker opened on less than was
			// drawn — or, here, on nothing at all.
			marks := journaledMarks(t, path)
			if len(marks) == 0 {
				t.Fatal("no mark was journaled")
			}
			last := marks[len(marks)-1]
			if last.Sketch != custodyMixedShape {
				t.Errorf("the file recorded the drawing as %q, want the reader's own line %q",
					last.Sketch, custodyMixedShape)
			}
			if !strings.Contains(last.Kept, custodyHeldPart) {
				t.Errorf("the file recorded what stayed as %q, want the coordination half", last.Kept)
			}
		})
	}
}

// agentTranscript is the conversation as the next turn would open on it.
func agentTranscript(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	var out strings.Builder
	for _, message := range agent.messages {
		for _, part := range message.Content {
			out.WriteString(part.Text)
			out.WriteString("\n")
		}
	}
	return out.String()
}
