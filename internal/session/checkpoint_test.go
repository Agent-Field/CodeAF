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
		got := meter.round()
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
		if mark := meter.round(); mark != 0 {
			t.Fatalf("round %d fired mark %d before the price", round, mark)
		}
	}
	meter.tighten(routeVerdict{Work: true, Wide: true, Acceptance: "the four pieces exist"})

	if mark := meter.round(); mark != 1 {
		t.Fatalf("the boundary the verdict landed on gave mark %d, want the first mark", mark)
	}
	// AND THE VERDICT IS KEPT, because whatever task eventually starts out of this
	// turn is the task those two readers were reading about.
	if !meter.raced.Wide || meter.raced.Acceptance != "the four pieces exist" {
		t.Errorf("the race's own reading was dropped: %+v", meter.raced)
	}
	// THE LATER RUNGS ARE THE ORDINARY ONES.
	for round := 4; round < checkpointMarkAt(2); round++ {
		if mark := meter.round(); mark != 0 {
			t.Fatalf("round %d fired mark %d; the second rung stands at %d",
				round, mark, checkpointMarkAt(2))
		}
	}
	if mark := meter.round(); mark != 2 {
		t.Fatalf("round %d gave mark %d, want the second mark at the ordinary rung",
			checkpointMarkAt(2), mark)
	}
	for round := checkpointMarkAt(2) + 1; round < checkpointMarkAt(checkpointMarks); round++ {
		if mark := meter.round(); mark != 0 {
			t.Fatalf("round %d fired mark %d; the ceiling stands at %d",
				round, mark, checkpointMarkAt(checkpointMarks))
		}
	}
	if mark := meter.round(); mark != checkpointMarks {
		t.Fatalf("the ceiling gave mark %d at round %d, want %d",
			mark, checkpointMarkAt(checkpointMarks), checkpointMarks)
	}
}

// AND A YES THAT LANDS AFTER THE FIRST MARK HAS NOTHING LEFT TO TIGHTEN — but its
// verdict is still about this work, so it is still kept.
func TestALateTightenKeepsTheVerdictAndMovesNothing(t *testing.T) {
	meter := &checkpointMeter{}
	for round := 1; round <= checkpointMarkAt(1); round++ {
		meter.round()
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
	const want = "[checkpoint] In one line, sketch what remains as parts and arrows: " +
		"independent parts separated by ' | ', ordered steps joined by ' > '. " +
		"Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. " +
		"Nothing else on that line. Then one sentence saying what each letter is."
	if checkpointSketchAsk != want {
		t.Fatalf("the mark's ask reads:\n%s\nwant:\n%s", checkpointSketchAsk, want)
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
// The rules, stated once here and once in [topLevelParts]: a top-level ' | ' is
// what makes a part, a chain is one job, and a fork BEHIND a step is one job too —
// because at this mark the work in front of the turn is that first step. Reading
// nested pipes as width was the loose scoring; the strict reading took the
// mastermind's trap accuracy from 58% to 100% on the same answers.
func TestTheShapeIsReadAsPartsAtTheTopLevelOnly(t *testing.T) {
	for _, c := range []struct {
		shape string
		parts int
		split bool
		why   string
	}{
		{"A | B | C", 3, true, "three parts with nothing in front of them"},
		{"A > B > C", 1, false, "a chain: the second step cannot begin until the first is done"},
		{"A > (B | C)", 1, false, "the fork is behind a step that has not happened yet"},
		{"(A | B) > C", 1, false, "one bracketed group, so nothing stands at the top level"},
		{"(A | B | C | D) > E", 1, false, "the same, however many parts are inside the bracket"},
		{"Validation | (Arithmetic & Currency) > Shared updates", 2, true, "a real top-level split"},
		{"A > B > E | C > D > F", 2, true, "two chains that do not wait on each other"},
		{"(done)", 1, false, "one atom and no separator at all"},
		{"I will keep going with the parser rewrite.", 1, false, "prose is one part"},
		{"", 0, false, "nothing was drawn"},
		{"|", 0, false, "a separator with nothing beside it is not a part"},
		{"| A", 1, false, "the empty side does not count"},
	} {
		got := topLevelParts(c.shape)
		if got != c.parts {
			t.Errorf("%q read as %d parts, want %d — %s", c.shape, got, c.parts, c.why)
		}
		if split := (checkpointSketch{parts: got}).split(); split != c.split {
			t.Errorf("%q decided split=%v, want %v — %s", c.shape, split, c.split, c.why)
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
)

// checkpointSlack is how many of a script's steps a grinding turn spends on
// things that are not tool rounds: one read per mark, and the dowry ask at the
// end. A script cut to the round count alone runs out under the sidecar, and the
// scripted completer's past-the-end answer would then be read as a sketch and as
// a brief.
const checkpointSlack = checkpointMarks + 2

// checkpointAgent is a watched conversation with a mastermind the mark can be
// read by. Everything else is [newTestAgent]'s.
func checkpointAgent(t *testing.T, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
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
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			return toolResponse(fmt.Sprintf("call-%d", round), "ls", string(arguments)), nil
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

// askedForHandoff reports whether this request ends on the dowry ask.
func askedForHandoff(messages []ai.Message) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), "[handing over]")
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
	steps := append(grindingSteps(rounds+1, checkpointChainSketch, ""), finalText("done"))
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
	// ceiling stands three times further along.
	if completer.requests() > checkpointMarkAt(1)+3 {
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
	steps := append(grindingSteps(rounds+2, checkpointChainSketch, "Finish it\nwhat is left"), finalText(answered))
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

	ctx, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer done()
	started := time.Now()
	sketch := agent.readMark(ctx)

	if sketch.drawn() || sketch.split() {
		t.Fatalf("a read that never answered produced %+v", sketch)
	}
	if waited := time.Since(started); waited > checkpointSketchWindow {
		t.Errorf("the harness waited %s on a reader that never answered", waited)
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
	// AND THE LINE THE PERSON READS IS ABOUT THEIR WORK. This is the told-after
	// line as it is drawn, before the namer has had its second at it.
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
}

// AND A CONTINUATION SAYING NOTHING IS LEFT DROPS THE CEILING'S HANDOVER.
//
// The ceiling reads a counter, and a counter cannot see that the work finished
// thirty seconds ago. The one reader that can is the model holding the findings,
// which is the model this ask is put to — so it is asked, and an answer of
// [checkpointNothingLeft] ends the matter: no task, no line, no gap spent, and
// the turn carries on to the answer it was about to give.
func TestAContinuationSayingNothingIsLeftDropsTheCeilingHandover(t *testing.T) {
	const answered = "all eight files are written and the smoke check passed"

	rounds := checkpointMarkAt(checkpointMarks)
	steps := append(grindingSteps(rounds+checkpointSlack, checkpointChainSketch, checkpointNothingLeft), finalText(answered))
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

// A NODE, A SCREENLESS SESSION AND THE SESSION'S OWN VOICE ARE ALL LEFT ALONE.
//
// Each for its own reason, and each of them is a turn that would be made worse by
// a ceiling: a node already runs under a step cap, a deadline and a checker; a
// session with nobody watching has no one to read the line; and a turn the
// session started for itself is the session spending money on its own sentence.
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
		{"a session nobody is watching", func(config *Config) { config.AskConsent = false }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			steps := append(grindingSteps(rounds, checkpointSplitSketch, ""), finalText("done"))
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
