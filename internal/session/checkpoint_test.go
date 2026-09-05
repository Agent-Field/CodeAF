package session

// THE FOURTH MOMENT: a turn priced while it is running, and READ BY SOMEBODY WHO
// IS NOT RUNNING IT.
//
// The three that existed before all decide with no evidence in front of them —
// the prompt's law, the route judge's read of a request, and a proposal the model
// remembers to make. This one reads the only thing that is a fact mid-turn: what
// the answer has cost so far, in finished tool rounds, against what handing it
// over costs — and then, at each mark, has a sidecar on the tier that thinks
// sketch what is actually left.
//
// So these tests pin the things that could quietly stop being true: that the
// meter fires at the marks and nowhere else, that the ask is the wording the
// benchmark chose and the shapes are read the way the benchmark scored them, that
// the running model is never sent a word of any of it, that a sketch with parts
// in it hands the turn over with the parts named and the road armed by a
// judgement, that a sketch saying "one job" costs the turn nothing at all, that
// every failure is a carry-on, that the ceiling still fires regardless, and that
// the turns which must never be checkpointed never are.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the meter ───────────────────────────────────────────────────────────────

// THE MARKS ARE WHERE THE POLICY SAYS THEY ARE, AND NOTHING FIRES BETWEEN THEM.
//
// This is the deterministic half of the whole mechanism: no model, no content, no
// clock. A counter and two constants decide, so a drift in either is visible here
// before it is visible anywhere a person could be interrupted by it.
func TestTheMeterFiresOnlyAtTheGeometricMarks(t *testing.T) {
	want := map[int]int{}
	at := checkpointPrice
	for mark := 1; mark <= checkpointMarks; mark++ {
		want[at] = mark
		at *= checkpointRatio
	}
	if len(want) != checkpointMarks {
		t.Fatalf("two marks share a round, so the ladder is not geometric: %v", want)
	}

	meter := &checkpointMeter{}
	// Well past the last mark, because the bound is the point: a turn that runs
	// for ever must not be interrupted for ever.
	last := at * checkpointRatio
	for round := 1; round <= last; round++ {
		got := meter.round(true)
		if expected, marked := want[round]; marked {
			if got != expected {
				t.Fatalf("round %d gave mark %d, want %d", round, got, expected)
			}
			continue
		}
		if got != 0 {
			t.Fatalf("round %d gave mark %d, but no mark stands there", round, got)
		}
	}
	if meter.rounds != last {
		t.Errorf("the meter counted %d rounds of %d", meter.rounds, last)
	}
	if meter.marks != checkpointMarks {
		t.Errorf("the meter climbed to %d of %d marks", meter.marks, checkpointMarks)
	}
}

// AND A RACED YES PULLS THE FIRST RUNG DOWN AND LEAVES THE REST WHERE THEY WERE.
//
// This is the whole of what the pre-turn read can still do to a turn
// (route_judge.go's [Agent.routeTriage]): it noticed early that this one is worth
// LOOKING at, so the look happens at the boundary its verdict landed on. It is
// not a reason to look oftener, so the second mark and the ceiling do not move —
// and the ceiling in particular is the recovery bound the whole fail-open design
// leans on.
func TestATightenedMeterMovesOnlyTheFirstMark(t *testing.T) {
	meter := &checkpointMeter{}
	// Two ordinary rounds, then the race's verdict lands at the third boundary.
	for round := 1; round <= 2; round++ {
		if mark := meter.round(true); mark != 0 {
			t.Fatalf("round %d fired mark %d before the price", round, mark)
		}
	}
	meter.tighten(routeVerdict{Work: true, Wide: true, Acceptance: "the four pieces exist"})

	if mark := meter.round(true); mark != 1 {
		t.Fatalf("the boundary the verdict landed on gave mark %d, want the first mark", mark)
	}
	// AND THE VERDICT IS KEPT, because whatever task eventually starts out of this
	// turn is the task those two readers were reading about.
	if !meter.raced.Wide || meter.raced.Acceptance != "the four pieces exist" {
		t.Errorf("the race's own reading was dropped: %+v", meter.raced)
	}
	// THE LATER RUNGS ARE THE ORDINARY ONES.
	for round := 4; round < checkpointMarkAt(2); round++ {
		if mark := meter.round(true); mark != 0 {
			t.Fatalf("round %d fired mark %d; the second rung stands at %d",
				round, mark, checkpointMarkAt(2))
		}
	}
	if mark := meter.round(true); mark != 2 {
		t.Fatalf("round %d gave mark %d, want the second mark at the ordinary rung",
			checkpointMarkAt(2), mark)
	}
	for round := checkpointMarkAt(2) + 1; round < checkpointMarkAt(checkpointMarks); round++ {
		if mark := meter.round(true); mark != 0 {
			t.Fatalf("round %d fired mark %d; the ceiling stands at %d",
				round, mark, checkpointMarkAt(checkpointMarks))
		}
	}
	if mark := meter.round(true); mark != checkpointMarks {
		t.Fatalf("the ceiling gave mark %d at round %d, want %d",
			mark, checkpointMarkAt(checkpointMarks), checkpointMarks)
	}
}

// AND A YES THAT LANDS AFTER THE FIRST MARK HAS NOTHING LEFT TO TIGHTEN — but its
// verdict is still about this work, so it is still kept.
func TestALateTightenKeepsTheVerdictAndMovesNothing(t *testing.T) {
	meter := &checkpointMeter{}
	for round := 1; round <= checkpointMarkAt(1); round++ {
		meter.round(true)
	}
	if meter.marks != 1 {
		t.Fatalf("the meter is at %d marks, want the first one spent", meter.marks)
	}
	meter.tighten(routeVerdict{Work: true, Wide: true})
	if meter.firstAt != 0 {
		t.Errorf("a spent first mark was pulled down to round %d", meter.firstAt)
	}
	if !meter.raced.Wide {
		t.Error("a late verdict's reading of breadth was thrown away")
	}
}

// THE PRICE IS A FLOOR AND THE LADDER IS SHORT. Both are policy rather than
// arithmetic, and both are the kind of number somebody tunes without reading the
// argument for it, so they are pinned to the band the design was chosen in.
func TestTheHandoffPriceStaysAFloorAndTheLadderStaysShort(t *testing.T) {
	if checkpointPrice < 8 || checkpointPrice > 12 {
		t.Errorf("the handoff price is %d rounds; the fixed cost it is counted off — the ten git "+
			"commands that open and close a task's worktree — puts it between 8 and 12", checkpointPrice)
	}
	if checkpointRatio < 2 {
		t.Errorf("the ratio is %d, so the marks do not get dearer and the harness measures its own "+
			"impatience rather than the turn's cost", checkpointRatio)
	}
	if checkpointMarks != 3 {
		t.Errorf("a turn gets %d marks; two readings and a ceiling is the whole design, and a third "+
			"reading is the harness talking to itself (looped.go reached the same number)", checkpointMarks)
	}
	// AND THE SIDECAR IS BOUNDED, because it stands at a step boundary of a turn
	// somebody is watching. An unbounded read here would be a person waiting on a
	// question they never asked.
	if checkpointSketchWindow <= 0 || checkpointSketchWindow > time.Minute {
		t.Errorf("the mark's read gets %s; it holds up the next round of tool calls, so it is a "+
			"person's patience and not a generous bound", checkpointSketchWindow)
	}
}

// ── what the sidecar is asked ───────────────────────────────────────────────

// THE ASK IS THE MEASURED WORDING, PINNED WORD FOR WORD.
//
// It is variant C of four scored off-policy over 240 completions
// (bench/oneroad/replay/RESULTS.md), and it won on the one thing that matters
// here: it left nothing unanswered on either model, where every phrasing that
// asked for a JUDGEMENT was ignored on a quarter to a half of the turns. The
// harness parses what comes back, so a reworded ask is a contract with one party.
func TestTheSketchAskIsTheMeasuredWording(t *testing.T) {
	// THE GRAMMAR HALF IS VARIANT C WORD FOR WORD. What the reader is shown moved
	// — a digest of the work rather than the transcript — so the framing sentence
	// in front of this one moved with it, and the parser is pinned to everything
	// from here on.
	const grammar = "independent parts separated by ' | ', ordered steps joined by ' > '. " +
		"Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. " +
		"Nothing else on that line. Then one sentence saying what each letter is."
	if !strings.Contains(checkpointSketchAsk, grammar) {
		t.Fatalf("the mark's ask reads:\n%s\nand no longer carries the measured grammar:\n%s",
			checkpointSketchAsk, grammar)
	}
	// AND IT IS ANCHORED TO THE ASK, which is what the digest put in front of it:
	// "what remains" of a conversation is a different question from what remains
	// of what the person actually wanted.
	if !strings.Contains(checkpointSketchAsk, "what remains of the ask") {
		t.Errorf("the ask never anchors the question to what was asked:\n%s", checkpointSketchAsk)
	}
	// AND IT TEACHES THE READER HOW TO SAY NOTHING IS LEFT. The ceiling's drop now
	// needs this answer ([checkpointSketch.saysDone]), and a token the question
	// never named is one the reader has no reason to write.
	if !strings.Contains(checkpointSketchAsk, "(done)") {
		t.Errorf("the ask never names the shape that means the work is finished:\n%s", checkpointSketchAsk)
	}
	// IT CARRIES NO THRESHOLD. This is task_escalation_test.go's pin on
	// prompts/system.md applied here: a number invites the reader to answer about
	// the number instead of about the work in front of it. The example shapes are
	// letters for exactly that reason.
	if strings.ContainsAny(checkpointSketchAsk, "0123456789") {
		t.Errorf("the ask carries a number, so it reads as a threshold:\n%s", checkpointSketchAsk)
	}
	// AND IT NAMES NOTHING ABOUT THE KIND OF WORK. aforge is a general harness: a
	// research sweep, a writing project and a mechanical change are one shape of
	// problem to this question.
	for _, narrow := range []string{
		"file", "code", "repo", "test", "commit", "function", "package",
	} {
		if strings.Contains(strings.ToLower(checkpointSketchAsk), narrow) {
			t.Errorf("the ask says %q, which narrows a general question to coding work:\n%s",
				narrow, checkpointSketchAsk)
		}
	}
	// AND IT ASKS FOR THE LEGEND, which is the half that reaches the worker. The
	// shape alone is what the harness parses and is useless to anybody who has to
	// do the work.
	if !strings.Contains(checkpointSketchAsk, "what each letter is") {
		t.Errorf("the ask never asks what the letters are, so a split hands over a shape "+
			"nobody can read:\n%s", checkpointSketchAsk)
	}
	// AND THE SEPARATOR IT TEACHES IS THE ONE THE HARNESS READS.
	if !strings.Contains(checkpointSketchAsk, "' | '") {
		t.Errorf("the ask never names the separator the parser splits on:\n%s", checkpointSketchAsk)
	}
}

// AND THE SHAPES ARE READ THE WAY THE BENCHMARK SCORED THEM.
//
// The rules, stated once here and once in [topLevelParts]: what could be started
// NOW is what counts. A ' | ' is what makes a part and it binds looser than an
// arrow, a chain is one job, and a fork BEHIND a step is one job too — because at
// this mark the work in front of the turn is that first step. Reading nested pipes
// as width was the loose scoring; the strict reading took the mastermind's trap
// accuracy from 58% to 100% on the same answers.
//
// AND THE PARTS-THEN-GATHER SHAPE IS A SPLIT, which is the reading the replay
// bought: `(A | B | C) > D` is what kimi actually draws for a batch of jobs, 6 of
// 6 at round ten, and reading it as one bracketed group took the batch's recall to
// 2 of 12 (bench/oneroad/replay/RESULTS-2.md).
func TestTheShapeIsReadAsWhatCouldBeStartedNow(t *testing.T) {
	for _, c := range []struct {
		shape string
		parts int
		split bool
		after []string
		why   string
	}{
		{"A | B | C", 3, true, nil, "three parts with nothing in front of them"},
		{"A > B > C", 1, false, nil, "a chain: the second step cannot begin until the first is done"},
		{"A > (B | C)", 1, false, nil, "the fork is behind a step that has not happened yet"},
		{"(A | B) > C", 2, true, []string{"C"}, "two jobs that can start now, and one step that gathers them"},
		{"(A | B | C) > D", 3, true, []string{"D"}, "the shape a reader draws for a batch: the parts, then the gather"},
		{"(A | B | C | D) > E", 4, true, []string{"E"}, "the same, however many parts are inside the bracket"},
		{"Validation | (Arithmetic & Currency) > Shared updates", 2, true, nil, "a real top-level split"},
		{"A > B > E | C > D > F", 2, true, nil, "two chains that do not wait on each other, and nothing after them"},
		{"(done)", 1, false, nil, "one atom and no separator at all"},
		{"I will keep going with the parser rewrite.", 1, false, nil, "prose is one part"},
		{"", 0, false, nil, "nothing was drawn"},
		{"|", 0, false, nil, "a separator with nothing beside it is not a part"},
		{"| A", 1, false, nil, "the empty side does not count"},
	} {
		got := topLevelParts(c.shape)
		if got != c.parts {
			t.Errorf("%q read as %d parts, want %d — %s", c.shape, got, c.parts, c.why)
		}
		if split := (checkpointSketch{parts: got}).split(); split != c.split {
			t.Errorf("%q decided split=%v, want %v — %s", c.shape, split, c.split, c.why)
		}
		// AND WHAT WAITS BEHIND ALL OF THE PARTS IS THE PARENT'S OWN STEP, never a
		// part and never lost (task_divide_sketch.go's afterParts).
		if after := readShape(c.shape).after; !equalStrings(after, c.after) {
			t.Errorf("%q leaves %v after the parts, want %v — %s", c.shape, after, c.after, c.why)
		}
	}
}

// AND THE ANSWER IS READ AS A LINE AND A LEGEND, WHATEVER IT IS DRESSED IN.
func TestTheSketchIsTheFirstLineAndEverythingUnderItIsTheLegend(t *testing.T) {
	sketch := parseCheckpointSketch("`A | B | C`\nA is the workflow; B is arithmetic; C is currency.")
	if sketch.shape != "A | B | C" {
		t.Errorf("the shape came out as %q, so a code span defeated the parser", sketch.shape)
	}
	if !strings.Contains(sketch.legend, "A is the workflow") {
		t.Errorf("the legend came out as %q", sketch.legend)
	}
	if !sketch.split() {
		t.Error("three top-level parts did not decide a split")
	}
	// A leading blank line is a line the reader left, not the shape.
	if got := parseCheckpointSketch("\n\nA > B\nA is the read, B is the fix.").shape; got != "A > B" {
		t.Errorf("the shape came out as %q, want the first line with anything in it", got)
	}
	// AND GARBAGE IS A CARRY-ON. Every failure shape lands in the same place: no
	// drawing, no parts, no split.
	for _, junk := range []string{"", "   ", "\n\n", dsmlSentinel} {
		sketch := parseCheckpointSketch(junk)
		if sketch.split() {
			t.Errorf("%q was read as a split", junk)
		}
	}
}

// AND THE SKETCH HEADS THE BRIEF THE WORKER OPENS ON.
func TestTheSketchStandsAtTheHeadOfTheDowry(t *testing.T) {
	sketch := parseCheckpointSketch(checkpointSplitSketch)
	brief := sketch.head("what is left, and everything this turn already found out")
	if !strings.HasPrefix(brief, "WHAT IS LEFT, AS PARTS: A | B | C") {
		t.Fatalf("the brief does not open on the parts:\n%s", brief)
	}
	if !strings.Contains(brief, "the validation workflow") {
		t.Errorf("the legend did not reach the worker:\n%s", brief)
	}
	if !strings.Contains(brief, "everything this turn already found out") {
		t.Errorf("the dowry was lost under the sketch:\n%s", brief)
	}
	// AND A SKETCH NOBODY DREW CHANGES NOTHING AT ALL.
	if got := (checkpointSketch{}).head("the dowry alone"); got != "the dowry alone" {
		t.Errorf("an empty sketch rewrote the brief as %q", got)
	}
}

// ── the lines a person reads ────────────────────────────────────────────────

// THE CEILING'S LINE.
//
// Pinned as an exact string rather than as a shape, exactly as the mid-answer
// handoff line is: somebody reads this on a turn they did not ask to be
// interrupted on, and the wording IS the feature.
func TestTheCeilingLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "this is running long · moving it to a task that is watched and can split"
	if checkpointCeilingNote != want {
		t.Fatalf("the ceiling line reads %q, want %q", checkpointCeilingNote, want)
	}
	inTheHouseRegister(t, checkpointCeilingNote)
}

// AND THE SPLIT'S LINE, WHICH IS THE THIRD IN THE FAMILY AND SAYS SOMETHING THE
// OTHER TWO CANNOT.
//
// The ceiling has watched an answer outrun its own price. This has had somebody
// read the work and draw independent parts out of it, and at the first mark the
// turn may be no time at all — so a line claiming it had run long would be a lie,
// exactly as the race's old line claiming it four seconds in was.
func TestTheSplitLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "this has parts · handing it to a task that can take them side by side"
	if checkpointSplitNote != want {
		t.Fatalf("the split line reads %q, want %q", checkpointSplitNote, want)
	}
	inTheHouseRegister(t, checkpointSplitNote)
	if checkpointSplitNote == checkpointCeilingNote || checkpointSplitNote == taskEscalationNote {
		t.Error("the split says what another moment says, and the three have seen different things")
	}
	if strings.Contains(checkpointSplitNote, "running long") {
		t.Errorf("the split claims the turn has run long: %q", checkpointSplitNote)
	}
}

// inTheHouseRegister is the shape every dim one-liner the harness writes over
// somebody's turn is held to: an observation, a middle dot, a promise — one line,
// lowercase, no full stop, no machinery vocabulary (internal/tui3's taskWideNote,
// and CLAUDE.md's vocabulary law).
func inTheHouseRegister(t *testing.T, line string) {
	t.Helper()
	if plain := plainWords(line); plain != line {
		t.Errorf("the line carries machinery vocabulary; plainly it would read %q", plain)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("the line is more than one line: %q", line)
	}
	if line != strings.ToLower(line) {
		t.Errorf("the line is not lowercase: %q", line)
	}
	if strings.HasSuffix(line, ".") {
		t.Errorf("the line ends in a full stop, which makes a remark into an announcement: %q", line)
	}
	if !strings.Contains(line, " · ") {
		t.Errorf("the line has no middle dot, so it is not the observation-then-promise the surface "+
			"already speaks in: %q", line)
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// checkpointMarkModel is what the mark's sidecar rides, and setting it is both
// the fixture and an assertion: the reader is asked of the CREW ALONE, with no
// fall-through to the conversation's own model ([Agent.readMark]), so a session
// without this configured gets no reading at all.
const checkpointMarkModel = "test/mark-reader"

// The two answers a sidecar gives in these tests, written as a real reader
// writes them: a shape line and a sentence naming the letters.
const (
	checkpointSplitSketch = "A | B | C\n" +
		"A is the validation workflow, B is the arithmetic module, C is the currency module."
	checkpointChainSketch = "A > B > C\n" +
		"A is reading the rest of the file, B is the one fix it needs, C is running the suite."
	// AND THE THIRD ANSWER, which the digest's ask now teaches the reader to give:
	// nothing is left. It is the second mind the ceiling's drop is corroborated
	// against ([checkpointSketch.saysDone]).
	checkpointDoneSketch = "(done)\n" +
		"Everything that was asked for has been written and checked."
)

// checkpointSlack is how many of a script's steps a grinding turn spends on
// things that are not tool rounds: one read per mark, the dowry DRAFT at the end,
// and the mastermind that writes the brief out of it. A script cut to the round
// count alone runs out under the sidecar, and the scripted completer's
// past-the-end answer would then be read as a sketch and as a brief.
const checkpointSlack = checkpointMarks + 3

// answerTheNamerOffTheQueue installs the aside every handover fixture needs
// ([scriptedCompleter.aside]).
//
// THE NAMER IS NOT ONE OF THE TURN'S ROUNDS, so it must not spend one of the
// turn's steps. The ceiling's handover asks for a name the moment it decides to
// move the work, on a goroutine of its own and ahead of the two model calls that
// write the brief (checkpoint.go, taskname.go's [nameAhead]) — which is #333's
// design and is correct. What was wrong was here: this file scripted ONE
// positional queue, so on a machine with a spare processor the namer's call took
// whichever step the turn was about to take, and three tests failed about one run
// in five (#392).
//
// IT IS ANSWERED WITH AN EMPTY NAME, which is a namer that could not name — the
// one answer that changes nothing anywhere. [cleanTaskName] hands "" back,
// [Agent.launchRouteTask] adopts a landed name only when it is non-empty, and
// [TaskGraph.nameNode]'s fallback ask carries the same system line and so is
// answered off the queue too. So the row and the told-after line keep the
// person's own words, which is what every assertion on these pages was written
// against.
//
// A completer that is not scripted has no queue to protect and is left alone.
func answerTheNamerOffTheQueue(completer Completer) {
	scripted, ok := completer.(*scriptedCompleter)
	if !ok {
		return
	}
	scripted.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isNameCall(messages) {
			return nil, false
		}
		return textResponse(""), true
	}
}

// checkpointAgent is a watched conversation with a mastermind the mark can be
// read by. Everything else is [newTestAgent]'s.
func checkpointAgent(t *testing.T, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
		for _, extra := range mutate {
			extra(config)
		}
	})
	return agent
}

// grindingSteps answers every request one of three ways: the mark's sidecar ask
// with the sketch it is given, the ceiling's dowry ask with the brief, and
// everything else with one more tool call — a different path each time, so the
// loop detector has nothing to say about it.
//
// It is a script rather than a stub so that what the sidecar is shown is the real
// transcript: the reader has to see a turn's worth of tool calls to be reading
// anything at all.
func grindingSteps(count int, sketch, brief string) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(sketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse(brief), nil
			}
			// THE WRITER IS SILENT IN THIS FIXTURE, and that is deliberate: what
			// these tests are about is the road, and a mastermind answering with a
			// tool call is the ordinary failure that drops the ladder back onto the
			// draft the running model wrote. The tests that are about the writer
			// script it themselves ([handoffSteps]).
			if askedToWriteHandoff(messages) {
				return toolResponse("no-writer", "ls", `{"path":"."}`), nil
			}
			// AND A TURN'S END ASKS WHETHER THE ASK IS FINISHED. A grinding turn is
			// moved by a mark or by the ceiling long before it reaches this, and
			// the tests that end in words say so here rather than letting the
			// question fall past the script.
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			// Visible progress keeps this fixture about checkpoint pricing rather
			// than the independent silent-turn ladder, which now ends a turn at its
			// third warning before the ordinary forty-round ceiling.
			return toolResponseWithText(fmt.Sprintf("call-%d", round), "ls", string(arguments),
				"Working through the next path."), nil
		}
	}
	return steps
}

// askedForSketch reports whether this request ends on a mark's ask.
func askedForSketch(messages []ai.Message) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), "[checkpoint]")
}

// askedForHandoff reports whether this request ends on the dowry ask — the DRAFT,
// put to the model that spent the turn.
func askedForHandoff(messages []ai.Message) bool {
	return endsOn(messages, "[handing over]")
}

// askedToWriteHandoff reports whether this request ends on the mastermind's ask:
// the draft, the digest and the person's words in, one worker's instruction out.
func askedToWriteHandoff(messages []ai.Message) bool {
	return endsOn(messages, "[write the handoff]")
}

// askedForRemains reports whether this request ends on the question a turn's END
// puts to the mark's reader: is what the person asked for finished?
func askedForRemains(messages []ai.Message) bool {
	return endsOn(messages, "[still asked]")
}

func endsOn(messages []ai.Message, marker string) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), marker)
}

// finalAnswer is [finalText] for a session that has a mastermind: the turn's last
// words, and the one answer the end of a turn now asks a reader for.
func finalAnswer(text string) step {
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if askedForRemains(messages) {
			return textResponse(checkpointNothingLeft), nil
		}
		return textResponse(text), nil
	}
}

// marksRead is how many times the sidecar was actually asked.
func marksRead(completer *scriptedCompleter) int {
	read := 0
	for index := range completer.requests() {
		if askedForSketch(completer.request(index)) {
			read++
		}
	}
	return read
}

// handoffAsks counts the requests that ended on the handoff ask.
func handoffAsks(completer *scriptedCompleter) int {
	asked := 0
	for index := range completer.requests() {
		if askedForHandoff(completer.request(index)) {
			asked++
		}
	}
	return asked
}

func noticeTexts(events []Event) []string {
	var said []string
	for _, event := range events {
		if event.Kind == EventNotice {
			said = append(said, event.Text)
		}
	}
	return said
}

func saidSomething(said []string, want string) bool {
	for _, line := range said {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// ── the running model never sees any of it ──────────────────────────────────

// THE MODEL ANSWERING THE TURN IS NEVER SENT THE CHECKPOINT.
//
// This is the whole shape of the wave in one assertion. The question used to ride
// the ambient note lane INTO the running turn, and it was ignored — answered with
// a tool call instead of an answer — on between 17% and 53% of measured turns,
// depending on the phrasing. So it is asked beside the turn now, of a different
// model, and nothing about it reaches the conversation: no note in the transcript,
// no line in any request the session model was sent.
func TestTheRunningModelIsNeverSentTheCheckpoint(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack, checkpointChainSketch, "Finish it\nwhat is left over")}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	// NOT IN THE TRANSCRIPT. A note here would ride into the next request by
	// itself, which is exactly how it used to reach the model.
	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	for _, message := range messages {
		if strings.Contains(messageText(message), "[checkpoint]") {
			t.Fatalf("the checkpoint is in the transcript as a %s: %q", message.Role, messageText(message))
		}
	}
	// AND NOT IN ANY REQUEST THE CONVERSATION'S OWN MODEL WAS SENT. The sidecar's
	// own request carries it, and the sidecar rides the mastermind — which is the
	// only honest way to tell the two apart, because both go down one client.
	for index := range completer.requests() {
		if completer.model(index) == checkpointMarkModel {
			continue
		}
		for _, message := range completer.request(index) {
			if strings.Contains(messageText(message), "[checkpoint]") {
				t.Fatalf("request %d, on model %q, carried the checkpoint: %q",
					index, completer.model(index), messageText(message))
			}
		}
	}
	// AND THE SIDECAR WAS ASKED, so this is a test about where the question went
	// rather than about a question nobody asked.
	if read := marksRead(completer); read != checkpointMarks {
		t.Fatalf("the sidecar was asked %d times over %d rounds, want one per mark (%d)",
			read, rounds, checkpointMarks)
	}
	for index := range completer.requests() {
		if askedForSketch(completer.request(index)) && completer.model(index) != checkpointMarkModel {
			t.Errorf("a mark was read by %q, want the mastermind %q",
				completer.model(index), checkpointMarkModel)
		}
	}
}

// AND THE SIDECAR IS ASKED AT THE MARKS AND NOWHERE ELSE.
//
// The cost of this mechanism on an ordinary turn is zero calls, and on a grinding
// one it is bounded at three — one per mark, on a turn that has already spent ten
// rounds of tool calls.
func TestTheSidecarIsAskedOncePerMarkAndNotBetween(t *testing.T) {
	// Every round up to one short of the second mark: the first mark fires, the
	// second does not.
	rounds := checkpointMarkAt(2) - 1
	steps := append(grindingSteps(rounds+1, checkpointChainSketch, ""), finalAnswer("done"))
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if read := marksRead(completer); read != 1 {
		t.Fatalf("the sidecar was asked %d times over %d rounds; one mark stands at %d and the next "+
			"at %d, so exactly one belongs here", read, rounds, checkpointMarkAt(1), checkpointMarkAt(2))
	}
}

// ── a mark that says the work has parts ─────────────────────────────────────

// A SKETCH WITH PARTS IN IT HANDS THE TURN OVER, AT THE FIRST MARK.
//
// The one assertion the whole wave turns on. What the person reads is the split's
// own line; what the worker opens on is the parts, named, above everything the
// turn found out; and the road is armed BY A JUDGEMENT — a mastermind read this
// transcript and drew independent parts out of it, which is what lets the division
// reviewer adjudicate a floor refusal later (task_divide.go).
func TestASplitSketchAtTheFirstMarkHandsTheTurnOver(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	// Well past the first mark and well short of the second, so what fires here
	// can only be the first.
	completer := &scriptedCompleter{steps: grindingSteps(checkpointMarkAt(1)+6, checkpointSplitSketch, dowry)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// EXACTLY ONE TASK, on the one road.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at the first mark, want exactly one", count)
	}
	// AND IT STOPPED AT THE MARK. The script had six rounds left in it, and the
	// ceiling stands three times further along. The slack past the mark is the
	// calls the handover itself makes: the sketch, the draft, and the mastermind
	// that writes the brief out of it.
	if completer.requests() > checkpointMarkAt(1)+4 {
		t.Errorf("the turn made %d requests past a first mark standing at %d rounds",
			completer.requests(), checkpointMarkAt(1))
	}
	// THE LINE, EXACTLY.
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("the split never said its line; notices were %q", noticeTexts(collected))
	}
	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Error("a split at the first mark drew the ceiling's line")
	}
	// THE PERSON'S ASK RIDES IT VERBATIM.
	if node.spec.request != asked {
		t.Errorf("the task's request is %q, want the person's own words %q", node.spec.request, asked)
	}
	// THE SKETCH IS AT THE HEAD OF THE BRIEF, so the worker's divide_work has the
	// parts already named — and the dowry is still under it.
	if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS: A | B | C") {
		t.Errorf("the brief does not open on the parts the sidecar drew:\n%s", node.spec.brief)
	}
	if !strings.Contains(node.spec.brief, "the validation workflow") {
		t.Errorf("the legend did not reach the worker:\n%s", node.spec.brief)
	}
	if !strings.Contains(node.spec.brief, "everything this turn already found out") {
		t.Errorf("the task lost what the turn learned:\n%s", node.spec.brief)
	}
	// ARMED, AND ARMED BY THE READER THAT READ THE WORK. The word matters: only a
	// model's own reading lets the reviewer reconsider a below-floor division
	// ([TaskNode.armedByJudgement]).
	if got := node.armedBy(); got != armedJudged {
		t.Errorf("the task was armed by %q, want the mark reader's judgement (%q)", got, armedJudged)
	}
	if !node.dividing() {
		t.Error("a sketch that named three parts armed nothing")
	}
	// THE TURN IS OVER, and the transcript is not left with a question nobody
	// answered — the next turn would open on it and answer it.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointSplitNote) {
		t.Errorf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
}

// AND A SKETCH THAT SAYS ONE JOB COSTS THE TURN NOTHING BUT THE CALL.
//
// Nothing is injected, nothing is said, nothing is started, and the turn runs to
// its own end. This is the answer on almost every turn that ever reaches a mark,
// so it has to be free of everything except the one call.
func TestASketchSayingOneJobLetsTheTurnRunOn(t *testing.T) {
	const answered = "here is the fix and the suite is green"

	rounds := checkpointMarkAt(2)
	steps := append(grindingSteps(rounds+2, checkpointChainSketch, "Finish it\nwhat is left"), finalAnswer(answered))
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "work through the one thing I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started off a sketch that said the work was one job", count)
	}
	if said := noticeTexts(collected); saidSomething(said, checkpointSplitNote) ||
		saidSomething(said, checkpointCeilingNote) {
		t.Fatalf("a carry-on said something to the person: %q", said)
	}
	// AND THE DOWRY WAS NEVER ASKED FOR, which is what makes this free: a carry-on
	// costs the sidecar's call and not a second request on the turn's own model.
	if asks := handoffAsks(completer); asks != 0 {
		t.Errorf("a carry-on asked for the dowry %d times", asks)
	}
	// BOTH MARKS WERE READ, so this is two carry-ons rather than a mechanism that
	// never fired.
	if read := marksRead(completer); read != 2 {
		t.Errorf("the sidecar was asked %d times over %d rounds, want both marks", read, rounds)
	}
	// AND THE TURN'S OWN ANSWER STANDS.
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
		t.Errorf("the turn ended as a %s saying %q, want the answer the model was giving",
			last.Role, messageText(last))
	}
}

// AND A READER NOBODY CAN REACH IS A CARRY-ON TOO.
//
// The fail-open direction, and it is safe for exactly one reason: the ceiling is
// the recovery bound. A session whose sidecar faults every time is a session that
// behaves as it did before any of this existed — the last mark still moves the
// work.
func TestASidecarThatCannotBeReachedCarriesOnAndTheCeilingStillFires(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	steps := grindingSteps(rounds+checkpointSlack, "", "Finish it\nwhat is left and what was found")
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return nil, errors.New("the reader is down")
			}
			return inner(ctx, messages)
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// NOTHING MOVED AT THE FIRST TWO MARKS.
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Error("a reader that faulted still split the turn")
	}
	// AND THE CEILING MOVED IT ANYWAY, which is the whole of why the fail-open is
	// safe.
	if !saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Fatalf("the ceiling never fired behind a sidecar that never answered; notices were %q",
			noticeTexts(collected))
	}
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at the ceiling, want exactly one", count)
	}
	// AND THE BRIEF IS THE DOWRY ALONE, with no sketch above it to head it.
	if strings.Contains(node.spec.brief, "WHAT IS LEFT, AS PARTS") {
		t.Errorf("a sketch nobody drew headed the brief:\n%s", node.spec.brief)
	}
	if !strings.Contains(node.spec.brief, "what was found") {
		t.Errorf("the task lost the dowry: %q", node.spec.brief)
	}
}

// AND A READER THAT MISSES ITS WINDOW IS A CARRY-ON.
//
// The window is a deadline on the call's own context, so a context that runs out
// under the reader exercises exactly the branch a slow mastermind reaches. What is
// pinned here is that the harness does not WAIT past it and does not read anything
// out of a call that never came back.
func TestASidecarThatMissesItsWindowIsACarryOn(t *testing.T) {
	held := &holdingCompleter{}
	agent := checkpointAgent(t, held)
	// THE READER IS SHOWN AN ACCOUNT OF THE WORK, so there has to be work: a turn
	// with nothing in it is one the harness declines to pay a reader for at all.
	workedTurn(agent, "work through the four things I listed", 3)

	ctx, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer done()
	started := time.Now()
	read := agent.readMark(ctx)

	if read.sketch.drawn() || read.sketch.split() {
		t.Fatalf("a read that never answered produced %+v", read.sketch)
	}
	if !read.asked || !read.failed {
		t.Errorf("a read that timed out was recorded as asked=%v failed=%v", read.asked, read.failed)
	}
	if waited := time.Since(started); waited > checkpointSketchWindow {
		t.Errorf("the harness waited %s on a reader that never answered", waited)
	}
}

// ── the word the mark's reading wears ───────────────────────────────────────

// TestTheMarksReadingSaysTakingStockAndTakesItBack is the third stage that used
// to run in the dark.
//
// A turn stops mid-round, a mastermind is shown an account of the work and asked
// what is left of the ask, and the reading is bounded at
// [checkpointSketchWindow] — ten to thirty seconds on the measured runs, with a
// screen that drew nothing whatever for it. It is now a stage with a word, and
// the word is `taking stock`: what this reader does is weigh the whole ask
// against everything that has been done, which is not what the gates at the end
// of a turn do when they say `checking`.
func TestTheMarksReadingSaysTakingStockAndTakesItBack(t *testing.T) {
	log := watchPhases(t)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(checkpointChainSketch), nil
		},
	}}
	agent := checkpointAgent(t, completer)
	workedTurn(agent, "read the four modules and fix what is broken", 3)

	agent.readMark(context.Background())

	news := log.all()
	said := false
	for _, one := range news {
		if one.Phase == provider.PhaseTakingStock {
			said = true
			if one.Since.IsZero() {
				t.Fatal("the reading carries no start, so nothing can count up from it")
			}
		}
	}
	if !said {
		t.Fatalf("the mark's reading drew nothing; phases were %v", phaseWords(news))
	}
	// AND IT COMES OFF THE SCREEN WITH THE READING. A stage that is over and
	// still drawn is the defect the phase clock exists for.
	if len(news) == 0 || news[len(news)-1].Phase != "" {
		t.Fatalf("the reading left a clock running; phases were %v", phaseWords(news))
	}
}

// AND ON EVERY WAY OUT, which for this reader means the one that matters most:
// a mastermind that never answered inside its window. A stage cleared only on
// the happy path is a clock a person watches for a whole window after the work
// behind it gave up.
func TestAMarkReadingThatMissedItsWindowStillTakesItsClockOff(t *testing.T) {
	log := watchPhases(t)
	agent := checkpointAgent(t, &holdingCompleter{})
	workedTurn(agent, "work through the four things I listed", 3)

	ctx, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer done()
	agent.readMark(ctx)

	news := log.all()
	if len(news) == 0 || news[len(news)-1].Phase != "" {
		t.Fatalf("a reading that never answered left a clock running; phases were %v", phaseWords(news))
	}
}

// AND A MARK WITH NOTHING TO SHOW A READER DRAWS NOTHING. No call is made, so
// there is no wait, and a word for a stage nobody waits through is the surface
// narrating machinery.
func TestAMarkThatAsksNobodyDrawsNoClock(t *testing.T) {
	log := watchPhases(t)
	agent := checkpointAgent(t, &scriptedCompleter{})

	if read := agent.readMark(context.Background()); read.asked {
		t.Fatal("a turn with nothing in it still paid for a reading")
	}
	for _, one := range log.all() {
		if one.Phase == provider.PhaseTakingStock {
			t.Fatal("a reading that never happened drew a clock")
		}
	}
}

// holdingCompleter answers nothing at all: it waits for its context and reports
// what ended it, which is what a provider that is thinking too slowly does.
type holdingCompleter struct{}

func (holdingCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// ── the ceiling ─────────────────────────────────────────────────────────────

// AT THE CEILING THE HARNESS STOPS READING, WHATEVER THE SKETCHES SAID.
//
// The guarantee this whole file exists to make: past the last mark the turn ends,
// exactly one task carries the work, the person's own sentence rides it verbatim,
// and the line they read is that line. Two sketches here said the work was one
// job, which is what makes the ceiling a bound rather than a third opinion.
func TestAtTheCeilingTheTurnEndsAndTheWorkMovesToOneWatchedTask(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const brief = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	rounds := checkpointMarkAt(checkpointMarks)
	// One step past the ceiling, so a turn that failed to stop would be visible as
	// a turn that kept calling tools rather than as a turn that ran out of script.
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack+2, checkpointChainSketch, brief)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
	// The node is recorded and left running: a node that LANDS posts its report
	// into the session, which wakes a turn of its own, and this test is about the
	// turn that just ended.
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// EXACTLY ONE TASK, on the one road.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at the ceiling, want exactly one", count)
	}
	// THE PERSON'S ASK RIDES IT VERBATIM. It is the one thing on this road no
	// writer may touch: the brief is the model's, the request is theirs.
	if node.spec.request != asked {
		t.Errorf("the task's request is %q, want the person's own words %q", node.spec.request, asked)
	}
	if !strings.Contains(node.instruction(), asked) {
		t.Errorf("the document the worker reads does not carry the person's words:\n%s", node.instruction())
	}
	// AND THE DOWRY WITH IT.
	if !strings.Contains(node.spec.brief, "everything this turn already found out") {
		t.Errorf("the task lost what the turn learned: %q", node.spec.brief)
	}
	// ARMED TO SPLIT, which is what the line promises — and armed by the ceiling's
	// own measured evidence rather than by a sketch that said the opposite.
	if !node.spec.wide {
		t.Error("the ceiling's task is not armed to split, so the line promises something it did not do")
	}
	if got := node.armedBy(); got != armedWide {
		t.Errorf("the ceiling's task was armed by %q, want its own measured breadth (%q)", got, armedWide)
	}
	// THE LINE, EXACTLY.
	if !saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Fatalf("the ceiling never said its line; notices were %q", noticeTexts(collected))
	}
	// THE TURN IS OVER, and the transcript is not left with a question nobody
	// answered — the next turn would open on it and answer it.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointCeilingNote) {
		t.Errorf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	// AND IT STOPPED WHERE IT SAID IT WOULD. What stands past the ceiling is the
	// last mark's read, the handoff brief and the errand that names the session
	// (title.go), and none of those is another tool round.
	if completer.requests() > rounds+checkpointSlack {
		t.Errorf("the turn made %d requests past a ceiling standing at %d rounds", completer.requests(), rounds)
	}
	// THE WORK WAS READ TWICE BEFORE THE HARNESS DECIDED, AND ONCE MORE TO BRIEF
	// THE WORKER.
	if read := marksRead(completer); read != checkpointMarks {
		t.Errorf("the sidecar was asked %d times, want one read per mark (%d)", read, checkpointMarks)
	}
}

// AND THE CEILING TAKES THE LAST SKETCH WHEN THERE IS ONE.
//
// The decision at the ceiling is already made and nothing a sketch says can stop
// it, but the drawing was paid for and it is exactly what the worker's first
// paragraph should be — so a ceiling reached on a sidecar that finally saw parts
// hands them over named.
func TestTheCeilingCarriesTheLastSketchIntoTheBrief(t *testing.T) {
	const brief = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack, checkpointSplitSketch, brief)}
	// Divide is off, which is the product's other posture: the sketch still heads
	// the brief, because a brief is a document and not a road.
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// A split fired at the FIRST mark here, so this is the same road arriving
	// early — which is the honest thing to assert about a sidecar that saw parts.
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("a sketch with parts in it never split the turn; notices were %q", noticeTexts(collected))
	}
	if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS: A | B | C") {
		t.Errorf("the brief does not open on the parts:\n%s", node.spec.brief)
	}
}

// AND THE CEILING STILL HANDS THE WORK OVER WHEN THE BRIEF CANNOT BE WRITTEN.
//
// A provider fault, an empty reply, a model that answers with nothing: the dowry
// is lost and the work is not. The task starts on the person's own sentence,
// which is [unshaped]'s answer to the same failure at the typed door — a task
// that could not start at all would be the guarantee broken.
func TestTheCeilingHandsOverEvenWhenNobodyCanWriteTheBrief(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack, checkpointChainSketch, "   ")}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
	if node.spec.brief != asked {
		t.Errorf("with no brief written the task should run on the person's own words; it has %q",
			node.spec.brief)
	}
}

// THE HANDOFF BRIEF IS NOT SPOKEN INTO THE ROOM.
//
// It is a worker's instruction, not a word to the person, and the turn it is
// written at the end of has a stream observer installed that types deltas into
// the room in the assistant's voice. Left on that stream the brief would paint
// itself over the top of the answer it is ending, which is the fault
// [provider.WithoutStream] exists to prevent everywhere else in this package.
func TestTheHandoffBriefIsNotStreamedIntoTheRoom(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	steps := grindingSteps(rounds+checkpointSlack, checkpointChainSketch, "Finish it\nwhat is left")
	streamed := make(chan bool, 4)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForHandoff(messages) {
				streamed <- provider.Streaming(ctx)
			}
			return inner(ctx, messages)
		}
	}
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	select {
	case on := <-streamed:
		if on {
			t.Error("the handoff brief was asked for on the turn's own stream, so it types itself " +
				"into the room in the model's voice")
		}
	default:
		t.Fatal("the ceiling never asked for a handoff brief")
	}
}

// ── what comes back when the dowry is asked for ─────────────────────────────

// dsmlSentinel is the answer a deepseek model actually gave the handoff ask on a
// live turn: the request carried no tools, and the model wrote its own chat
// template's tool-call token as text anyway. The person read
// "task 1 started: <｜DSML｜tool_calls>".
//
// NOTHING IN THE CODE MATCHES THIS STRING. [briefIsProse] reads the SHAPE of an
// answer — words with spaces between them — so this is a fixture here and a
// pattern nowhere, and the guard holds for the next provider's sentinel too.
const dsmlSentinel = "<｜DSML｜tool_calls>"

// A DOWRY THAT IS NOT PROSE IS NOT A BRIEF, AND IT IS NEVER THE NAME EITHER.
//
// No belt is not the same fact as no tool grammar: the ask goes out with no
// tools on it, over a transcript in which every turn so far called one. When what
// comes back is markup, the work still moves — the guarantee is not conditional —
// but it moves on the person's own sentence, exactly as it does when the provider
// faults, and the line they read names their words rather than the machinery.
func TestADowryOfMachineMarkupIsRefusedAndNeverBecomesTheName(t *testing.T) {
	const asked = "work through the four things I listed and report back"

	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack, checkpointChainSketch, dsmlSentinel)}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// THE WORK STILL MOVES. A brief nobody could read is the same failure as no
	// brief at all, and [unshaped]'s answer to it is the person's own words.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
	if node.spec.brief != asked {
		t.Errorf("the task runs on %q; a brief that is not prose falls back to the person's words %q",
			node.spec.brief, asked)
	}
	// AND THE LINE THE PERSON READS IS ABOUT THEIR WORK. The namer runs AHEAD of
	// this line now, on purpose (#333), and the fixture answers it with no name
	// ([answerTheNamerOffTheQueue]) — so what is left to read here is the
	// told-after line as the road itself draws it, off the person's own sentence.
	notice := routeNotice(collected)
	if notice == "" {
		t.Fatalf("no task was announced; notices were %q", noticeTexts(collected))
	}
	if strings.Contains(notice, dsmlSentinel) || strings.Contains(notice, "DSML") {
		t.Errorf("the sentinel became the task's name: %q", notice)
	}
	if !strings.Contains(notice, "work through the four things") {
		t.Errorf("the task was announced as %q, want the person's own words", notice)
	}
	// AND THE NAMER DID ASK, off the queue rather than out of the turn's script.
	// Without this the three assertions above would still pass on a fixture that
	// had quietly stopped running the namer at all, and the race #392 was about
	// would come back the next time anything moved.
	//
	// IT IS AWAITED AND NOT SAMPLED, because the ask is an errand and nothing in
	// the turn's path orders it against the turn — that is the whole subject of
	// this fix. The ask rides [Agent.nameAhead]'s own goroutine, and the fallback
	// ask [TaskGraph.nameNode] makes when the first came back empty rides another
	// one (taskname.go), so on a starved box either can land after this line runs.
	// Reading the count once would be this test failing on the scheduler, which is
	// exactly the class of failure #392 was.
	waitFor(t, "the namer's ask, answered off the queue", func() bool {
		return completer.asideRequests() > 0
	})
}

// AND A CONTINUATION SAYING NOTHING IS LEFT DROPS THE CEILING'S HANDOVER — WHEN
// THE READER AGREES.
//
// The ceiling reads a counter, and a counter cannot see that the work finished
// thirty seconds ago. The one reader that can is the model holding the findings,
// which is the model this ask is put to — so it is asked, and an answer of
// [checkpointNothingLeft] ends the matter: no task, no line, no gap spent, and
// the turn carries on to the answer it was about to give.
//
// AND THE MARK'S OWN READER SAID THE SAME THING at the same moment, which is what
// makes this a drop rather than a model grading itself — see the two tests below.
func TestAContinuationSayingNothingIsLeftDropsTheCeilingHandover(t *testing.T) {
	const answered = "all eight files are written and the smoke check passed"

	rounds := checkpointMarkAt(checkpointMarks)
	steps := append(grindingSteps(rounds+checkpointSlack, checkpointDoneSketch, checkpointNothingLeft), finalAnswer(answered))
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "write the eight files I listed and smoke-check them")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// NOTHING WAS STARTED and nothing was said about starting anything.
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started out of a turn that had nothing left to hand over", count)
	}
	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Errorf("the person was told their answer was being moved and then watched it finish where "+
			"it was; notices were %q", noticeTexts(collected))
	}
	if routeNotice(collected) != "" {
		t.Errorf("a task was announced: %q", routeNotice(collected))
	}
	// AND THE TURN'S OWN ANSWER STANDS. The transcript ends on the model's words,
	// not on a line the harness wrote over the top of them.
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
		t.Errorf("the turn ended as a %s saying %q, want the answer the model was about to give",
			last.Role, messageText(last))
	}
	// AND THE ASK IS NOT PUT TWICE. The ladder is spent, so no mark can fire
	// again, and a turn that declared itself finished is not re-interrogated at
	// every round that follows.
	if asks := handoffAsks(completer); asks != 1 {
		t.Errorf("the dowry was asked for %d times, want once", asks)
	}
}

// THE REMAINS CONTRACT IS PINNED, because it is a contract and not a hint.
//
// The harness reads one token out of a free-text answer, and it can only read
// what the ask told the model to write. A wording change on one side alone is a
// contract with one party — either the harness never sees a done turn again, or
// it starts guessing at prose, which is the keyword rule this token exists
// instead of.
func TestTheHandoffAskCarriesTheRemainsContractItIsReadAgainst(t *testing.T) {
	if checkpointNothingLeft != "NOTHING LEFT TO DO" {
		t.Errorf("the done token reads %q; it is quoted in the manual and matched off the first line "+
			"of the answer, so it is not a string to reword on one side", checkpointNothingLeft)
	}
	if !strings.Contains(checkpointHandoffAsk, checkpointNothingLeft) {
		t.Fatalf("the ask never tells the model the token the harness reads:\n%s", checkpointHandoffAsk)
	}
	if !strings.Contains(checkpointHandoffAsk, "WHAT REMAINS") {
		t.Errorf("the ask never asks what remains, so the token has nothing to be the answer to:\n%s",
			checkpointHandoffAsk)
	}
	// AND THE TOKEN IS WHAT THE READER READS. The ask and [declaresNothingLeft]
	// are one contract, so the answer the ask demands must be the answer the
	// harness recognises — bare, emphasised, or followed by the reason.
	for _, answer := range []string{
		checkpointNothingLeft,
		"**" + checkpointNothingLeft + "**",
		checkpointNothingLeft + " — the eight files are written and checked.",
		strings.ToLower(checkpointNothingLeft),
	} {
		if !declaresNothingLeft(answer) {
			t.Errorf("the contract's own answer is not read as one: %q", answer)
		}
	}
	// AND A BRIEF IS NOT A DECLARATION. The token found in the middle of an
	// instruction is a sentence, and reading it as a declaration would drop the
	// handoff of work that is genuinely left.
	for _, brief := range []string{
		"Finish the parser rewrite\nThere is nothing left to do on the lexer.",
		"Rewrite the lexer, then there is NOTHING LEFT TO DO.",
	} {
		if declaresNothingLeft(brief) {
			t.Errorf("a brief was read as a declaration that the work is done: %q", brief)
		}
	}
}

// AND WHAT COUNTS AS A BRIEF IS PROSE, JUDGED BY SHAPE.
//
// The bar is deliberately structural: an instruction is words with spaces
// between them, in any language and from any provider. A rule spelled in one
// model's special tokens is a rule that is out of date the next time a template
// ships.
func TestOnlyProseIsAcceptedAsADowry(t *testing.T) {
	for _, refused := range []string{
		"", "   ", "\n\n",
		dsmlSentinel,
		"<|tool_calls_begin|>",
		"<tool_call>",
		"{}",
		"1 2 3 4 5",
	} {
		if briefIsProse(refused) {
			t.Errorf("%q was accepted as a worker's instruction", refused)
		}
	}
	for _, accepted := range []string{
		"Finish the four pieces, and the auth test is the one still failing.",
		"what is left, and everything this turn already found out",
	} {
		if !briefIsProse(accepted) {
			t.Errorf("%q was refused as a worker's instruction", accepted)
		}
	}
}

// ── the turns that are never checkpointed ───────────────────────────────────

// A NODE, A SCREENLESS SESSION WITH NOBODY LEFT IN CHARGE AND THE SESSION'S OWN
// VOICE ARE ALL LEFT ALONE.
//
// Each for its own reason, and each of them is a turn that would be made worse by
// a ceiling: a node already runs under a step cap, a deadline and a checker; a
// session with nobody watching and nobody left in charge has no one to read the
// line; and a turn the session started for itself is the session spending money
// on its own sentence.
//
// AND NONE OF THEM PAYS FOR A SIDECAR EITHER. The gate stands in front of the
// meter, so a turn that may not be checkpointed is a turn this file never bills.
func TestTheTurnsThatMustNeverBeCheckpointedAreNot(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks) + 2

	for _, shape := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"a node", func(config *Config) { config.InTask = true }},
		{"a session nobody is watching and nobody is left in charge of", func(config *Config) { config.AskConsent = false }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			steps := append(grindingSteps(rounds, checkpointSplitSketch, ""), finalAnswer("done"))
			completer := &scriptedCompleter{steps: steps}
			agent := checkpointAgent(t, completer, shape.mutate)
			graph := stubbedGraph(agent, func(node *TaskNode) {
				node.finish("done", nil, "", "")
				node.graph.complete(node, TaskDone)
			})

			events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)

			if read := marksRead(completer); read != 0 {
				t.Errorf("%s paid for %d mark readings", shape.name, read)
			}
			if said := noticeTexts(collected); saidSomething(said, checkpointCeilingNote) ||
				saidSomething(said, checkpointSplitNote) {
				t.Errorf("%s was moved off its own turn: %q", shape.name, said)
			}
			if count := admitted(graph); count != 0 {
				t.Errorf("%s had %d tasks started over the top of it", shape.name, count)
			}
			// The turn ran to the end of its own script rather than being stopped.
			if completer.requests() <= rounds {
				t.Errorf("%s made only %d requests of %d, so something ended it early",
					shape.name, completer.requests(), rounds)
			}
		})
	}
}

// AND A TURN THE PERSON INTERRUPTS IS NOT CHECKPOINTED ON ITS WAY OUT.
//
// The interrupt is the person saying they do not want this; answering it with a
// task on the rail would be the harness having the last word.
func TestAnInterruptedTurnIsNotCheckpointed(t *testing.T) {
	ceiling := checkpointMarkAt(checkpointMarks)
	rounds := ceiling + checkpointSlack + 2
	// A CHAIN AT EVERY MARK, so the only thing that could move this turn is the
	// ceiling — which is what the interrupt is being tested against.
	steps := grindingSteps(rounds, checkpointChainSketch, "")
	// The interrupt lands as the CEILING'S ROUND is being answered, so the turn
	// reaches the checkpoint seam with a context that is already over. It is
	// counted off the tool rounds rather than off a script index, because the
	// sidecar's own calls sit between them and a fixed index would drift every time
	// the reading changes.
	var agent *Agent
	var tools atomic.Int64
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) || askedForHandoff(messages) {
				return inner(ctx, messages)
			}
			if tools.Add(1) == int64(ceiling) {
				agent.Interrupt()
			}
			return inner(ctx, messages)
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent = checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if said := noticeTexts(collected); saidSomething(said, checkpointCeilingNote) ||
		saidSomething(said, checkpointSplitNote) {
		t.Error("an interrupted turn was moved onto the rail")
	}
	if count := admitted(graph); count != 0 {
		t.Errorf("%d tasks were started out of an interrupted turn", count)
	}
	// And the turn ended where the interrupt landed rather than running on: the
	// stream closing is the turn being over ([collect]), and the script had rounds
	// left in it.
	if completer.requests() > ceiling+checkpointSlack {
		t.Errorf("the turn made %d requests after an interrupt at round %d",
			completer.requests(), ceiling)
	}
}

// ── what the reader is actually shown ───────────────────────────────────────

// THE READER IS SHOWN AN ACCOUNT OF THE WORK AND NOT THE CONVERSATION.
//
// This is the assertion the wave turns on. Reading the raw transcript was
// measured at 57k, 65k and 91k input tokens across three marks of one crew run —
// $0.62 on the mastermind tier against $0.123 for the sixty-two calls that did
// the work — and all three answered "carry on". So the things that actually bear
// on what is left are assembled by hand, and everything else, which is the bulk
// and the bill, is left out.
func TestTheDigestIsTheAskTheLedgerWhatWasWrittenAndTheLastWord(t *testing.T) {
	const asked = "add the validation workflow, then fix the arithmetic and the currency"
	const bulk = "SEVENTY LINES OF SEARCH RESULTS NOBODY NEEDS TO SEE"

	digest := checkpointDigest(asked, []ai.Message{
		textMessage("user", asked),
		toolCallMessage("c1", "grep", `{"pattern":"validate(","path":"./internal"}`),
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: bulk}}},
		toolCallMessage("c2", "write", `{"path":"./workflow.yml","content":"`+strings.Repeat("x", 4000)+`"}`),
		{Role: "tool", ToolCallID: "c2", Content: []ai.ContentPart{{Type: "text", Text: "wrote 4000 bytes"}}},
		toolCallMessage("c3", "edit", `{"path":"./money.go","edits":[]}`),
		{Role: "tool", ToolCallID: "c3", Content: []ai.ContentPart{{Type: "text", Text: "edited"}}},
		textMessage("assistant", "the workflow is in; the currency module is still untouched"),
	})

	// THE ASK, VERBATIM AND FIRST. Everything else in the digest is measured
	// against it, and it is the one thing on this road nobody rewrites.
	if !strings.HasPrefix(digest, checkpointDigestAsked+"\n"+asked) {
		t.Fatalf("the digest does not open on the person's own words:\n%s", digest)
	}
	// THE LEDGER: one line per call, naming the tool and the thing it touched.
	for _, line := range []string{
		"grep validate(",
		"write ./workflow.yml",
		"edit ./money.go",
	} {
		if !strings.Contains(digest, line) {
			t.Errorf("the ledger is missing %q:\n%s", line, digest)
		}
	}
	// AND WHAT CAME BACK, WHICH IS THE HALF THIS DIGEST USED TO REFUSE TO CARRY.
	// A ledger says a suite was run; a result says what it reported, and that is
	// the only statement of how much of the ask is actually discharged.
	found := digest[strings.Index(digest, checkpointDigestFound):]
	for _, evidence := range []string{bulk, "wrote 4000 bytes", "edited"} {
		if !strings.Contains(found, evidence) {
			t.Errorf("the reader was not shown %q:\n%s", evidence, digest)
		}
	}
	// AND NEWEST FIRST, so a reader that runs out of attention spends it on the
	// work in front of the turn.
	if strings.Index(found, "edited") > strings.Index(found, bulk) {
		t.Errorf("the results are printed oldest first:\n%s", found)
	}
	// AND NO PAYLOAD EITHER. `write` names a path here precisely because the
	// argument that carries the bytes is the thing this exists to leave out —
	// whichever order the two arrived in.
	if strings.Contains(digest, strings.Repeat("x", 200)) {
		t.Errorf("a written file's contents reached the reader:\n%s", digest)
	}
	// WHAT HAS BEEN WRITTEN, deduplicated — a part of the ask already discharged
	// is exactly what the reader is being asked to subtract.
	written := digest[strings.Index(digest, checkpointDigestWritten):]
	for _, path := range []string{"./workflow.yml", "./money.go"} {
		if !strings.Contains(written, path) {
			t.Errorf("%q is not in what has been written:\n%s", path, digest)
		}
	}
	if strings.Contains(written, "grep") {
		t.Errorf("a search was recorded as something written:\n%s", digest)
	}
	// AND THE LAST THING THE RUNNING MODEL SAID.
	if !strings.Contains(digest, checkpointDigestSaid+"\nthe workflow is in") {
		t.Errorf("the last thing said did not reach the reader:\n%s", digest)
	}
}

// AND A SECTION WITH NOTHING IN IT IS NOT WRITTEN AT ALL.
//
// The emptiness law, applied to a document a model reads: an empty heading is an
// invitation to answer about the emptiness.
func TestTheDigestWritesNoEmptySections(t *testing.T) {
	digest := checkpointDigest("count the rows in the ledger", []ai.Message{
		toolCallMessage("c1", "read", `{"path":"./ledger.csv"}`),
	})
	if strings.Contains(digest, checkpointDigestWritten) {
		t.Errorf("a turn that wrote nothing carries a heading saying so:\n%s", digest)
	}
	if strings.Contains(digest, checkpointDigestSaid) {
		t.Errorf("a turn that said nothing carries a heading saying so:\n%s", digest)
	}
	// AND A TURN WITH NOTHING IN IT AT ALL IS NOT A DIGEST. [Agent.readMark]
	// spends nothing on one, which is the emptiness law reaching the bill.
	if got := checkpointDigest("", nil); got != "" {
		t.Errorf("an empty turn produced a digest:\n%s", got)
	}
}

// AND THE WHOLE OF IT IS BOUNDED, WITH THE OLDEST STEPS THE FIRST TO GO.
//
// The ledger is the one section that grows without bound, and a turn of ninety
// calls is exactly the turn whose last words and written things matter most — so
// they are fitted first and the ledger takes what room is left. What it drops it
// says it dropped: a silent truncation would let the reader believe the turn had
// done less than it had.
func TestTheDigestIsBoundedAndDropsTheOldestStepsFirst(t *testing.T) {
	const asked = "sweep every one of these and report"
	messages := []ai.Message{textMessage("user", asked)}
	for index := 0; index < 4000; index++ {
		messages = append(messages, toolCallMessage(fmt.Sprintf("c%d", index), "read",
			fmt.Sprintf(`{"path":"./%s/%d.txt"}`, strings.Repeat("deep", 12), index)))
	}
	messages = append(messages,
		toolCallMessage("last-write", "write", `{"path":"./report.md"}`),
		textMessage("assistant", "the last thing this turn said"))

	digest := checkpointDigest(asked, messages)

	if len(digest) > checkpointDigestBytes {
		t.Fatalf("the digest is %d bytes against a bound of %d", len(digest), checkpointDigestBytes)
	}
	// AND THE THREE SECTIONS THAT ARE NOT THE LEDGER SURVIVED IT WHOLE.
	if !strings.Contains(digest, asked) {
		t.Errorf("the ask was squeezed out by the ledger:\n%s", digest[:400])
	}
	if !strings.Contains(digest, "./report.md") {
		t.Error("what was written was squeezed out by the ledger")
	}
	if !strings.Contains(digest, "the last thing this turn said") {
		t.Error("the last thing said was squeezed out by the ledger")
	}
	// THE NEWEST STEPS ARE THE ONES KEPT.
	if !strings.Contains(digest, "/3999.txt") {
		t.Error("the ledger dropped the most recent step, which is the work in front of the turn")
	}
	if strings.Contains(digest, "/0.txt") {
		t.Error("a bounded ledger kept its oldest line")
	}
	// AND IT SAYS WHAT IT DROPPED.
	if !strings.Contains(digest, "earlier steps") {
		t.Errorf("the ledger was truncated in silence:\n%s", digest[:400])
	}
}

// AND THE SIDECAR'S REQUEST IS THAT DIGEST AND NOTHING ELSE.
//
// The unit above pins what the digest says; this pins that it is what actually
// goes on the wire — one message, with the ask under it, and not a line of the
// transcript the running model is holding.
func TestTheMarkReaderIsSentTheDigestAndNotTheTranscript(t *testing.T) {
	const bulk = "A TOOL RESULT LONG ENOUGH TO PAY FOR THE WHOLE MECHANISM"
	// A second result, on a call the reader is NOT going to be shown a line for,
	// because no call in this transcript claims that id. A result credited to the
	// wrong line is evidence that is worse than none.
	const orphan = "A RESULT WHOSE CALL NOBODY MADE"

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(checkpointChainSketch), nil
		},
	}}
	agent := checkpointAgent(t, completer)
	workedTurn(agent, "read the four modules and fix what is broken", 3)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		ai.Message{Role: "tool", ToolCallID: "call-0", Content: []ai.ContentPart{{Type: "text", Text: bulk}}},
		ai.Message{Role: "tool", ToolCallID: "nobody", Content: []ai.ContentPart{{Type: "text", Text: orphan}}})
	agent.mu.Unlock()

	read := agent.readMark(context.Background())
	if read.sketch.shape != "A > B > C" {
		t.Fatalf("the reader's answer came back as %+v", read.sketch)
	}
	if completer.requests() != 1 {
		t.Fatalf("the sidecar made %d requests, want one", completer.requests())
	}
	sent := completer.request(0)
	if len(sent) != 1 {
		t.Fatalf("the reader was sent %d messages, want the one digest", len(sent))
	}
	// THE ACCOUNT AND NOT THE THREAD. A tool result reaches the reader as the tail
	// of one line under the call that made it, tied by id — and a result whose id
	// names no call is dropped rather than attached to whichever line is beside it.
	page := messageText(sent[0])
	if !strings.Contains(page, "read ./0.txt"+checkpointResultArrow+bulk) {
		t.Errorf("the result was not attached to the call that made it:\n%s", page)
	}
	if strings.Contains(page, orphan) {
		t.Errorf("a result nobody's call produced was shown to the reader:\n%s", page)
	}
	if !strings.Contains(messageText(sent[0]), "read the four modules") {
		t.Errorf("the person's ask was not sent to the reader:\n%s", messageText(sent[0]))
	}
	if !strings.HasSuffix(messageText(sent[0]), checkpointSketchAsk) {
		t.Errorf("the digest does not end on the ask:\n%s", messageText(sent[0]))
	}
	// AND THE CALL WAS PRICED, which is what the journal line beside it carries.
	if !read.asked || read.failed {
		t.Errorf("a read that answered was recorded as asked=%v failed=%v", read.asked, read.failed)
	}
}

// ── what the journal now holds ──────────────────────────────────────────────

// EVERY MARK READ IS WRITTEN DOWN, WITH WHAT IT DECIDED AND WHAT IT COST.
//
// None of this existed. Three reads on one measured run cost five times the work
// they were judging, all three answered "carry on", and the file held three
// anonymous auxiliary usage lines — no ask, no answer, no decision. A mechanism
// that cannot be measured cannot be tuned, so the reading is journaled beside the
// money.
func TestEveryMarkReadIsJournaledWithItsDecisionAndItsCost(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+checkpointSlack, checkpointChainSketch,
		"Finish the four pieces\nwhat is left, and everything this turn already found out")}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	marks := journaledMarks(t, path)
	if len(marks) != checkpointMarks {
		t.Fatalf("%d marks were journaled over a turn that crossed %d", len(marks), checkpointMarks)
	}
	for index, mark := range marks {
		if mark.N != index+1 {
			t.Errorf("mark %d is journaled as rung %d", index+1, mark.N)
		}
		if mark.Rounds != checkpointMarkAt(index+1) {
			t.Errorf("rung %d fired at round %d, want %d", mark.N, mark.Rounds, checkpointMarkAt(index+1))
		}
		if mark.Decision != checkpointDecisionContinue {
			t.Errorf("rung %d decided %q, want %q", mark.N, mark.Decision, checkpointDecisionContinue)
		}
		if mark.Sketch != "A > B > C" {
			t.Errorf("rung %d journaled the sketch as %q", mark.N, mark.Sketch)
		}
		if mark.Model != checkpointMarkModel {
			t.Errorf("rung %d was journaled against %q, want the mastermind %q",
				mark.N, mark.Model, checkpointMarkModel)
		}
	}
	// AND THE CEILING SAYS WHAT IT DID WITH THE TURN, with the node that took it.
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 1 {
		t.Fatalf("%d ceiling lines were journaled, want exactly one", len(ceilings))
	}
	if ceilings[0].Decision != checkpointCeilingMoved {
		t.Errorf("the ceiling journaled %q, want %q", ceilings[0].Decision, checkpointCeilingMoved)
	}
	if ceilings[0].Rounds != rounds {
		t.Errorf("the ceiling is journaled at round %d, want %d", ceilings[0].Rounds, rounds)
	}
	if ceilings[0].TaskID == 0 {
		t.Error("the ceiling moved the work and named no task")
	}
	// AND EVERY DOLLAR HAS A LINE. The errand's own call line is what makes the
	// journal's calls sum to the bill rather than to the turn's share of it.
	reads := 0
	for _, call := range journaledCalls(t, path) {
		if call.Role == string(roles.RoleMarkReader) {
			reads++
		}
	}
	if reads != checkpointMarks {
		t.Errorf("%d call lines name the mark reader, want one per mark (%d)", reads, checkpointMarks)
	}
}

// AND A MARK THAT NOBODY COULD READ IS JOURNALED AS A FAILURE AND NOT AS A
// CARRY-ON.
//
// They are the same thing to the turn and opposite things to anybody reading the
// file: one is a reader that looked and saw one job, the other is a mechanism
// that is not running at all.
func TestAMarkNobodyCouldReadIsJournaledAsAFailure(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	steps := grindingSteps(rounds+checkpointSlack, "", "Finish it\nwhat is left and what was found")
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return nil, errors.New("the reader is down")
			}
			return inner(ctx, messages)
		}
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	marks := journaledMarks(t, path)
	if len(marks) != checkpointMarks {
		t.Fatalf("%d marks were journaled, want one per rung", len(marks))
	}
	for _, mark := range marks {
		if mark.Decision != checkpointDecisionFailed {
			t.Errorf("rung %d journaled %q for a reader nobody could reach, want %q",
				mark.N, mark.Decision, checkpointDecisionFailed)
		}
	}
	// AND THE CEILING STILL MOVED THE WORK, which is the recovery bound the whole
	// fail-open design leans on.
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingMoved {
		t.Errorf("the ceiling behind a dead reader journaled %+v", ceilings)
	}
}

// ── the ceiling's drop is believed once ─────────────────────────────────────

// THE RUNNING MODEL IS BELIEVED ONCE, AND THEN IT IS MET.
//
// A model mid-grind declaring "everything is done" at round forty is that model
// grading its own work at the exact moment it has a reason to. It was measured:
// the handover was dropped and the same model then ground on for twenty more
// rounds unwatched.
//
// THE ANSWER TO THAT USED TO BE A SECOND READER and is now the turn's own budget
// ([checkpointMeter.believeDone]). Corroboration could not tell a model that was
// lying from a reader that had failed, drawn nothing, or drawn a shape without
// parts in it — and on the frozen revision cells it turned three finished
// requests into cold workers for exactly that reason
// (completion_stale_test.go). So the claim is granted, and a turn that then does
// [checkpointPrice] more rounds of real work has disproved it: the ceiling comes
// back, the claim is spent, and the work moves — on the person's own sentence,
// because a continuation that spent its answer on the token wrote no instruction
// to hand anybody.
func TestTheRunningModelsSayS0IsBelievedOnceAndThenMet(t *testing.T) {
	const asked = "write the eight files I listed and smoke-check them"

	// A CHAIN AT EVERY MARK: the reader never says the work is done and never
	// draws independent parts either, which is the live cells' own shape and the
	// one corroboration could say nothing useful about. The continuation says
	// nothing is left, at the ceiling and at the rung that comes back after it.
	rounds := checkpointMarkAt(checkpointMarks) + checkpointPrice + checkpointSlack
	steps := append(grindingSteps(rounds, checkpointChainSketch, checkpointNothingLeft),
		finalAnswer("done"))
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted; a turn that ground on past its own claim is not finished", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Errorf("the ceiling moved the work and said nothing; notices were %q", noticeTexts(collected))
	}
	// AND IT MOVED ON THE PERSON'S OWN WORDS, because the continuation wrote no
	// brief — the same fallback a dowry of machine markup gets.
	if node.spec.brief != asked {
		t.Errorf("the task runs on %q, want the person's own sentence %q", node.spec.brief, asked)
	}
	// AND THE FILE SAYS BOTH THINGS THAT HAPPENED, in order: the claim believed,
	// and the same claim refused when the turn carried on working past it. A run
	// that dropped once and a run that dropped forever used to read identically.
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 2 {
		t.Fatalf("the ceiling journaled %+v, want the drop and the ending after it", ceilings)
	}
	if ceilings[0].Decision != checkpointCeilingNothing {
		t.Errorf("the first ending journaled %q, want %q", ceilings[0].Decision, checkpointCeilingNothing)
	}
	if ceilings[1].Decision != checkpointCeilingMoved {
		t.Errorf("the second ending journaled %q, want %q", ceilings[1].Decision, checkpointCeilingMoved)
	}
	// AND THE SECOND ENDING IS THE ONE THAT COST A TURN'S WORK MORE than the
	// first, so a reader can see the grind the budget was spent on.
	if ceilings[1].Rounds < ceilings[0].Rounds+checkpointPrice {
		t.Errorf("the ceiling came back at round %d after a drop at %d, want %d rounds of work between them",
			ceilings[1].Rounds, ceilings[0].Rounds, checkpointPrice)
	}
}

// AND IT IS DROPPED WHEN THE SECOND MIND AGREES.
//
// Both readers say the same thing at the same moment — the reader's sketch is
// `(done)` and the continuation answers with the remains token — and that is
// evidence rather than a claim. Nothing happens: no task, no line, and the turn's
// own answer stands.
func TestTheCeilingIsDroppedWhenTheReaderAgreesNothingRemains(t *testing.T) {
	const answered = "all eight files are written and the smoke check passed"

	rounds := checkpointMarkAt(checkpointMarks)
	steps := append(grindingSteps(rounds+checkpointSlack, checkpointDoneSketch, checkpointNothingLeft),
		finalAnswer(answered))
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) { config.SessionFile = path })
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "write the eight files I listed and smoke-check them")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started out of a turn two readers agreed was finished", count)
	}
	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Errorf("the person was told their answer was being moved; notices were %q", noticeTexts(collected))
	}
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
		t.Errorf("the turn ended as a %s saying %q, want the answer it was about to give",
			last.Role, messageText(last))
	}
	// AND THE DROP IS IN THE FILE, which is the whole reason the measured failure
	// could not be attributed: a run that dropped and a run that never fired read
	// identically.
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 1 || ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the ceiling journaled %+v, want %q", ceilings, checkpointCeilingNothing)
	}
	if ceilings[0].TaskID != 0 {
		t.Errorf("a dropped ceiling named task %d", ceilings[0].TaskID)
	}
}

// AND A SPLIT'S HANDOVER CAN NEVER BE DROPPED, because a sketch with parts in it
// is a reader stating that work remains — it cannot corroborate a claim that none
// does.
func TestASplitIsNeverDroppedByTheRunningModelsDeclaration(t *testing.T) {
	completer := &scriptedCompleter{steps: grindingSteps(checkpointMarkAt(1)+6,
		checkpointSplitSketch, checkpointNothingLeft)}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at a split the running model declared finished", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Errorf("the split never said its line; notices were %q", noticeTexts(collected))
	}
}

// ── the reader sees what the model saw ──────────────────────────────────────

// EVERY VERB'S ARGUMENTS RENDER, AND THE LEDGER KNOWS NO VERB'S NAME FOR
// ANYTHING.
//
// The old rule was a list of five anticipated keys, and `fork` — which takes
// `parts` — drew as a bare line reading `fork`. So a measured turn that had
// already fanned out twice was described to the reader as two steps that touched
// nothing, and the reader sketched serial work over the top of it three times.
func TestEveryVerbsArgumentsRenderInTheLedger(t *testing.T) {
	for _, shape := range []struct {
		name      string
		arguments string
		want      []string
		absent    string
	}{
		{
			name:      "a fork renders its parts",
			arguments: `{"parts":[{"role":"the parser","scope":["p.go"]},{"role":"the handlers","scope":["h.go"]}]}`,
			want:      []string{"the parser", "the handlers"},
		},
		{
			name:      "a command renders itself",
			arguments: `{"command":"go test ./...","timeout":30}`,
			want:      []string{"go test ./..."},
		},
		{
			name:      "a search renders its pattern",
			arguments: `{"pattern":"validate(","path":"./internal"}`,
			want:      []string{"validate("},
		},
		{
			// THE PAYLOAD NEVER WINS, whichever order it arrived in. The first pass
			// skips any argument too long to be a ledger line, which is the generic
			// spelling of what the old list of keys bought by naming `path`.
			name:      "a write renders its path and never its contents",
			arguments: `{"content":"` + strings.Repeat("x", 4000) + `","path":"./workflow.yml"}`,
			want:      []string{"./workflow.yml"},
			absent:    strings.Repeat("x", 100),
		},
		{
			// A NUMBER SAYS NOTHING ABOUT WHAT WAS TOUCHED, so the walk carries on to
			// the argument that does.
			name:      "a leading number is stepped over",
			arguments: `{"lines":200,"deep":true,"path":"./money.go"}`,
			want:      []string{"./money.go"},
		},
		{
			// AND A VERB NOBODY ANTICIPATED IS DESCRIBED BADLY RATHER THAN NOT AT ALL.
			name:      "arguments with nothing sayable in them still draw a line",
			arguments: `{"depth":3,"recursive":true}`,
			want:      []string{"depth"},
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			line := checkpointArgument(shape.arguments)
			for _, want := range shape.want {
				if !strings.Contains(line, want) {
					t.Errorf("the ledger drew %q, want %q in it", line, want)
				}
			}
			if shape.absent != "" && strings.Contains(line, shape.absent) {
				t.Errorf("the ledger carried a payload: %q", line)
			}
			if len(line) > checkpointLedgerBytes {
				t.Errorf("the line is %d bytes against a bound of %d: %q",
					len(line), checkpointLedgerBytes, line)
			}
		})
	}
}

// AND WHAT A COMMAND ACTUALLY REPORTED REACHES THE READER — THE TAIL OF IT.
//
// This is the defect in one assertion. A ledger says a suite was run; the tail of
// its output says `Passed: 0`, and only one of those two lets a reader work out
// how much of the ask is discharged. The measured turn had exactly that line in
// front of it and the reader was shown neither it nor anything like it.
func TestAToolResultsTailReachesTheReader(t *testing.T) {
	const verdict = "Loaded 68186 golden test points across 12 suites — Passed: 0, Failed: 68186"
	const scroll = "compiling package number four hundred and ninety, which nobody needs to read\n"
	body := strings.Repeat(scroll, 200) + verdict

	digest := checkpointDigest("run the suite and make it pass", []ai.Message{
		toolCallMessage("c1", "bash", `{"command":"go test ./..."}`),
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	})

	if !strings.Contains(digest, verdict) {
		t.Fatalf("the one line that says how the ask is going never reached the reader:\n%s", digest)
	}
	// AND IT IS THE TAIL AND NOT THE OUTPUT. A result kept whole would put a
	// mastermind's bill back on the digest this file exists to keep off it.
	if strings.Count(digest, scroll) > checkpointResultBytes/len(scroll)+1 {
		t.Errorf("the whole of a result reached the reader:\n%s", digest)
	}
	if !strings.Contains(digest, "bash go test ./..."+checkpointResultArrow) {
		t.Errorf("the result is not under the call that made it:\n%s", digest)
	}
}

// AND A DIGEST OVER ITS BUDGET DROPS RESULTS BEFORE IT DROPS ANYTHING ELSE.
//
// The order of eviction is the whole of the priority rule: the boundary sections
// are fitted first and can never be squeezed out, then the ledger, which is the
// complete account of what was touched, and the results take what room is left —
// so the oldest results go first and the oldest ledger lines only after them.
func TestADigestOverBudgetDropsResultsBeforeItDropsTheAsk(t *testing.T) {
	const asked = "work through every package and report what is failing"
	messages := []ai.Message{textMessage("user", asked)}
	for index := 0; index < 300; index++ {
		id := fmt.Sprintf("c%d", index)
		messages = append(messages,
			toolCallMessage(id, "bash", fmt.Sprintf(`{"command":"go test ./pkg/%d"}`, index)),
			// The verdict is at the END of the output, where a real one is, so this
			// also asserts that what survives the per-result bound is the tail.
			ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text",
				Text: fmt.Sprintf("%s package %d FAILED", strings.Repeat("noise ", 200), index)}}})
	}
	messages = append(messages, textMessage("assistant", "the last thing this turn said"))

	digest := checkpointDigest(asked, messages)

	if len(digest) > checkpointDigestBytes {
		t.Fatalf("the digest is %d bytes against a bound of %d", len(digest), checkpointDigestBytes)
	}
	// THE ASK SURVIVES, WHOLE. It is the one thing on this road nobody may rewrite
	// and the one thing everything else is measured against.
	if !strings.HasPrefix(digest, checkpointDigestAsked+"\n"+asked) {
		t.Fatalf("the ask was squeezed out by the evidence:\n%s", digest[:400])
	}
	if !strings.Contains(digest, "the last thing this turn said") {
		t.Error("the last thing said was squeezed out by the evidence")
	}
	// AND SO DOES THE LEDGER, WHICH IS CHEAPER PER LINE AND IS THE COMPLETE
	// ACCOUNT: three hundred lines of it fit where three hundred results cannot.
	if strings.Contains(digest, "earlier steps") {
		t.Errorf("the ledger was cut to make room for results:\n%s", digest[:400])
	}
	if !strings.Contains(digest, "bash go test ./pkg/0\n") {
		t.Error("the ledger dropped its oldest line while results were still being carried")
	}
	// THE NEWEST RESULTS ARE THE ONES KEPT, and the oldest are the ones that went.
	found := digest[strings.Index(digest, checkpointDigestFound):]
	if !strings.Contains(found, "package 299 FAILED") {
		t.Errorf("the newest result was dropped, which is the evidence in front of the turn:\n%s", found)
	}
	if strings.Contains(found, "package 0 FAILED") {
		t.Error("a bounded results section kept its oldest line")
	}
	// AND IT SAYS WHAT IT DROPPED, for the ledger's own reason: a silent truncation
	// lets a reader believe the turn learned less than it did.
	if !strings.Contains(found, "earlier results") {
		t.Errorf("the results were truncated in silence:\n%s", found)
	}
}

// ── the handoff is drafted by the runner and written by the mastermind ──────

// THE BRIEF A WORKER OPENS ON IS WRITTEN BY SOMEBODY WHO DID NOT SPEND THE TURN.
//
// The runner holds the findings and drafts; the mastermind holds the judgement
// and writes. Asking one model to be both was the shipping arrangement and it was
// measured producing 5,882 characters of loop that a cold worker was then started
// on as its whole world.
func TestTheHandoffIsDraftedByTheRunnerAndWrittenByTheMastermind(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const draft = "Finish the four pieces, and the auth test is the one still failing."
	const written = "Finish the currency module. The auth test is the one still failing and the yaml " +
		"route has been ruled out. It is done when the whole suite is green."

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, draft, written)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	// THE WORKER OPENS ON WHAT THE MASTERMIND WROTE, not on the draft.
	if !strings.Contains(node.spec.brief, written) {
		t.Fatalf("the worker's brief is not the written handoff:\n%s", node.spec.brief)
	}
	// AND THE WRITER WAS SHOWN THE FOUR THINGS IT IS OWED: the person's own words,
	// an account of the turn WITH what came back in it, and the draft.
	page := ""
	for index := range completer.requests() {
		if askedToWriteHandoff(completer.request(index)) {
			page = messageText(completer.request(index)[0])
			if completer.model(index) != checkpointMarkModel {
				t.Errorf("the handoff was written by %q, want the mastermind %q",
					completer.model(index), checkpointMarkModel)
			}
		}
	}
	if page == "" {
		t.Fatal("nobody was ever asked to write the handoff")
	}
	for _, want := range []string{
		checkpointHandoffAskedHeading, asked,
		checkpointHandoffWorkHeading, checkpointDigestDone,
		checkpointHandoffDraftHeading, draft,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the writer was never shown %q:\n%s", want, page)
		}
	}
	// AND THE CALL IS ON THE BILL UNDER ITS OWN NAME. A mastermind-priced line with
	// nothing beside it saying what it bought is the shape of bill the mark
	// reader's own journal line already exists because of.
	billed := 0
	for _, call := range journaledCalls(t, path) {
		if call.Role == string(roles.RoleHandoff) {
			billed++
		}
	}
	if billed != 1 {
		t.Errorf("%d journaled calls name the handoff writer, want exactly one", billed)
	}
}

// AND THE WRITER STILL WRITES WHEN THE DRAFT FAILED, WHICH IS WHY THE BARE ASK IS
// NOW THE LAST RESORT AND NOT THE SECOND OPTION.
//
// Across the twenty-two handoffs of the measured run, seven fell straight through
// to the person's bare sentence — a sentinel or an empty answer from a tired
// model — and a bare sentence hands a worker everything the turn found out except
// the findings. The writer has the digest whatever the runner managed to say.
func TestTheHandoffWriterStillWritesWhenTheDraftFailed(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const written = "Finish the currency module; the arithmetic and validation modules are already done."

	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, dsmlSentinel, written)}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	if !strings.Contains(node.spec.brief, written) {
		t.Fatalf("a refused draft dropped the whole handoff; the worker got:\n%s", node.spec.brief)
	}
	if strings.Contains(node.spec.brief, dsmlSentinel) {
		t.Errorf("the sentinel reached the worker:\n%s", node.spec.brief)
	}
}

// AND A BRIEF THAT HAS STOPPED SAYING NEW THINGS IS NOT A BRIEF.
//
// [briefIsProse] asks whether an answer is words at all; this asks whether those
// words are still going somewhere. A repeat loop passes every test for shape,
// which is exactly how 85 clauses with 39 distinct among them became a worker's
// whole world.
func TestABriefThatHasStoppedSayingNewThingsIsNotABrief(t *testing.T) {
	loop := strings.Repeat("Also: rerun the suite. Also: check the parser. Also: wire the handlers. "+
		"Also: rerun the suite. Also: check the parser. Also: wire the handlers. ", 14)
	if !briefRepeats(loop) {
		t.Errorf("a brief that says six things forty times over was accepted")
	}
	// AND A HEAVILY FORMATTED ONE IS THE SAME SENTENCE TO THIS. Emphasis, case and
	// punctuation are folded away, so a loop cannot be dressed past the check.
	dressed := strings.Repeat("**Also: rerun the suite.**\n- also, RERUN the suite!\n", 10)
	if !briefRepeats(dressed) {
		t.Errorf("a loop written in markdown was accepted")
	}
	// AND AN HONEST BRIEF IS NOT REFUSED, however consistent its vocabulary.
	honest := "Finish the currency module. The auth test is the one still failing, and it fails on the " +
		"rounding rather than on the lookup. The yaml route has been ruled out: the parser cannot " +
		"see the tag. The arithmetic module is done and its tests are green. The validation " +
		"workflow is written but not wired. Wire it into the job that runs on push. " +
		"Do not touch the fixtures. It is done when the whole suite is green on a clean checkout."
	if briefRepeats(honest) {
		t.Errorf("an honest brief was read as a loop:\n%s", honest)
	}
	// AND A SHORT BRIEF IS NEVER DEGENERATE. A ratio over four sentences is noise,
	// and refusing one would cost a regeneration on the document that needed none.
	if briefRepeats("Wire the handlers. Wire the handlers. Wire the handlers.") {
		t.Errorf("a brief too short to judge was judged")
	}
}

// AND A DEGENERATE BRIEF BUYS ONE REGENERATION AND THEN THE LADDER TAKES OVER.
//
// A mastermind that looped once may not loop twice; one that looped twice is one
// that is going to. What catches the second failure is the fallback ladder, which
// costs nothing: the runner's own draft, and then the person's sentence.
func TestADegenerateHandoffIsRegeneratedOnceAndThenGivenUpOn(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const draft = "Finish the four pieces, and the auth test is the one still failing."
	loop := strings.Repeat("Also: rerun the suite. Also: check the parser. Also: wire the handlers. ", 20)

	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack+2,
		checkpointChainSketch, draft, loop)}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	// THE LOOP NEVER BECAME A SPEC.
	if strings.Contains(node.spec.brief, "Also: rerun the suite. Also: rerun the suite.") {
		t.Fatalf("a degenerate brief became a worker's whole world:\n%s", node.spec.brief)
	}
	if !strings.Contains(node.spec.brief, draft) {
		t.Errorf("the ladder did not fall back to the runner's own draft:\n%s", node.spec.brief)
	}
	// AND IT WAS ASKED FOR TWICE AND NOT FOREVER.
	asks := 0
	for index := range completer.requests() {
		if askedToWriteHandoff(completer.request(index)) {
			asks++
		}
	}
	if asks != checkpointBriefTries {
		t.Errorf("the writer was asked %d times, want %d", asks, checkpointBriefTries)
	}
}

// ── the acceptance stays the person's ───────────────────────────────────────

// THE SPEC THE CEILING BUILDS IS FINISHED AGAINST THE PERSON'S ASK, AND THE BRIEF
// IS THE STATE UNDER IT.
//
// They are two documents and they were collapsing into one. A handoff brief says
// what is left of the work RIGHT NOW; finishing a task against that is finishing
// it against a to-do the turn happened to be holding, which is how a ten-hour ask
// was measured being accepted as met once the code compiled.
func TestTheSpecTheCeilingBuildsIsFinishedAgainstThePersonsAsk(t *testing.T) {
	const asked = "port the whole language server and get every golden test passing"
	const written = "Finish the type-checker. The parser is done and the 12 protocol methods are enumerated."

	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, "a draft that names the compile errors", written)}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	// THE ACCEPTANCE IS THEIRS.
	if !strings.Contains(node.spec.acceptance, asked) {
		t.Fatalf("the work is finished against %q, want the person's own words", node.spec.acceptance)
	}
	// AND IT IS NOT THE BRIEF'S LOCAL TO-DO.
	if strings.Contains(node.spec.acceptance, written) {
		t.Errorf("the handoff brief became the done-condition:\n%s", node.spec.acceptance)
	}
	// AND THE BRIEF IS STILL THE STATE, which is what a worker opens on.
	if !strings.Contains(node.spec.brief, written) {
		t.Errorf("the brief is not the state the turn handed over:\n%s", node.spec.brief)
	}
	// AND THE CHECKER READS THE PERSON'S QUESTION, which is the only place any of
	// this actually lands (task_audit.go).
	if question := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, landingFiles{}, "", nil); !strings.Contains(question, asked) {
		t.Fatalf("the checker was asked %q, want the person's own words in it", question)
	}
}

// ── every turn is metered ───────────────────────────────────────────────────

// A WOKEN TURN IS METERED EXACTLY LIKE A TYPED ONE.
//
// It was not, and that was the whole of the measured idleness: a task landed, the
// note started a turn, and that turn made 127 tool calls over 46 minutes with no
// mark, no ceiling and no handover — it ended when the model stopped talking, and
// the harness then sat idle for the remaining seven and a half hours of the ask.
func TestAWokenTurnIsMeteredExactlyLikeATypedOne(t *testing.T) {
	const asked = "port the whole language server and get every golden test passing"

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, "a draft", "a written brief that somebody could work from")}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	// The person's ask, as the session already holds it: this turn is not the one
	// they typed, and what it is measured against is still their sentence.
	agent.mu.Lock()
	agent.personAsk = asked
	agent.mu.Unlock()

	// A TASK LANDS, which is the one road [Agent.wakeLocked] opens.
	settleTask(t, agent, "the first piece", "the first piece is done")

	waitFor(t, "the woken turn to be read at its first mark", func() bool {
		return marksRead(completer) > 0
	})
	waitFor(t, "the mark to reach the journal", func() bool {
		return len(journaledMarks(t, path)) > 0
	})
	marks := journaledMarks(t, path)
	if marks[0].Rounds != checkpointMarkAt(1) {
		t.Errorf("the woken turn's first mark fired at round %d, want the price %d",
			marks[0].Rounds, checkpointMarkAt(1))
	}
	// AND A LINE THE SESSION WROTE THAT NOBODY OWES AN ANSWER FOR IS STILL LEFT
	// ALONE, which is the half of the old rule that was right.
	ambient := userText("the session's own note")
	ambient.authored = true
	if agent.checkpoints(context.Background(), ambient) {
		t.Error("an ambient note the session wrote to itself is metered")
	}
}

// A WOKEN TURN THAT STOPS SHORT IS RE-OPENED EVEN WHEN IT IS TOO CHEAP TO HAVE
// PAID A READER, WHICH A PERSON'S TURN IS NOT.
//
// This is SWE-Marathon s14, 18:16Z, to the round: a task came home failed, the
// note woke a turn, it read for six rounds — four under the first rung — and
// sealed on a stated next step with no question. The price gate that rightly
// leaves a person's cheap turn alone left this one unread, and the best clean
// seed of the run settled idle with seven and a half of its ten hours unspent.
// A woken turn has nobody sitting in front of it to carry the work on, so the
// cheapness that protects a typed turn does not protect this one.
func TestAWokenTurnThatStopsShortIsReopenedEvenWhenCheap(t *testing.T) {
	const stopped = "This is a large, multi-part problem. Let me diagnose the specific " +
		"failures systematically rather than rewriting everything."
	const remains = "implementation is 0/322 and semanticTokens is 0/60; nothing is wired " +
		"and the golden tests still fail"

	var remainsAsks atomic.Int64
	// SIX ROUNDS, four under the first rung — the exact depth s14 sealed at, and
	// the depth a typed turn is left unread at (see the test below this one).
	completer := &scriptedCompleter{steps: stoppingSteps(checkpointMarkAt(1)-4, stopped, func() string {
		if remainsAsks.Add(1) == 1 {
			return remains
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})
	// The person's ask, as the session already holds it: the woken turn is not the
	// one they typed, and what its remains are read against is still their sentence.
	agent.mu.Lock()
	agent.personAsk = "port the whole language server and get every golden test passing"
	agent.mu.Unlock()

	// A TASK LANDS, which is the one road [Agent.wakeLocked] opens.
	settleTask(t, agent, "the first piece", "the first piece is done")

	waitFor(t, "the cheap woken turn's remains to be read", func() bool {
		return remainsAsks.Load() > 0
	})
	waitFor(t, "the cheap woken turn to be re-opened on what is left", func() bool {
		return strings.Contains(transcriptText(agent), checkpointCarryOnLead+remains)
	})
}

// AND THE WAKE CARVE-OUT STOPS AT THE PERSON'S OWN QUESTION.
//
// Lifting the price gate for a woken turn must not lift the exclusion that
// matters most: a turn whose last words ask the PERSON something is waiting, not
// stopping, and re-opening it would answer a question addressed to somebody else.
// That gate runs in front of the price gate, so a cheap woken turn that ends on a
// question is left alone exactly as a typed one is — no reader is spent on it.
func TestAWokenTurnEndingOnAQuestionIsNotReopened(t *testing.T) {
	var remainsAsks atomic.Int64
	// THREE ROUNDS, under the first rung: only the wake carve-out could arm the
	// reader here, and only the question can be keeping the turn shut.
	completer := &scriptedCompleter{steps: stoppingSteps(3,
		"I could wire the handlers or the folding ranges first. **Which should I do?**", func() string {
			remainsAsks.Add(1)
			return "there is work left"
		})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})
	agent.mu.Lock()
	agent.personAsk = "port the language server and get the golden tests passing"
	agent.mu.Unlock()

	settleTask(t, agent, "the first piece", "the first piece is done")
	waitForQuiet(t, agent)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a woken turn waiting on the person was read %d times for what remains", got)
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Errorf("a woken turn waiting on an answer was carried on:\n%s", transcriptText(agent))
	}
}

// AND A WOKEN TURN THAT RUNS LONG HITS THE SAME CEILING AND MOVES WORK TO A TASK.
//
// The ceiling is not a person's-turn rule with a wake exception bolted on: it is
// the one meter, and a woken turn climbs the same ladder to it. A task landing
// starts a turn, the turn grinds past the ceiling, and what is left is handed to
// exactly one governed task on the one road — the same ending a typed turn gets.
func TestAWokenTurnPastTheCeilingMovesWorkToATask(t *testing.T) {
	// Grinds past the ceiling and answers the mastermind's handoff when it is
	// asked — the identical script a typed turn hands over on.
	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, "a draft", "a brief somebody could work from")}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })
	agent.mu.Lock()
	agent.personAsk = "port the whole language server and get every golden test passing"
	agent.mu.Unlock()

	// A TASK LANDS AND WAKES A TURN: the note carries wake, [Agent.wakeLocked]
	// opens the turn on it, and it grinds like any other.
	if !agent.enqueueNote(wakeNote("task 1 is done · the first piece landed")) {
		t.Fatal("the wake note was not taken")
	}
	node := ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted when the woken turn hit the ceiling, want exactly one", count)
	}
	// AND IT IS THE PERSON'S OWN ASK THE WORK GOES OUT AGAINST, not the wake note.
	if !strings.Contains(node.spec.acceptance, "port the whole language server") {
		t.Errorf("the handed-over work is finished against %q, want the person's own words", node.spec.acceptance)
	}
}

// ── a turn ends; the ask does not ───────────────────────────────────────────

// A TURN THAT STOPS SHORT OF THE ASK IS RE-OPENED, ONCE, WITH WHAT IS LEFT.
//
// "I've finished the parser, next I'll wire the handlers" ends a turn exactly as
// firmly as a finished job does, and until this nothing checked which of the two
// it was. All three harnesses in the measured comparison stopped with hours of
// the ask unused.
func TestATurnThatStopsShortOfTheAskIsReopened(t *testing.T) {
	const stopped = "I've finished the parser, next I'll wire the handlers"
	const remains = "the handlers are not wired and the golden tests have never been run"

	var remainsAsks atomic.Int64
	// Past the ladder's first rung, which is what arms the reader at all: a turn
	// the meter never charged one reading for is never read at its end either.
	completer := &scriptedCompleter{steps: stoppingSteps(checkpointMarkAt(2), stopped, func() string {
		if remainsAsks.Add(1) == 1 {
			return remains
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 2 {
		t.Fatalf("the ask was read %d times; want one that re-opened and one that let the turn end", got)
	}
	// THE CONTINUATION IS THE READER'S OWN LINE, AND IT SAYS WHO IS SPEAKING.
	transcript := transcriptText(agent)
	if !strings.Contains(transcript, checkpointCarryOnLead+remains) {
		t.Errorf("the turn was not re-opened on what the reader said is left:\n%s", transcript)
	}
	// AND THE PERSON IS TOLD, once, in the register the other three notes use.
	if !saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("nobody said why the turn kept going; notices were %q", noticeTexts(collected))
	}
}

// AND A TURN THAT ENDS BY ASKING THE PERSON SOMETHING IS NEVER RE-OPENED.
//
// It is WAITING rather than stopping, and re-opening it would be the harness
// answering a question that was addressed to somebody else. It is read
// structurally — a question mark on the last words — because a list of openers
// would be a rule about English.
func TestATurnThatEndsOnAQuestionToThePersonIsNotReopened(t *testing.T) {
	var remainsAsks atomic.Int64
	// Past the first rung, so the reader is armed and the question is the only
	// thing left that can be keeping the turn shut.
	completer := &scriptedCompleter{steps: stoppingSteps(checkpointMarkAt(2),
		"Two schemas would both work here. **Which one should I use?**", func() string {
			remainsAsks.Add(1)
			return checkpointNothingLeft
		})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn waiting on the person was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn waiting on an answer was carried on: %q", noticeTexts(collected))
	}
	// AND THE STRUCTURE IS THE WHOLE OF THE RULE, decoration and all.
	for _, waiting := range []string{"which one?", "**which one?**", `"which one?"`, "which one? "} {
		if !endsAskingThePerson(waiting) {
			t.Errorf("%q was not read as a question to the person", waiting)
		}
	}
	for _, stopping := range []string{"", "I've finished the parser.", "next: the handlers"} {
		if endsAskingThePerson(stopping) {
			t.Errorf("%q was read as a question to the person", stopping)
		}
	}
}

// AND A RE-OPENED TURN IS ON THE SAME METER, SO IT STILL MARKS.
//
// The bound on re-opening is the meter and nothing else — no second counter — so
// a re-open is charged as a round, the marks still fire, and a re-opened turn
// that reaches the ceiling hands off exactly as any other does.
func TestAReopenedTurnStillMarksOnTheSameMeter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var remainsAsks atomic.Int64
	// One short of the SECOND mark and then words: the turn is past the first rung,
	// so the reader is armed, and the re-open is the round that crosses the second.
	completer := &scriptedCompleter{steps: stoppingSteps(checkpointMarkAt(2)-1,
		"I've finished the parser, next I'll wire the handlers", func() string {
			if remainsAsks.Add(1) == 1 {
				return "the handlers are not wired"
			}
			return checkpointNothingLeft
		})}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	marks := journaledMarks(t, path)
	if len(marks) < 2 {
		t.Fatalf("a re-opened turn was read at %d marks, want the rung it climbed and the one the "+
			"re-open crossed; the reader was asked %d times about the ask", len(marks), remainsAsks.Load())
	}
	if marks[1].Rounds != checkpointMarkAt(2) {
		t.Errorf("the second mark fired at round %d, want %d — a re-open is a round",
			marks[1].Rounds, checkpointMarkAt(2))
	}
}

// AND A TURN SHORTER THAN THE FIRST MARK IS NEVER READ UNLESS IT CHANGED THE
// TREE.
//
// THE PRICE GATE IS THE MARK LADDER'S FIRST RUNG, and it is there because one
// `bash ls` is one round: gated on a single finished round, EVERY small turn that
// touched a tool paid a mastermind call worth a third to a half of its own bill,
// and in eight of nine measured runs that call re-opened nothing.
//
// THE TURN HERE ONLY LOOKED, which is what leaves the price gate standing. A
// cheap turn that only read cannot have left a job half-done — the worst it can
// have left is a question — so it is priced exactly as it was before the exposure
// arm existed and pays no reader. What the arm answers is the OTHER kind of cheap
// turn, the one that wrote and stopped
// ([TestATurnThatWroteAndThenStoppedIsReadWhateverItCost]), and the two tests are
// one rule read from both sides.
func TestATurnShorterThanTheFirstMarkIsNeverReadUnlessItChangedTheTree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var remainsAsks atomic.Int64
	// One short of the first rung, and the reader would say there is work left if
	// anybody asked it — so nothing but the gate can be keeping the turn shut.
	completer := &scriptedCompleter{steps: stoppingSteps(checkpointMarkAt(1)-1,
		"I've finished the parser, next I'll wire the handlers", func() string {
			remainsAsks.Add(1)
			return "the handlers are not wired"
		})}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "list the folder and tell me what is in it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn that never reached the first rung was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn cheaper than one reading was carried on: %q", noticeTexts(collected))
	}
	// AND THE BILL IS THE POINT, so it is the bill that is pinned: no reader line in
	// the journal at all, which is the whole of what the measurement said was wasted.
	for _, call := range journaledCalls(t, path) {
		if call.Role == string(roles.RoleMarkReader) {
			t.Errorf("a turn of %d rounds paid the mark reader %q (%.5f USD)",
				checkpointMarkAt(1)-1, call.Model, call.CostUSD)
		}
	}
}

// A CHEAP TURN THAT WROTE AND THEN STOPPED IS READ FOR WHAT REMAINS ANYWAY.
//
// THE READER IS GATED ON EXPOSURE, NOT ON PRICE. SWE-Marathon s10, 10:35Z: a turn
// woken by a job's exit ran six rounds — four under the first rung — overwrote an
// 18,771-byte source file, said "now let me build and run the full test suite"
// with no tool call, and sealed. The price gate returned early, nothing read the
// turn, the unbuilt file it had just written was the six compile errors the run
// shipped with, and three hours of the ask went unspent.
//
// The rounds were cheap. The exposure was total, and it is the exposure this arm
// prices: the turn's LAST act was a write, so nothing has looked at what it did.
func TestATurnThatWroteAndThenStoppedIsReadWhateverItCost(t *testing.T) {
	const stopped = "Now let me build and run the full test suite"
	const remains = "analysis.rs was rewritten and never compiled; the build has not been run"

	var remainsAsks atomic.Int64
	// SIX ROUNDS, WHICH IS UNDER THE FIRST RUNG, and the sixth is the write.
	calls := append(readingCalls(5), fileWriteCall("analysis.rs", "fn analyze() {}\n"))
	completer := &scriptedCompleter{steps: stoppingStepsCalling(calls, stopped, func() string {
		if remainsAsks.Add(1) == 1 {
			return remains
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port analysis.rs and get the tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got == 0 {
		t.Fatalf("a turn that wrote and stopped was never read for what remains; it made %d rounds, "+
			"and the first rung is %d", len(calls), checkpointMarkAt(1))
	}
	if !strings.Contains(transcriptText(agent), checkpointCarryOnLead+remains) {
		t.Errorf("the turn was not re-opened on what the reader said is left:\n%s", transcriptText(agent))
	}
	if !saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("nobody said why the turn kept going; notices were %q", noticeTexts(collected))
	}
}

// AND A TURN THAT RAN A CHECK OVER WHAT IT WROTE IS NOT EXPOSED.
//
// The change WAS looked at, by the one party that could look at it, and the
// harness has no way of telling a build from a test from a read-back and no
// business trying: anything after the write is the turn checking itself. So the
// exposure arm answers no and the turn goes back on the price gate like every
// other cheap turn — it may still be read at the dear end of the ladder, but it
// is not read for free.
func TestATurnThatCheckedWhatItWroteIsNotExposed(t *testing.T) {
	var remainsAsks atomic.Int64
	// The same six rounds as the turn above, with the build it actually ran.
	calls := append(readingCalls(4),
		fileWriteCall("analysis.rs", "fn analyze() {}\n"),
		commandCall("cargo build"))
	completer := &scriptedCompleter{steps: stoppingStepsCalling(calls,
		"The build is clean. Next I will wire the handlers.", func() string {
			remainsAsks.Add(1)
			return "the handlers are not wired"
		})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port analysis.rs and get the tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn that checked its own write was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn that checked itself was carried on: %q", noticeTexts(collected))
	}
}

// AND A TURN THAT TOUCHED NOTHING IS NEVER EXPOSED BY THE TURN BEFORE IT.
//
// The ledger walks the whole transcript, so "the last call was a write" stays
// true of a conversation long after the turn that wrote ended. A person who types
// "thanks" next must not pay a reader for it: a turn with no rounds of its own
// owns no call at the newest end of that transcript, and the price gate holds.
func TestATurnWithNoRoundsIsNotExposedByAnEarlierTurnsWrite(t *testing.T) {
	var remainsAsks atomic.Int64
	answer := func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		// The reader would say there is work left if anybody asked it, so nothing
		// but the gate can be keeping this turn shut.
		if askedForRemains(messages) {
			remainsAsks.Add(1)
			return textResponse("there is more to do"), nil
		}
		return textResponse("You are welcome."), nil
	}
	agent := checkpointAgent(t, &scriptedCompleter{steps: []step{answer, answer, answer, answer}})
	stubbedGraph(agent, func(node *TaskNode) {})
	// A whole turn's worth of writing, already in the transcript, with nothing
	// after it — which is exactly what the arm reads as exposure.
	writtenTurn(agent, "port analysis.rs", 3)

	events, err := agent.Submit(context.Background(), "thanks")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn that touched nothing was read %d times on the previous turn's write", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn that touched nothing was carried on: %q", noticeTexts(collected))
	}
}

// AND THE READING ITSELF IS THE DIGEST'S OWN FACT, held to its four cases
// directly so that a change to [checkpointLedger] cannot quietly move the arm.
func TestExposureIsTheLastThingTheTurnDid(t *testing.T) {
	ls := func(path string) ai.Message {
		return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: path, Function: ai.ToolCallFunction{Name: "ls", Arguments: `{"path":"` + path + `"}`},
		}}}
	}
	wrote := func(path string) ai.Message {
		return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: path, Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"` + path + `"}`},
		}}}
	}
	for _, c := range []struct {
		what     string
		messages []ai.Message
		want     bool
	}{
		{"a turn that touched nothing", nil, false},
		{"a turn that only read", []ai.Message{ls("a"), ls("b")}, false},
		{"a turn whose last act was a write", []ai.Message{ls("a"), wrote("b")}, true},
		{"a turn that wrote and then looked", []ai.Message{wrote("a"), ls("b")}, false},
		{"a turn that wrote, looked, and wrote again", []ai.Message{wrote("a"), ls("b"), wrote("c")}, true},
	} {
		if got := turnLeftTheTreeUnchecked(c.messages); got != c.want {
			t.Errorf("%s: exposed = %v, want %v", c.what, got, c.want)
		}
	}
}

// ── the fixtures these use ──────────────────────────────────────────────────

// handoffSteps is [grindingSteps] with the mastermind that WRITES the brief
// scripted too: the sketch it draws at a mark, the draft the running model
// writes, and the document the writer makes out of it.
func handoffSteps(count int, sketch, draft, written string) []step {
	steps := grindingSteps(count, sketch, draft)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedToWriteHandoff(messages) {
				return textResponse(written), nil
			}
			return inner(ctx, messages)
		}
	}
	return steps
}

// stoppingSteps is a turn that does `rounds` tool calls and then STOPS IN WORDS,
// however many times it is re-opened — which is what makes it a fixture for the
// end of a turn rather than for the ceiling. remains answers the question a
// turn's end puts to the reader, and is a function so a test can say something
// different the second time it is asked.
// scriptedWorkingNote is the sentence a scripted long turn writes beside its
// calls. It exists because a turn of twenty tool calls with NOTHING visible
// between them is now held rather than run (processrule.go), and a test about
// the checkpoint ladder must not accidentally be a test of that rule. A model
// that says one line per step is the ordinary shape these tests mean to script.
const scriptedWorkingNote = "looking at the next piece, then I will say what I found"

func stoppingSteps(rounds int, stopped string, remains func() string) []step {
	var done atomic.Int64
	steps := make([]step, rounds+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from, written by the mastermind"), nil
			}
			if askedForRemains(messages) {
				return textResponse(remains()), nil
			}
			if call := done.Add(1); call <= int64(rounds) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), scriptedWorkingNote), nil
			}
			return textResponse(stopped), nil
		}
	}
	return steps
}

// writtenTurn is [workedTurn] for a turn that ENDED ON A WRITE: the calls it made
// are writes, so the newest thing in the transcript is a change nothing looked at.
func writtenTurn(a *Agent, asked string, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.personAsk = asked
	a.messages = append(a.messages, textMessage("user", asked))
	for index := 1; index <= calls; index++ {
		id := fmt.Sprintf("written-%d", index)
		a.messages = append(a.messages, ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: id,
			Function: ai.ToolCallFunction{
				Name:      "write",
				Arguments: fmt.Sprintf(`{"path":"./%d.rs","content":"fn main() {}"}`, index),
			},
		}}})
		a.messages = append(a.messages, ai.Message{
			Role: "tool", ToolCallID: id,
			Content: []ai.ContentPart{{Type: "text", Text: "Successfully wrote 13 bytes"}},
		})
	}
	a.messages = append(a.messages, textMessage("assistant", "Now let me build and run the full test suite"))
}

// workedTurn puts a turn's worth of work into an agent by hand: the person's ask
// and n finished tool calls. It is what the digest is assembled out of, so a test
// about the reader needs one before there is anything to read.
func workedTurn(a *Agent, asked string, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.personAsk = asked
	for index := 0; index < calls; index++ {
		a.messages = append(a.messages, toolCallMessage(fmt.Sprintf("call-%d", index), "read",
			fmt.Sprintf(`{"path":"./%d.txt"}`, index)))
	}
}

// toolCallMessage is one assistant message that called one tool and said nothing,
// which is what nearly every message of a grinding turn is.
func toolCallMessage(id, name, arguments string) ai.Message {
	return ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: ""}},
		ToolCalls: []ai.ToolCall{{ID: id, Type: "function",
			Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}},
	}
}

// journaledMarks, journaledCeilings and journaledCalls read one kind of line back
// out of a session file. They parse the file rather than a struct the agent kept,
// because the file is what the bench reads.
func journaledMarks(t *testing.T, path string) []journalMark {
	t.Helper()
	var marks []journalMark
	for _, entry := range journaledEntries(t, path, "mark") {
		if entry.Mark != nil {
			marks = append(marks, *entry.Mark)
		}
	}
	return marks
}

func journaledCeilings(t *testing.T, path string) []journalCeiling {
	t.Helper()
	var ceilings []journalCeiling
	for _, entry := range journaledEntries(t, path, "ceiling") {
		if entry.Ceiling != nil {
			ceilings = append(ceilings, *entry.Ceiling)
		}
	}
	return ceilings
}

func journaledCalls(t *testing.T, path string) []journalCall {
	t.Helper()
	var calls []journalCall
	for _, entry := range journaledEntries(t, path, "call") {
		if entry.Call != nil {
			calls = append(calls, *entry.Call)
		}
	}
	return calls
}

func journaledEntries(t *testing.T, path, kind string) []sessionEntry {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	var entries []sessionEntry
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type != kind {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// scriptedCall is one tool call a turn makes, as the script names it.
type scriptedCall struct{ tool, arguments string }

// readingCalls is n calls that only LOOK, each at a different path so the loop
// detector has nothing to say about them.
func readingCalls(n int) []scriptedCall {
	calls := make([]scriptedCall, 0, n)
	for index := 1; index <= n; index++ {
		calls = append(calls, scriptedCall{"ls", fmt.Sprintf(`{"path":"./%d"}`, index)})
	}
	return calls
}

// fileWriteCall and commandCall are the two verbs the exposure arm tells apart:
// one CHANGES the working tree and one looks at what changed.
func fileWriteCall(path, content string) scriptedCall {
	arguments, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	return scriptedCall{"write", string(arguments)}
}

func commandCall(command string) scriptedCall {
	arguments, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	return scriptedCall{"bash", string(arguments)}
}

// stoppingStepsCalling is [stoppingSteps] with the calls NAMED BY THE CALLER.
//
// The read-only fixture says `ls` for every round, which is exactly the wrong
// shape for the exposure arm: what a turn TOUCHED is the whole of what that arm
// reads, so a test about it has to be able to spell `write` and `bash` in the
// order the measured turn spelled them.
func stoppingStepsCalling(calls []scriptedCall, stopped string, remains func() string) []step {
	var done atomic.Int64
	steps := make([]step, len(calls)+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from, written by the mastermind"), nil
			}
			if askedForRemains(messages) {
				return textResponse(remains()), nil
			}
			if call := done.Add(1); call <= int64(len(calls)) {
				made := calls[call-1]
				return toolResponse(fmt.Sprintf("call-%d", call), made.tool, made.arguments), nil
			}
			return textResponse(stopped), nil
		}
	}
	return steps
}

// ── coordination is not a division, and watching is not working ─────────────
//
// The two halves of the same live failure (#276), 2026-09-01. A conversation had
// four pieces out and spent a turn watching them: reading their logs, waiting for
// what they sent back. The turn crossed a mark on the strength of that watching
// alone; the honest sketch of a turn like that is "wait for the second | wait for
// the third | wait for the fourth", which is three parts by the separator; and the
// harness converted it into a task, twice, whose whole brief was to review reports
// and accept work that a worker in its own copy cannot see. Two junk tasks, about
// fifteen minutes of node time, and a rail the person had to distrust.

// A DRAWING OF THE CONVERSATION'S OWN COORDINATION IS NOT A DIVISION.
//
// The table is the invariant. What makes a part real is that somebody else could
// be given it, and neither a wait nor a bare verb over work already out is
// anything anybody can be given — while a verb WITH SOMETHING AFTER IT is
// ordinary work and still splits, which is the half that keeps this from eating
// the road it stands beside.
func TestADrawingOfTheConversationsOwnCoordinationIsNotADivision(t *testing.T) {
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
			sketch := parseCheckpointSketch(one.shape + "\nA sentence naming the letters.")
			if sketch.handsBack != one.handsBack {
				t.Errorf("%q reads as hands-back %v, want %v", one.shape, sketch.handsBack, one.handsBack)
			}
			if sketch.split() != one.split {
				t.Errorf("%q reads as a split %v, want %v (%d parts)", one.shape, sketch.split(), one.split, sketch.parts)
			}
			// AND THE DIVISION A DRAWING PROPOSES IS THE SAME ANSWER, because a
			// harness that refused to convert a turn on a drawing and then handed
			// the same drawing to a worker would be two answers to one question.
			if proposed := (drawnDivision{sketch: sketch}).proposes(); proposed != one.split {
				t.Errorf("%q proposes a division %v while the split says %v", one.shape, proposed, one.split)
			}
		})
	}
}

// AND THE REFUSAL IS WRITTEN DOWN THE WAY `(done)` IS.
//
// A carry-on and a hand-back both leave the turn running, and a file that spelled
// them alike could not tell a turn that is one long job from a turn that was only
// ever watching its own pieces.
func TestAHandBackIsJournalledAsItsOwnDecision(t *testing.T) {
	waiting := parseCheckpointSketch("(waiting)\nThe three pieces are still out.")
	if got := waiting.carryOnDecision(); got != checkpointDecisionWaiting {
		t.Errorf("a hand-back journals as %q, want %q", got, checkpointDecisionWaiting)
	}
	chain := parseCheckpointSketch(checkpointChainSketch)
	if got := chain.carryOnDecision(); got != checkpointDecisionContinue {
		t.Errorf("one long job journals as %q, want %q", got, checkpointDecisionContinue)
	}
	if checkpointDecisionWaiting == checkpointDecisionContinue {
		t.Error("the two carry-ons are spelled the same, so nothing can tell them apart afterwards")
	}
}

// AND THE ASK TEACHES THE TOKEN, because a reader that was never told how to say
// it draws the coordination as parts instead.
func TestTheSketchAskNamesTheWaitingToken(t *testing.T) {
	if !strings.Contains(checkpointSketchAsk, "(waiting)") {
		t.Errorf("the ask never names the shape that means the pieces are already out:\n%s",
			checkpointSketchAsk)
	}
	if !parseCheckpointSketch("(waiting)").handsBack {
		t.Error("the token the ask teaches is not the token the harness reads")
	}
}

// A MARK THAT READS COORDINATION STARTS NOTHING, AND THE SAME MARK OVER REAL WORK
// STILL HANDS THE TURN OVER.
//
// Both arms in one test on purpose: the fix is worth nothing if it bought the
// refusal by turning the road off.
func TestACoordinationSketchStartsNothingAndRealPartsStillConvert(t *testing.T) {
	const asked = "keep an eye on the four pieces I have out"
	const answered = "all four are still running; nothing needs you yet"
	const coordination = "wait for the second | wait for the third | wait for the fourth\n" +
		"The second, third and fourth pieces are the ones still out."

	t.Run("coordination", func(t *testing.T) {
		rounds := checkpointMarkAt(2)
		steps := append(grindingSteps(rounds+2, coordination, "Finish it\nwhat is left"), finalAnswer(answered))
		completer := &scriptedCompleter{steps: steps}
		agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
		graph := stubbedGraph(agent, func(node *TaskNode) {})

		events, err := agent.Submit(context.Background(), asked)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		collected := collect(t, events)

		if count := admitted(graph); count != 0 {
			t.Fatalf("%d tasks were started off a drawing of the conversation's own waiting", count)
		}
		if said := noticeTexts(collected); saidSomething(said, checkpointSplitNote) {
			t.Fatalf("the split line was said over a turn that was only waiting: %q", said)
		}
		// AND THE READER WAS ASKED, so this is a refusal rather than a mechanism
		// that never fired.
		if read := marksRead(completer); read != 2 {
			t.Errorf("the sidecar was asked %d times over %d rounds, want both marks", read, rounds)
		}
		// AND THE TURN'S OWN ANSWER STANDS.
		if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
			t.Errorf("the turn ended as a %s saying %q, want the answer the model was giving",
				last.Role, messageText(last))
		}
	})

	t.Run("real parts", func(t *testing.T) {
		completer := &scriptedCompleter{steps: grindingSteps(checkpointMarkAt(1)+6, checkpointSplitSketch, "Finish the four pieces\nwhat is left")}
		agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
		ran := make(ranNodes, 2)
		graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

		events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		collected := collect(t, events)
		node := ran.await(t)

		if count := admitted(graph); count != 1 {
			t.Fatalf("%d tasks were admitted off three real parts, want exactly one", count)
		}
		if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
			t.Fatalf("the split never said its line; notices were %q", noticeTexts(collected))
		}
		if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS: A | B | C") {
			t.Errorf("the brief does not open on the parts the sidecar drew:\n%s", node.spec.brief)
		}
	})
}

// ── WAITING IS NOT WORKING, ON THE CONVERSATION'S SIDE ──────────────────────

// A ROUND SPENT WATCHING WORK ALREADY OUT PUSHES THE LADDER RATHER THAN CLIMBING
// IT.
//
// This is [childRun.park]'s law in the unit this meter counts in: a node's
// deadline is pushed by exactly the parked time, and a conversation's marks stand
// exactly as far ahead as they did before a round of looking.
func TestARoundSpentWatchingDoesNotClimbTheLadder(t *testing.T) {
	meter := &checkpointMeter{}
	// Twice the whole ladder, spent watching. Nothing fires.
	for round := 1; round <= checkpointMarkAt(checkpointMarks)*2; round++ {
		if mark := meter.round(false); mark != 0 {
			t.Fatalf("watching round %d fired mark %d", round, mark)
		}
	}
	if meter.rounds != 0 {
		t.Errorf("the meter counted %d rounds of work over a turn that only watched", meter.rounds)
	}
	if meter.watched != checkpointMarkAt(checkpointMarks)*2 {
		t.Errorf("the meter remembers %d watched rounds of %d", meter.watched, checkpointMarkAt(checkpointMarks)*2)
	}
	// AND THE FIRST MARK STILL STANDS WHERE IT ALWAYS DID, counted in work.
	for round := 1; round < checkpointMarkAt(1); round++ {
		if mark := meter.round(true); mark != 0 {
			t.Fatalf("round %d of work fired mark %d before the price", round, mark)
		}
	}
	if mark := meter.round(true); mark != 1 {
		t.Fatalf("the first mark gave %d at round %d of work", mark, checkpointMarkAt(1))
	}
}

// AND A BATCH IS READ BY WHAT IT TOUCHED.
func TestABatchOfLooksAtWorkAlreadyOutIsNotARoundOfWork(t *testing.T) {
	call := func(names ...string) []ai.ToolCall {
		var calls []ai.ToolCall
		for _, name := range names {
			calls = append(calls, ai.ToolCall{Function: ai.ToolCallFunction{Name: name}})
		}
		return calls
	}
	for _, one := range []struct {
		name     string
		calls    []ai.ToolCall
		watching bool
	}{
		{"the task rail", call("tasks"), true},
		{"a job's output", call("jobs"), true},
		{"both, in one breath", call("tasks", "jobs"), true},
		{"a look and a read", call("tasks", "read"), false},
		{"ordinary work", call("read", "write"), false},
		{"no batch at all", nil, false},
	} {
		if got := roundWasWatching(one.calls); got != one.watching {
			t.Errorf("%s reads as watching %v, want %v", one.name, got, one.watching)
		}
	}
}

// AND THE TWO WINDOWS IT NAMES ARE REAL TOOLS. A name that drifted out of the
// belt would turn this carve-out off in silence, which is the failure mode the
// whole file is built to avoid.
func TestTheWatchToolsAreOnTheBelt(t *testing.T) {
	agent := checkpointAgent(t, &scriptedCompleter{steps: []step{finalText("nothing")}})
	onBelt := make(map[string]bool)
	for _, name := range beltNames(agent) {
		onBelt[name] = true
	}
	for name := range checkpointWatchTools {
		if !onBelt[name] {
			t.Errorf("the checkpoint discounts rounds spent in %q, which is not a tool on the belt: %v",
				name, beltNames(agent))
		}
	}
}

// AND A WHOLE TURN SPENT WATCHING NEVER REACHES A MARK AT ALL.
//
// The incident's own shape, scripted end to end through [Agent.Submit]: a turn
// that does nothing but look at the pieces it already has out runs to its own end
// with the sidecar never asked, nothing said and nothing started.
func TestATurnSpentWatchingItsOwnWorkIsNeverCheckpointed(t *testing.T) {
	const answered = "all four are still running; I will tell you when they land"

	rounds := checkpointMarkAt(checkpointMarks) + 4
	steps := make([]step, rounds)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			if round == rounds-1 {
				return textResponse(answered), nil
			}
			arguments, _ := json.Marshal(struct {
				Query string `json:"query"`
			}{Query: fmt.Sprintf("piece %d", round)})
			return toolResponseWithText(fmt.Sprintf("look-%d", round), "tasks", string(arguments),
				"Checking where the pieces have got to."), nil
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "keep an eye on the four pieces I have out")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if read := marksRead(completer); read != 0 {
		t.Errorf("the sidecar was asked %d times about a turn that only watched", read)
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started off a turn that only watched", count)
	}
	if said := noticeTexts(collected); saidSomething(said, checkpointSplitNote) ||
		saidSomething(said, checkpointCeilingNote) {
		t.Fatalf("a watching turn was told something happened to it: %q", said)
	}
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), answered) {
		t.Errorf("the turn ended as a %s saying %q, want the answer the model was giving",
			last.Role, messageText(last))
	}
}

// ── #468: A READER THAT CANNOT WORK IS ABSENT, NOT FAILING ──────────────────

// A SESSION WITH NO SECOND MODEL ASKS NOBODY, AND SAYS SO ONCE.
//
// [roles.RoleMarkReader] answers to the crew alone, so a profile with no
// mastermind resolves to nothing at all. Every mark and every end-of-turn reading
// still made the call, failed in under two milliseconds and journaled itself as a
// mark that FAILED — a row that reads exactly like a mastermind that was there
// and could not be reached, written on every round of an evening. The decision
// carries on without it, on the work.
func TestAMarkReaderWithNoModelIsAbsentRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	// NO ROLES SOURCE AT ALL, which is the install this is about: no pin, no
	// tier, and a crew-only caller passes no floor of its own.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = dir
		c.SessionFile = transcript
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	workedTurn(agent, "port the parser and wire the handlers", 3)

	for round := 1; round <= 3; round++ {
		read := agent.readMark(context.Background())
		if read.asked {
			t.Fatalf("round %d: a session with no second model still paid for a reading", round)
		}
		agent.journalMarkRead(read, round, 10, checkpointDecisionContinue)
		if line := agent.readRemains(context.Background()); line.answered {
			t.Fatalf("round %d: a reader that is not there answered %+v", round, line)
		}
	}

	marks := journaledMarks(t, transcript)
	if len(marks) != 1 {
		t.Fatalf("the absence was written down %d times, want once: %+v", len(marks), marks)
	}
	if marks[0].Decision != checkpointDecisionNoReader {
		t.Fatalf("the journal calls a missing reader %q, want %q", marks[0].Decision, checkpointDecisionNoReader)
	}

	// AND THE DECISION STILL RUNS, ON WHAT LANDED. This is the half that was lost:
	// with no reader there was no line, and the road read that silence as a
	// finished ask.
	landOne(agent, TaskFailed, "wire the handlers", "incomplete — no route for PATCH")
	got := agent.decideRemains(context.Background(), agent.readRemains(context.Background()), "That completes the port.")
	if got.Verb != DecideCarryOn {
		t.Fatalf("work that did not finish ended the run: %+v", got)
	}
	if !strings.Contains(got.Brief, "wire the handlers did not finish") {
		t.Fatalf("the brief does not name the unfinished work:\n%s", got.Brief)
	}
}

// ── #468: THE NOTE SAYS WHAT WAS OBSERVED ───────────────────────────────────

// THE ONE LINE A PERSON READS AT THE CAP NAMES WHAT THE READING SHOWED.
//
// It used to assert "it is still not finished" as a FACT, on a road whose only
// observation was the same unmet set three times over. The claim was nobody's
// reading; the items are.
func TestTheCarriedOnNoteSaysWhatWasObserved(t *testing.T) {
	decision := budgetLeft(t).Decide(Remains{
		Acceptance: "every handler answers",
		Landed:     true,
		Landings:   []Landing{{ID: 1, Title: "wire the handlers", State: TaskFailed}},
		Checks:     []CheckRun{{Command: "go build ./...", Passed: false}},
		// A red that counts is a red the before-reading has landed for; until it
		// has, no check is counted against the run and the note says so instead.
		BaselineRead: true,
	})
	note := checkpointCarriedOnNote(decision.Observed)
	if !strings.Contains(note, "wire the handlers did not finish") ||
		!strings.Contains(note, "go build ./... does not pass") {
		t.Fatalf("the note does not name what was read:\n%s", note)
	}
	if strings.Contains(note, "it is still not finished") {
		t.Fatalf("the note still asserts a conclusion nobody read:\n%s", note)
	}
	// AND WITH NOTHING OBSERVED IT SAYS THAT, rather than inventing a reason on
	// the reading's behalf.
	if bare := checkpointCarriedOnNote(nil); !strings.Contains(bare, "nothing was read back") {
		t.Fatalf("a note with no observation behind it invented one:\n%s", bare)
	}
}
