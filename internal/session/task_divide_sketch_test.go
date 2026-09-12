package session

// THE SKETCH AS THE DIVISION, AS TESTS.
//
// A mark's second reader draws what is left of a turn, the turn is handed over,
// and the drawing rides the spec into the task that takes the work
// (task_divide_sketch.go). What is under test is that the harness then PUTS that
// drawing to the division road on the worker's behalf — through the same verb, the
// same gates and the same reviewer — instead of hoping a cheap worker will find
// the parts again.
//
// Everything here drives the real doors, for task_divide_test.go's reason: the
// whole of this road is that there is no second way to spawn work.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// batchSketch is the drawing a mastermind makes of a request carrying several
// whole jobs. THE ACCOUNT IT WAS DRAWN FROM ENUMERATES NOTHING A COUNTER CAN SEE,
// which is the ordinary case and the whole reason the tiebreak exists: four whole
// asks are four ownable jobs and count as zero items. A division built on this
// reaches the reviewer to be decided.
func batchSketch(shape, legend string) drawnDivision {
	return drawnDivision{
		sketch: checkpointSketch{shape: shape, legend: legend, parts: topLevelParts(shape)},
		digest: "WHAT WAS ASKED\n" + personSentence + "\n\nWHAT HAS BEEN DONE SO FAR, ONE LINE PER STEP\nread cmd/main.go\ngrep flake",
	}
}

// countedSketch is the same drawing over an account that DOES enumerate enough
// items to clear the floor for free.
//
// It is what most of the tests below want, and the reason is the reviewer's two
// postures. Below the floor the review is the only reader that has said yes, so an
// unreachable one leaves the floor's refusal standing — which makes every test
// about what the HARNESS wrote depend on a scripted mastermind repeating it back.
// Over the floor the review fails open, so an absent one admits the harness's own
// parts unchanged, and what reaches the graph is exactly what this file built.
func countedSketch(shape, legend string) drawnDivision {
	drawn := batchSketch(shape, legend)
	drawn.digest += "\n\nWHAT HAS BEEN WRITTEN OR CHANGED\n" + wideEvidence
	return drawn
}

// drawnSpec is a task handed over on a mark's split: armed by a model's own
// reading of breadth, and carrying the drawing that reading produced.
func drawnSpec(drawn drawnDivision) taskSpec {
	spec := judgedWide
	spec.drawn = drawn
	return spec
}

// sketchReviewer answers the division review with n parts of its own, sharpened,
// so a test can tell the reviewer's briefs from the harness's.
func sketchReviewer(titles ...string) *divideReviewer {
	parts := make([]string, 0, len(titles))
	for _, title := range titles {
		parts = append(parts, fmt.Sprintf(
			`{"title":%q,"summary":"s","brief":"SHARPENED %s","acceptance":"%s is checked by running it"}`,
			title, title, title))
	}
	return &divideReviewer{answer: `{"parts":[` + strings.Join(parts, ",") + `]}`}
}

// journaledDivisions reads the division lines back out of one worker's journal,
// which is the file the bench reads (sessionfile.go's [journalDivision]).
func journaledDivisions(t *testing.T, path string) []journalDivision {
	t.Helper()
	var divisions []journalDivision
	for _, entry := range journaledEntries(t, path, "division") {
		if entry.Division != nil {
			divisions = append(divisions, *entry.Division)
		}
	}
	return divisions
}

// ── the drawing becomes a division ──────────────────────────────────────────

// THE WHOLE POINT OF THE WAVE. Three converted cells landed parts=0 with a
// division already written at the head of their brief; this is that division
// existing as nodes instead.
func TestASketchWithPartsIsHandedOutWithoutTheWorkerAsking(t *testing.T) {
	reviewer := sketchReviewer("the auth test", "the http client", "the release notes")
	nest := newDivideNestFrom(t, drawnSpec(batchSketch("A | B | C",
		"A is the flaking auth test, B is the http client major version, C is the release notes for 2.4")),
		0, reviewer, nil)

	said, _, _ := weighBeside(t, nest.node)

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the drawing bore %d parts, want the 3 the reader drew", len(kids))
	}
	// THE PARENT STAYS AND COORDINATES, which is the law every part on this road
	// is admitted under: three parts under one node that is still open.
	if nest.parent.stateNow().settled() {
		t.Fatalf("the work that was divided is already %s: the parent stays to fold the reports", nest.parent.stateNow())
	}
	for _, kid := range kids {
		if kid.parent != nest.parent.id {
			t.Fatalf("part %d hangs off %d, want the work it came out of", kid.id, kid.parent)
		}
	}
	// AND THE WORKER IS TOLD, IN THE WORDS IT WOULD HAVE READ HAD IT ASKED ITSELF.
	if !strings.Contains(said, "split into 3 parts:") {
		t.Fatalf("the worker is told %q, want the division's own receipt", said)
	}
	if !strings.Contains(said, "do not wait for them") {
		t.Fatalf("the worker is told %q, want it told not to wait on the parts", said)
	}
	assertPlainWords(t, "what the worker is told about its parts", said)
}

// AND IT GOES THROUGH THE REVIEWER, WHOSE BRIEFS ARE WHAT THE PARTS GET. The
// harness cut these parts out of one line of letters; a mastermind reading them
// together is the only reader that can say whether they overlap, and it is the
// same call a worker's own division is put to.
func TestTheHarnessSubmittedDivisionIsReadByTheSameReviewer(t *testing.T) {
	floorPinnedOn(t)
	reviewer := sketchReviewer("the auth test", "the release notes")
	nest := newDivideNestFrom(t, drawnSpec(batchSketch("A | B",
		"A is the flaking auth test, B is the release notes for 2.4")),
		0, reviewer, nil)

	weighBeside(t, nest.node)

	if reviewer.reads() != 1 {
		t.Fatalf("the drawing was read by the reviewer %d times, want once", reviewer.reads())
	}
	// A BELOW-FLOOR DIVISION ON JUDGE-ARMED WORK REACHES IT THROUGH THE TIEBREAK
	// AND NOT FOR FREE: two whole jobs enumerate nothing a counter can see, so the
	// reviewer is told it is deciding rather than sharpening (task_divide.go).
	if !strings.Contains(reviewer.saw(), "THIS ONE IS YOURS TO DECIDE") {
		t.Fatalf("the reviewer was asked to sharpen a division it was deciding: %q", reviewer.saw())
	}
	// AND IT IS SHOWN WHAT THE MARK'S READER WAS SHOWN, which is the only honest
	// evidence there is for a division nobody worked for: a drawing is not evidence.
	if !strings.Contains(reviewer.saw(), "A | B") || !strings.Contains(reviewer.saw(), "WHAT HAS BEEN DONE SO FAR") {
		t.Fatalf("the reviewer saw %q, want the drawing and the account it was drawn from", reviewer.saw())
	}
	for _, kid := range nest.graph.children(nest.parent.id) {
		if !strings.Contains(kid.instruction(), "SHARPENED") {
			t.Fatalf("part %d works from %q, want the reviewer's own brief", kid.id, kid.instruction())
		}
	}
}

// EVERY PART HAS SOMETHING TO BE FINISHED AGAINST, whoever wrote the parts. A
// part is judged by a checker against its acceptance ALONE, so one admitted with
// none would be judged against nothing — and the review FAILS OPEN, so the
// stand-in has to hold when no mastermind can be reached at all.
func TestEveryPartOfADrawnDivisionCarriesADoneConditionAndItsSiblingsScopes(t *testing.T) {
	// AN ABSENT REVIEWER OVER THE FLOOR ADMITS THE HARNESS'S OWN PARTS, which is
	// what makes this a test about what this file wrote rather than about what a
	// scripted mastermind repeated back ([countedSketch]).
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B | C",
		"A is the flaking auth test, B is the http client major version, C is the release notes for 2.4")),
		0, &scriptedCompleter{}, nil)

	weighBeside(t, nest.node)

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the drawing bore %d parts, want 3", len(kids))
	}
	for _, kid := range kids {
		if strings.TrimSpace(kid.acceptance()) == "" {
			t.Fatalf("part %d has nothing to be finished against", kid.id)
		}
		// AND EACH KNOWS WHAT IT DOES NOT OWN. Nobody wrote these briefs, so the
		// boundary is stated in both directions or every part does all three jobs.
		brief := kid.instruction()
		if !strings.Contains(brief, divisionThisPart) || !strings.Contains(brief, divisionOtherParts) {
			t.Fatalf("part %d does not know which letter it is or that it has siblings: %q", kid.id, brief)
		}
		named := 0
		for _, other := range []string{"auth test", "http client", "release notes"} {
			if strings.Contains(brief, other) {
				named++
			}
		}
		if named != 3 {
			t.Fatalf("part %d names %d of the 3 scopes, want its own and both siblings': %q", kid.id, named, brief)
		}
	}
	// AND THE PERSON'S OWN WORDS STILL RIDE EVERY ONE OF THEM, as they do on every
	// other door into the graph.
	for _, kid := range kids {
		if got := kid.request(); got != personSentence {
			t.Fatalf("part %d opens on %q, want the person's own sentence", kid.id, got)
		}
	}
}

// AND A REFUSAL LEAVES EXACTLY WHAT THERE WAS BEFORE: one worker, one task,
// nothing cancelled — which is what every converted cell does today.
func TestAReviewerThatRefusesTheDrawingLeavesOneWorker(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "why": "these are stages of one job"}`}
	nest := newDivideNestFrom(t, drawnSpec(batchSketch("A | B",
		"A is the flaking auth test, B is the release notes for 2.4")),
		0, reviewer, nil)

	if said, _, _ := weighBeside(t, nest.node); said != "" {
		t.Fatalf("the worker was told %q about a division nobody admitted", said)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born from a refused division", len(kids))
	}
	// AND THE REFUSAL IS WRITTEN DOWN, WITH THE READER THAT MADE IT. Before this
	// line a task that ran alone could have never asked, been refused for free, or
	// been refused by a mastermind, and the file said the same nothing about all
	// three.
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 {
		t.Fatalf("the journal holds %d division lines, want one", len(divisions))
	}
	if divisions[0].Decision != divisionRefusedReview {
		t.Fatalf("the refusal was written down as %q, want the reviewer named", divisions[0].Decision)
	}
	if divisions[0].Source != divisionBySketch || divisions[0].Requested != 2 || divisions[0].Admitted != 0 {
		t.Fatalf("the line reads %+v, want the drawing's own two parts and none admitted", divisions[0])
	}
}

// AND AN ADMITTED ONE IS WRITTEN DOWN TOO, so never-asked, refused and admitted
// are three answers in the file rather than one absence.
func TestAnAdmittedDivisionSaysWhoAskedAndHowManyPartsExist(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B | C",
		"A is the auth test, B is the http client, C is the release notes")),
		0, &scriptedCompleter{}, nil)

	weighBeside(t, nest.node)

	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 {
		t.Fatalf("the journal holds %d division lines, want one", len(divisions))
	}
	if got := divisions[0]; got.Source != divisionBySketch || got.Decision != divisionAdmitted ||
		got.Requested != 3 || got.Admitted != 3 || got.TaskID != nest.parent.id {
		t.Fatalf("the line reads %+v, want a sketch's three parts admitted under task %d", got, nest.parent.id)
	}
}

// AND THE WORKER'S OWN VERB WRITES THE SAME LINE UNDER ITS OWN NAME. One record
// for one road, whoever asked.
func TestAWorkersOwnDivisionIsWrittenDownAsTheWorkers(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))

	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 {
		t.Fatalf("the journal holds %d division lines, want one", len(divisions))
	}
	if got := divisions[0]; got.Source != divisionByWorker || got.Decision != divisionAdmitted || got.Admitted != 2 {
		t.Fatalf("the line reads %+v, want the worker's own admitted division", got)
	}
}

// A DRAWING WITH ONE JOB IN IT SUBMITS NOTHING, and costs nothing to not submit.
// The ceiling reaches a handover with such a shape routinely — it fires whatever
// the last reading said.
func TestASketchOfOneJobHandsNothingOut(t *testing.T) {
	for _, shape := range []string{"A > B > C", "A > (B | C)", "finish the parser rewrite"} {
		t.Run(shape, func(t *testing.T) {
			reviewer := sketchReviewer("one", "two")
			nest := newDivideNestFrom(t, drawnSpec(batchSketch(shape, "A is the parser, B is the tests, C is the docs")),
				0, reviewer, nil)

			if said, _, _ := weighBeside(t, nest.node); said != "" {
				t.Fatalf("a chain was handed out: %q", said)
			}
			if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
				t.Fatalf("%d parts were born from a shape with one job in it", len(kids))
			}
			if reviewer.reads() != 0 {
				t.Fatalf("a shape with nothing to divide was read %d times: it must cost nothing", reviewer.reads())
			}
			if divisions := journaledDivisions(t, nest.journal); len(divisions) != 0 {
				t.Fatalf("a division nobody put was journaled %+v", divisions)
			}
		})
	}
}

// AND WORK THAT WAS NEVER ARMED IS NEVER DIVIDED FOR, whatever it is carrying. A
// worker that would not have been given the verb must not have a division
// submitted on its behalf either — that is the whole meaning of arming.
func TestAnUnarmedTaskIsNeverDividedForByTheHarness(t *testing.T) {
	spec := taskSpec{title: "one small thing", request: personSentence,
		brief: "rename the flag in one file", acceptance: "a", depth: 1,
		drawn: batchSketch("A | B", "A is the rename, B is the docs line")}
	nest := newDivideNestFrom(t, spec, 0, sketchReviewer("one", "two"), nil)
	if nest.parent.armedBy() != "" {
		t.Fatalf("this work was armed by %q, and the test needs work nobody armed", nest.parent.armedBy())
	}

	if said, _, _ := weighBeside(t, nest.node); said != "" {
		t.Fatalf("unarmed work was divided for: %q", said)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born under work that was never allowed any", len(kids))
	}
}

// AND IT IS PUT ONCE. A node whose parts already exist has already been divided,
// and a second worker built after a provider fault must not hand the same work
// out again.
func TestTheDrawingIsPutOnceAndNeverTwice(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B",
		"A is the auth test, B is the release notes")),
		0, &scriptedCompleter{}, nil)

	if said, _, _ := weighBeside(t, nest.node); said == "" {
		t.Fatal("the first ask handed nothing out")
	}
	if said, _, _ := weighBeside(t, nest.node); said != "" {
		t.Fatalf("the drawing was put a second time: %q", said)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 2 {
		t.Fatalf("%d parts exist after two asks, want the 2 that were drawn", len(kids))
	}
}

// THE PARTS-THEN-GATHER SHAPE HANDS OUT THE PARTS AND KEEPS THE GATHER. It is
// the shape a reader actually draws for a batch of jobs, and the step behind the
// bracket is the parent's own — the parent-stays law, said in the drawing's own
// letters.
func TestABracketedFirstStageHandsOutItsPartsAndKeepsTheStepBehindThem(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("(A | B | C) > D",
		"A is the auth test, B is the http client, C is the release notes, D is running the whole suite once")),
		0, &scriptedCompleter{}, nil)

	said, _, _ := weighBeside(t, nest.node)

	if kids := nest.graph.children(nest.parent.id); len(kids) != 3 {
		t.Fatalf("the bracketed stage bore %d parts, want its 3", len(kids))
	}
	if !strings.Contains(said, "ONCE THEIR REPORTS ARE IN") || !strings.Contains(said, "running the whole suite once") {
		t.Fatalf("the worker is told %q, want the step the parts were cut out from in front of", said)
	}
	assertPlainWords(t, "what the worker is told about the step it keeps", said)
}

// AND A DIVISION NOBODY IS FREE TO PICK UP IS STILL REFUSED, unchanged. The
// capacity gate is the road's, and the harness submitting the parts does not buy
// them a lane.
func TestADrawnDivisionNobodyCanPickUpIsRefusedLikeAnyOther(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B",
		"A is the auth test, B is the release notes")),
		1, &scriptedCompleter{}, nil)

	if said, _, _ := weighBeside(t, nest.node); said != "" {
		t.Fatalf("a session that runs one task at a time handed parts out: %q", said)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were admitted with no lane to run them in", len(kids))
	}
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 || divisions[0].Decision != divisionRefusedLane {
		t.Fatalf("the journal reads %+v, want the lane named as what refused", divisions)
	}
}

// ── reading the drawing ─────────────────────────────────────────────────────

// THE LEGEND'S OWN WORDS ARE WHAT A PART IS CALLED, whichever way the reader
// wrote the shape, and a bare coordinate is never a name: `A` on the rail tells a
// person nothing at all.
func TestAPartIsNamedFromTheLegendAndNeverFromABareLetter(t *testing.T) {
	for _, test := range []struct {
		name   string
		drawn  drawnDivision
		titles []string
	}{
		{"letters with a legend", batchSketch("A | B",
			"A is the flaking auth test, B is the release notes"),
			[]string{"the flaking auth", "the release notes"}},
		{"a legend written a line apiece", batchSketch("A | B",
			"A: the flaking auth test\nB: the release notes"),
			[]string{"the flaking auth", "the release notes"}},
		{"words in the shape itself", batchSketch("fix the auth test | write the release notes", ""),
			[]string{"fix the auth", "write the release"}},
		{"letters nobody explained", batchSketch("A | B", ""),
			[]string{"part 1", "part 2"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			proposal, ok := test.drawn.proposal()
			if !ok {
				t.Fatal("the drawing proposed nothing")
			}
			for index, part := range proposal.Parts {
				if part.Title != test.titles[index] {
					t.Errorf("part %d is called %q, want %q", index+1, part.Title, test.titles[index])
				}
				if !part.whole() {
					t.Errorf("part %d is missing a field: %+v", index+1, part)
				}
			}
		})
	}
}

// AND THE EVIDENCE IS WHAT THE READER WAS SHOWN, not what the harness would like
// to have been able to say. A drawing is not evidence; the account a mastermind
// judged is.
func TestTheEvidencePutToTheGatesIsTheAccountTheReaderJudged(t *testing.T) {
	drawn := batchSketch("A | B", "A is the auth test, B is the release notes")
	proposal, ok := drawn.proposal()
	if !ok {
		t.Fatal("the drawing proposed nothing")
	}
	for _, want := range []string{"A | B", "A is the auth test", "WHAT WAS ASKED", personSentence} {
		if !strings.Contains(proposal.Evidence, want) {
			t.Fatalf("the evidence is %q, want %q in it", proposal.Evidence, want)
		}
	}
}

// ── the seam: beside the worker's first request ─────────────────────────────

// weighBeside runs the drawing a worker's node was admitted with through the
// reading beside that worker, and waits for the reading to finish ON ITS OWN —
// never cancelled, which is what [sizingBeside.end] does — so a test reads what
// the worker would have been handed: the receipt on its queue, the person's job
// it was stopped for, and whether it was stopped at all. The worker must be
// sitting in its node's room, which is what every fixture here arranges.
func weighBeside(t *testing.T, worker *Agent) (receipt, person string, stopped bool) {
	t.Helper()
	node := worker.graph().node(worker.config.taskID)
	sizing := worker.sizeBeside(context.Background(), node.openRoom(), func() { stopped = true })
	if sizing == nil {
		return "", "", false
	}
	<-sizing.reading.done
	return sizing.receipt, sizing.person, stopped
}

// queuedFor is everything waiting on a worker's queue for its next request.
func queuedFor(worker *Agent) string {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	var queued strings.Builder
	for _, note := range worker.steering {
		queued.WriteString(note.text())
		queued.WriteString("\n")
	}
	return queued.String()
}

// THE WORKER'S FIRST REQUEST GOES OUT BEFORE THE DRAWING HAS BEEN WEIGHED, and the
// parts reach it while it works. That is the whole timing requirement of this
// road now, and it is the opposite of what it used to be: the reading stood in
// front of the first request, and on the measured node it stood there for two
// hundred and nineteen seconds.
//
// THE ORDER IS MADE BY THE TEST, NOT OBSERVED BY IT. The reviewer here will not
// answer until the worker has been asked something, so a harness that still held
// the first request behind the reading would never get either — and the bound on
// the wait below is what fails it, rather than a timestamp that could be read
// either way.
//
// It is asserted from the OUTSIDE — a real node run by the real runner — because
// what a worker's requests carry is the fact, and there is no honest way to test
// an ordering from inside the function that owns it.
func TestTheWorkersFirstRequestGoesOutBeforeTheDrawingIsWeighed(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newBesideCompleter(sketchReviewer("the auth test", "the http client", "the release notes").answer)
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	// ONLY THE PARENT RUNS FOR REAL. Its parts are counted as they are started and
	// then left alone, so that the worker is still parked on them when the test
	// reports them in by hand below.
	var startedMu sync.Mutex
	started := 0
	partsOut := make(chan struct{})
	graph.run = func(node *TaskNode) {
		if node.parent == 0 {
			graph.runOwned(node)
			return
		}
		startedMu.Lock()
		defer startedMu.Unlock()
		if started++; started == 3 {
			close(partsOut)
		}
	}
	// THE WORKER'S FIRST ANSWER WAITS FOR THE PARTS, so the receipt is on its
	// queue by the time its turn ends — the ordinary case of a reading that lands
	// while the worker is still at work.
	completer.hold = partsOut

	id := graph.reserve()
	spec := drawnSpec(countedSketch("A | B | C",
		"A is the flaking auth test, B is the http client major version, C is the release notes for 2.4"))
	spec.model = "test/model"
	graph.admit(id, spec)

	first := completer.request(t, 0)
	if strings.Contains(first, divisionHandedOutBeside) {
		t.Fatalf("the worker's FIRST request already knew its parts, so it waited for the reading: %q", first)
	}
	if !strings.Contains(first, briefAskHeading) {
		t.Fatalf("the worker's first request lost the person's own words: %q", first)
	}
	select {
	case <-partsOut:
	case <-time.After(30 * time.Second):
		t.Fatal("the drawing's parts were never handed out")
	}
	if at, verdict := completer.firstAskedAt(), completer.verdictAt(); at == 0 || verdict == 0 || at > verdict {
		t.Fatalf("the worker was asked at step %d and the reading answered at step %d: the first request must go first", at, verdict)
	}

	// THE PARENT STAYS AND COORDINATES: parked on its parts, open, and not asked
	// anything more until they report.
	parent := graph.node(id)
	kids := graph.children(id)
	if len(kids) != 3 {
		t.Fatalf("the node has %d parts under it, want the 3 its drawing named", len(kids))
	}
	for _, kid := range kids {
		kid.finish(kid.title()+" is done", nil, "", "")
		graph.complete(kid, TaskDone)
	}
	// AND THE REQUEST THEIR REPORTS START CARRIES THE RECEIPT IT WAS QUEUED, in
	// the words a worker that asked for the division would have read.
	second := completer.request(t, 1)
	if !strings.Contains(second, divisionHandedOutBeside) || !strings.Contains(second, "split into 3 parts:") {
		t.Fatalf("the worker never read the receipt for its parts: %q", second)
	}
	select {
	case <-parent.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the divided work never landed after its parts reported")
	}
}

// AND WORK ONLY A PERSON CAN DO STOPS THE WORKER THAT IS ALREADY AT IT. The
// reading comes back while the worker's first request is still out; the request
// is cut, and the node lands on the person with the reader's own sentence rather
// than running on to the check. The worker here never answers on its own — only
// the stop can end its request — so a harness that let it carry on would never
// land the node at all.
func TestWorkNoWorkerCanDoStopsARunningWorkerAndLandsOnThePerson(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newBesideCompleter(`{"refuse": true, "nobody": true, "why": ` + strconv.Quote(measuredHumanOnlyFinding) + `}`)
	completer.hold = make(chan struct{})
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	id := graph.reserve()
	spec := drawnSpec(countedSketch("(A | B) > C",
		"A is approving PR #1018, B is approving PR #1019, C is the merge queue merging both"))
	spec.model = "test/model"
	graph.admit(id, spec)

	node := graph.node(id)
	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the worker was never stopped for work only a person can do")
	}
	if first := completer.request(t, 0); first == "" {
		t.Fatal("the worker was never started, so this proves nothing about stopping one")
	}
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("the node landed %s, want it waiting on the person", state)
	}
	report, _, _, _ := node.leavings()
	if !strings.Contains(report, "an approving review GitHub will only accept from a human") {
		t.Fatalf("the person was handed %q, and not what the reader found", report)
	}
	if kids := graph.children(id); len(kids) != 0 {
		t.Fatalf("%d parts were admitted for work no worker can do", len(kids))
	}
}

// besideCompleter is a worker and a reviewer sharing one completer, with the one
// ordering this road is about made explicit: the reviewer does not answer until
// the worker has been asked something.
type besideCompleter struct {
	review string
	// judge, when set, is the sizing judge's answer, given once the worker has
	// been asked something — the same ordering the reviewer keeps.
	judge string
	// hold, when set, is what the worker's first answer waits for.
	hold <-chan struct{}

	mu    sync.Mutex
	step  int
	asked []string
	// offered is the tool names each of those requests carried, index for index
	// with asked. It is the WIRE's own answer to "what could the worker reach
	// for at that moment", which is the only honest way to ask whether a belt
	// gained a verb: a belt rebuilt from the config in a test would answer about
	// the config and not about the request that went out.
	offered  [][]string
	firstAt  int
	answerAt int
	judgedAt int
	firstIn  chan struct{}
	arrived  chan struct{}
	// judged and reviewed are closed as those two readers answer, and they are
	// what a test hands back as [besideCompleter.hold]: A READING IS CANCELLED
	// WHEN ITS WORKER FINISHES (task_beside.go's law), so a worker whose only
	// answer is "Done." can outrun the whole road and leave a test asserting
	// about a reading that was never made.
	judged   chan struct{}
	reviewed chan struct{}
}

func newBesideCompleter(review string) *besideCompleter {
	return &besideCompleter{review: review, firstIn: make(chan struct{}), arrived: make(chan struct{}, 16),
		judged: make(chan struct{}), reviewed: make(chan struct{})}
}

func (c *besideCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// The options are applied to a throwaway request so a test can assert which
	// tools each step actually rode.
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	switch system {
	case titleSystem, taskNameSystem:
		return textResponse("a name"), nil
	case shapePrompt:
		// A person's `/task` has its brief written beside the same worker
		// (task_shape.go); this road is about the division, so the shaper is one
		// that cannot answer and leaves the node on the person's words.
		return nil, errors.New("no shaper was scripted")
	case taskJudgePrompt:
		if c.judge == "" {
			return nil, errors.New("no judge was scripted")
		}
		select {
		case <-c.firstIn:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		c.step++
		c.judgedAt = c.step
		c.mu.Unlock()
		close(c.judged)
		return textResponse(c.judge), nil
	case divideReviewBrief:
		select {
		case <-c.firstIn:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		c.step++
		c.answerAt = c.step
		c.mu.Unlock()
		close(c.reviewed)
		return textResponse(c.review), nil
	}
	var asked strings.Builder
	for _, message := range messages {
		if message.Role == "user" {
			asked.WriteString(messageText(message))
			asked.WriteString("\n")
		}
	}
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Function.Name)
	}
	c.mu.Lock()
	c.step++
	c.asked = append(c.asked, asked.String())
	c.offered = append(c.offered, names)
	firstAsk := len(c.asked) == 1
	if firstAsk {
		c.firstAt = c.step
	}
	c.mu.Unlock()
	c.arrived <- struct{}{}
	if firstAsk {
		close(c.firstIn)
		if c.hold != nil {
			select {
			case <-c.hold:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return textResponse("Done."), nil
}

// request waits for the worker's nth request and answers what it carried.
func (c *besideCompleter) request(t *testing.T, n int) string {
	t.Helper()
	for {
		c.mu.Lock()
		if len(c.asked) > n {
			asked := c.asked[n]
			c.mu.Unlock()
			return asked
		}
		c.mu.Unlock()
		select {
		case <-c.arrived:
		case <-time.After(30 * time.Second):
			t.Fatalf("the worker was never asked a request %d", n+1)
		}
	}
}

// offeredAt is the belt the worker's nth request carried, as the wire saw it.
// It is read after [besideCompleter.request] has waited for that request.
func (c *besideCompleter) offeredAt(t *testing.T, n int) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.offered) <= n {
		t.Fatalf("the worker was never asked a request %d", n+1)
	}
	return c.offered[n]
}

func (c *besideCompleter) firstAskedAt() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstAt
}

func (c *besideCompleter) verdictAt() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answerAt
}

// A PERSON'S WIDE `/task` STARTS ITS WORKER FIRST, AND THE SIZING JUDGE'S PARTS
// REACH IT AS THE DIVISION RECEIPT — the control of issue #936. The judge used to
// be asked in front of the task, and its yes only armed the worker with a verb;
// it is asked beside the worker now, and its parts go through the same gates, the
// same reviewer and the same admission a drawing does.
//
// THE ORDER IS MADE BY THE TEST: the judge will not answer until the worker has
// been asked something, so a door that still asked it first would never start
// the worker, and the bound on the wait is what fails it.
func TestAWideTaskStartsItsWorkerFirstAndTheJudgesPartsArriveAsTheReceipt(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newBesideCompleter(sketchReviewer("the auth test", "the http client", "the release notes").answer)
	completer.judge = `{"parallelizable":true,"parts":["the auth test","the http client","the release notes"],"why":"three independent jobs"}`
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	var startedMu sync.Mutex
	started := 0
	partsOut := make(chan struct{})
	graph.run = func(node *TaskNode) {
		if node.parent == 0 {
			graph.runOwned(node)
			return
		}
		startedMu.Lock()
		defer startedMu.Unlock()
		if started++; started == 3 {
			close(partsOut)
		}
	}
	completer.hold = partsOut

	ask := "bring the flaking auth test, the http client upgrade and the release notes up to date"
	id, _, _, err := session.StartTask(t.Context(), ask, false)
	if err != nil {
		t.Fatal(err)
	}
	first := completer.request(t, 0)
	if strings.Contains(first, divisionHandedOutBeside) {
		t.Fatalf("the worker's FIRST request already knew its parts, so it waited for the judge: %q", first)
	}
	select {
	case <-partsOut:
	case <-time.After(30 * time.Second):
		t.Fatal("the judge's parts were never handed out")
	}
	completer.mu.Lock()
	firstAt, judgedAt := completer.firstAt, completer.judgedAt
	completer.mu.Unlock()
	if firstAt == 0 || judgedAt == 0 || firstAt > judgedAt {
		t.Fatalf("the worker was asked at step %d and the judge answered at step %d: the worker must go first", firstAt, judgedAt)
	}
	// AND THE WORD IS ON THE WORK, which is the other half of what the judge's
	// yes does now: the parts go to the road, and the node is armed so that the
	// worker still running can hand out more of it later (#958). It used to be
	// asserted EMPTY here, and that assertion was the defect written down.
	if armed := graph.node(id).armedBy(); armed != armedJudged {
		t.Fatalf("the node is armed %q after the judge called its work wide, want %q", armed, armedJudged)
	}
	kids := graph.children(id)
	if len(kids) != 3 {
		t.Fatalf("the node has %d parts under it, want the 3 the judge named", len(kids))
	}
	for _, kid := range kids {
		kid.finish(kid.title()+" is done", nil, "", "")
		graph.complete(kid, TaskDone)
	}
	second := completer.request(t, 1)
	if !strings.Contains(second, divisionHandedOutBeside) || !strings.Contains(second, "split into 3 parts:") {
		t.Fatalf("the worker never read the receipt for its parts: %q", second)
	}
	select {
	case <-graph.node(id).done:
	case <-time.After(30 * time.Second):
		t.Fatal("the divided work never landed after its parts reported")
	}
}

// THE JUDGE'S YES IS WRITTEN OUT AS A DIVISION the way a drawing is: a part per
// part it named, each with the stand-in done-condition the reviewer sharpens,
// and the sentence the judge read as the evidence. A yes naming one part is not
// a division, by the floor a drawing is held to.
func TestTheJudgesYesIsWrittenOutAsADivision(t *testing.T) {
	ask := "update the three regional reports"
	args, ok := judgedDivision(ask, []string{"the north report", "the south report", "C"}, "three regions")
	if !ok || len(args.Parts) != 3 {
		t.Fatalf("division = %+v ok=%v", args, ok)
	}
	if !strings.Contains(args.Evidence, ask) || !strings.Contains(args.Evidence, "three regions") {
		t.Fatalf("evidence %q, want the sentence the judge read and its reason", args.Evidence)
	}
	if part := args.Parts[0]; part.Title != "the north report" || part.Acceptance != divisionStandInDone("the north report") {
		t.Fatalf("first part = %+v", part)
	}
	if bare := args.Parts[2].Title; bare == "C" {
		t.Fatal("a bare label reached the rail as a name")
	}
	if _, ok := judgedDivision(ask, []string{"the whole thing"}, "one job"); ok {
		t.Fatal("a yes naming one part was written out as a division")
	}
}

// AND A READING THAT LANDS AFTER THE WORKER HAS SAID ITS LAST WORD IS DROPPED.
// Parts admitted under a worker that will never read again would be parts nobody
// folds; the reading sees the worker gone from its room and writes down that it
// had nobody to hand its answer to.
func TestADrawingWeighedAfterTheWorkerFinishedHandsNothingOut(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B | C",
		"A is the auth test, B is the http client, C is the release notes")),
		0, &scriptedCompleter{}, nil)
	// The runner withdraws the worker at its last read (task_child_run.go).
	nest.parent.openRoom().speaking(nil)

	receipt, person, stopped := weighBeside(t, nest.node)

	if receipt != "" || person != "" || stopped {
		t.Fatalf("a finished worker was handed %q / %q (stopped %v)", receipt, person, stopped)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were admitted under a worker that will never fold them", len(kids))
	}
	if queued := queuedFor(nest.node); queued != "" {
		t.Fatalf("a finished worker had %q queued for a request it will never make", queued)
	}
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 || divisions[0].Decision != divisionDropped {
		t.Fatalf("the journal reads %+v, want the reading written down as dropped", divisions)
	}
}

// AND ONE THE WORKER HAS ALREADY ANSWERED BY HANDING WORK OUT ITSELF IS DROPPED
// TOO: two divisions of one piece of work are two answers to one question. The
// worker hands its piece out WHILE the drawing is being read, which is the only
// moment the two can cross — a node with parts before the reading starts is never
// read at all ([Agent.sizeBeside]).
func TestADrawingWeighedAfterTheWorkerHandedWorkOutIsDropped(t *testing.T) {
	var nest *divideNest
	reviewer := &crossingReviewer{review: sketchReviewer("the auth test", "the release notes"), before: func() {
		id := nest.graph.reserve()
		nest.graph.admit(id, taskSpec{title: "a piece the worker handed out itself", request: personSentence,
			brief: "b", acceptance: "a", parent: nest.parent.id, depth: 2, owner: nest.node})
	}}
	nest = newDivideNestFrom(t, drawnSpec(countedSketch("A | B",
		"A is the auth test, B is the release notes")), 0, reviewer, nil)

	receipt, _, _ := weighBeside(t, nest.node)

	if receipt != "" {
		t.Fatalf("the drawing was handed out on top of the worker's own piece: %q", receipt)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 1 {
		t.Fatalf("the node has %d pieces, want the one the worker handed out itself", len(kids))
	}
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 || divisions[0].Decision != divisionDropped || divisions[0].Source != divisionBySketch {
		t.Fatalf("the journal reads %+v, want the drawing written down as dropped", divisions)
	}
}

// crossingReviewer is a reviewer with one thing that happens while it reads.
type crossingReviewer struct {
	review *divideReviewer
	before func()
	once   sync.Once
}

func (r *crossingReviewer) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if len(messages) > 0 && messageText(messages[0]) == divideReviewBrief {
		r.once.Do(r.before)
	}
	return r.review.CompleteWithMessages(ctx, messages, options...)
}

// AND THE ROW SAYS NOTHING ABOUT A READING NOBODY IS WAITING ON. The worker is at
// its own work, and "sizing the work" over it would be the row explaining a wait
// nobody is in; the worker's own `divide_work` still draws the word, because
// there the worker IS waiting (task_phase_test.go).
func TestAReadingBesideTheWorkerMovesNoPhase(t *testing.T) {
	nest := newDivideNestFrom(t, drawnSpec(countedSketch("A | B",
		"A is the auth test, B is the release notes")),
		0, sketchReviewer("the auth test", "the release notes"), nil)

	if receipt, _, _ := weighBeside(t, nest.node); receipt == "" {
		t.Fatal("the drawing handed nothing out, so this proves nothing about the row")
	}
	if life := nest.parent.lifeNow(); life != "" {
		t.Fatalf("the node's life moved to %q for a reading nobody was waiting on", life)
	}
}

// ── the manual knows ────────────────────────────────────────────────────────

func TestTheManualSaysAHandedOverTurnIsDividedWhileItsWorkerWorks(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "manual", "chat", "tasks.md"))
	if err != nil {
		t.Fatalf("reading the manual page: %v", err)
	}
	for _, want := range []string{"while its worker is already at work", "one worker"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("the manual never says %q about a turn handed over with parts in it", want)
		}
	}
	if strings.Contains(string(page), "starts already divided") {
		t.Fatal("the manual still says a handed-over task starts already divided: the reading runs beside its worker now")
	}
}

// A CHAIN PIECE IS NAMED FROM EVERY LETTER IT HOLDS. `A > B` is a module and
// then its test; a name from the first letter alone would hide half of what the
// part owns, and a name from the raw letters ("a > b") names nothing.
func TestAChainPieceIsNamedFromEveryLetterInIt(t *testing.T) {
	segments := legendSegments("A is the slugify module, B is its test file, C is the chunk module")
	if got := sketchSaid("A > B", segments); got != "the slugify module, then its test file" {
		t.Fatalf("chain piece named %q", got)
	}
	if got := sketchSaid("C", segments); got != "the chunk module" {
		t.Fatalf("single piece named %q", got)
	}
	if got := sketchSaid("(A > B)", segments); got != "the slugify module, then its test file" {
		t.Fatalf("bracketed chain named %q", got)
	}
}

// ── work only a person can do ───────────────────────────────────────────────
//
// THE MEASURED FAILURE. On 2026-08-31 a task arrived whose whole remainder was
// `(A | B) > C` — approve PR #1018, approve PR #1019, and then the merge queue
// merging both by itself. The reviewer read the drawing and answered, in the
// journal, verbatim: "Nothing here can actually be divided or done by a worker:
// A and B are the same single action — an approving review GitHub will only
// accept from a human who isn't the author — and C is just the merge queue and
// CodeQL re-scan happening on their own afterward."
//
// It was right, it cost two cents, and it arrived before the node had spent
// anything. It was then thrown away: the division was journalled `refused:review`
// and the worker ran anyway — nine minutes, $1.24 over the run, the check, a
// repair round and the check again, spent fixing a file in an empty repository
// while it looked for something it could do, and failed by the check at the end.

// AND THE FIRST LAW OF THIS ROAD IS THE ONE THAT KEEPS IT SAFE: A REVIEWER BEING
// CAUTIOUS MUST NEVER PARK DOABLE WORK ON SOMEBODY.
//
// The two answers are one sentence apart in prose — "these parts are really one
// job" and "nobody here can do this" are both a refusal explaining itself — and
// they are not one sentence apart in what they do. So the finding is a FIELD the
// reviewer has to reach for, never a reading of its words, and everything it
// merely says is an ordinary refusal that leaves one worker carrying on. This
// test is written first because it is the whole reason the field exists.
func TestACautiousRefusalStillLeavesOneWorkerToDoTheWork(t *testing.T) {
	for _, test := range []struct {
		name string
		why  string
	}{
		{"it reads them as one job", "these are stages of one job"},
		{"it will not have the boundary", "parts 2 and 3 are the same file"},
		// THE WORDS OF THE OTHER ANSWER, WITHOUT THE ANSWER. A reviewer that says
		// this and does not reach for the field has refused a division, and the
		// work is still work: a road that read the sentence would stop it here.
		{"it says the shape of the other answer without giving it",
			"nobody could split this sensibly and a person should really look at it"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &divideReviewer{answer: `{"refuse": true, "why": "` + test.why + `"}`}
			nest := newDivideNestFrom(t, drawnSpec(batchSketch("A | B",
				"A is the auth test, B is the release notes")), 0, reviewer, nil)

			said, person, stopped := weighBeside(t, nest.node)

			if person != "" {
				t.Fatalf("a refusal that only said %q parked the work on a person: %q", test.why, person)
			}
			if said != "" || stopped {
				t.Fatalf("a refused division told the worker %q (stopped %v), want it left at its work", said, stopped)
			}
			if queued := queuedFor(nest.node); queued != "" {
				t.Fatalf("a refused division queued %q for a worker that never asked", queued)
			}
			if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
				t.Fatalf("a refused division still bore %d parts", len(kids))
			}
			divisions := journaledDivisions(t, nest.journal)
			if len(divisions) != 1 || divisions[0].Decision != divisionRefusedReview {
				t.Fatalf("the journal reads %+v, want an ordinary refused division", divisions)
			}
		})
	}
}

// AND THE MEASURED SHAPE STOPS THE WORKER. The reviewer's own sentence comes back
// out, the worker it ran beside is stopped — the one answer that interrupts a
// started worker — no parts are admitted, and the record says which finding this
// was, because a task sitting on a person is not the same fact as a division a
// mastermind read as one job.
func TestWorkNoWorkerCanDoStopsTheWorkerAndIsHandedBack(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "nobody": true, "why": ` +
		strconv.Quote(measuredHumanOnlyFinding) + `}`}
	nest := newDivideNestFrom(t, drawnSpec(batchSketch("(A | B) > C",
		"A is approving PR #1018, B is approving PR #1019, C is the merge queue merging both")),
		0, reviewer, nil)

	said, person, stopped := weighBeside(t, nest.node)

	if person == "" || !stopped {
		t.Fatalf("the reader said no worker could do this and the worker was left going (stopped %v)", stopped)
	}
	// THE READER'S OWN WORDS, because the difference between "somebody has to
	// approve this" and "somebody has to give you an account" is the whole of
	// what the person is being handed.
	if !strings.Contains(person, "an approving review GitHub will only accept from a human") {
		t.Fatalf("the person is handed %q, and not what the reader actually found", person)
	}
	// AND NOT THE RECORD'S OWN BOOKKEEPING, which the journal keeps and a card
	// does not.
	if strings.HasPrefix(person, "refused") {
		t.Fatalf("the person's own job is handed over as %q", person)
	}
	if said != "" {
		t.Fatalf("a worker was told %q about parts that do not exist", said)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were admitted for work no worker can do", len(kids))
	}
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 || divisions[0].Decision != divisionRefusedNobody {
		t.Fatalf("the journal reads %+v, want the person named as what this came to", divisions)
	}
	if !strings.Contains(divisions[0].Error, "an approving review GitHub will only accept") {
		t.Fatalf("the record keeps %q and not the reason", divisions[0].Error)
	}
	// AND NOTHING WAS SPENT BUT THE READING ITSELF. One call — the reader's —
	// which is the whole of what this road costs when it stops the work.
	if spent := nest.node.Usage(); spent.Calls != 1 {
		t.Fatalf("stopping the work took %d calls, want the one reading", spent.Calls)
	}
}

// measuredHumanOnlyFinding is what the reader actually wrote on 2026-08-31,
// copied out of the journal. It is here rather than paraphrased because what is
// under test is a real answer's shape, and a tidied one would be a test about
// prose somebody wrote for it.
const measuredHumanOnlyFinding = "Nothing here can actually be divided or done by a worker: A and B are the same single action — an approving review GitHub will only accept from a human who isn't the author — and C is just the merge queue and CodeQL re-scan happening on their own afterward. The one real job left is to escalate the approval blocker to the user and then verify the merge and alert closure, and that is one pair of hands, not three."

// AND A WORKER THAT IS ALREADY RUNNING IS TOLD TO STOP RATHER THAN TO CARRY ON.
// It cannot be unspent — that is what the road above is for — but "carry on with
// the work in your own hands", which is what every other refusal here says and
// what this one used to say, is the instruction that produced the fix to a file
// in an empty repository.
func TestAWorkerThatAsksIsToldToStopAndSaySoRatherThanCarryOn(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "nobody": true, "why": "only a person can approve these pull requests"}`}
	nest := newDivideNestFrom(t, judgedWide, 0, reviewer, nil)

	answer := nest.divide(t, divideArgs(issueEvidence, 2))

	if !strings.HasPrefix(answer, "not split:") {
		t.Fatalf("the worker was told %q, want the same refusal shape the gates use", answer)
	}
	if !strings.Contains(answer, "it needs a person") {
		t.Fatalf("the worker is told %q and never that this needs somebody", answer)
	}
	if !strings.Contains(answer, "only a person can approve these pull requests") {
		t.Fatalf("the worker is told %q and never what the reader found", answer)
	}
	if strings.Contains(answer, "Carry on with the work in your own hands") {
		t.Fatalf("the worker is told to carry on with work nobody can do: %q", answer)
	}
	if !strings.Contains(answer, "say so in your report") {
		t.Fatalf("the worker is told %q and never to hand it back", answer)
	}
	// THE VOCABULARY LAW: a person reads this over the worker's shoulder.
	assertPlainWords(t, "what the worker is told about work only a person can do", answer)
}

// ── the judge's yes arms the worker it was read beside (#958) ───────────────

// A JUDGED-WIDE TASK WHOSE FIRST PARTS THE REVIEWER REFUSES KEEPS THE VERB.
//
// Two readers answered two different questions here. The judge read the person's
// sentence and said the WORK was wide; the reviewer read one set of parts drawn
// out of that same sentence, before anybody had opened the material, and said
// THOSE were one job. The second answer does not undo the first — so the worker
// that is already running gains `divide_work` and the page that says how to use
// it, which is exactly what it would have been constructed holding while the
// judge was still asked in front of the work.
//
// IT IS ASSERTED FROM THE OUTSIDE: what the wire offered on the first request,
// and what the running worker's own belt and instructions carry once the reading
// has been joined. The join is the runner's (task_beside.go's law: no reading
// outlives the node that started it), so the node landing is a fact that happens
// after the refusal and everything it decided.
func TestAJudgedWideTaskWhoseFirstPartsAreRefusedStillGainsTheDivideVerb(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newBesideCompleter(`{"refuse": true, "why": "these are stages of one job"}`)
	completer.judge = `{"parallelizable":true,"parts":["the auth test","the http client","the release notes"],"why":"three independent jobs"}`
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	// THE WORKER STAYS AT ITS FIRST ANSWER UNTIL THE REVIEWER HAS SPOKEN. A
	// reading does not outlive the node that started it, so a worker whose whole
	// run is one "Done." would cancel the judge mid-sentence and this test would
	// be about that instead. The refusal is decided after the answer this waits
	// for, and the join below is what makes it a fact.
	completer.hold = completer.reviewed

	ask := "bring the flaking auth test, the http client upgrade and the release notes up to date"
	id, _, _, err := session.StartTask(t.Context(), ask, false)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.node(id)

	// THE FIRST REQUEST WENT OUT WITHOUT THE VERB, which is the shape this is
	// about: the belt was built before anybody had read the work for width.
	completer.request(t, 0)
	if offered := completer.offeredAt(t, 0); slices.Contains(offered, "divide_work") {
		t.Fatal("the worker's first request already carried divide_work, so this is not the branch #958 is about")
	}
	worker := node.openRoom().speaker()
	if worker == nil {
		t.Fatal("no worker was in the room after its first request")
	}

	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the task never landed after its division was refused")
	}

	// THE REVIEWER DID READ THE PARTS AND DID REFUSE THEM. Without both halves
	// this test would pass on a run where the judge was never asked at all.
	if completer.verdictAt() == 0 {
		t.Fatal("the reviewer was never asked, so no refusal was under test")
	}
	if kids := graph.children(id); len(kids) != 0 {
		t.Fatalf("%d parts were born from a division the reviewer refused", len(kids))
	}
	// AND THE WORK IS ARMED, ON THE JUDGE'S OWN WORD.
	if armed := node.armedBy(); armed != armedJudged {
		t.Fatalf("a refused division left the work armed %q, want the judge's own %q", armed, armedJudged)
	}
	// THE BELT AND THE INSTRUCTIONS AGREE, because both were rebuilt from that
	// one write: the verb the worker can reach for, and the page telling it what
	// the verb is for.
	if !liveBeltHas(worker, "divide_work") {
		t.Fatal("the running worker's belt never gained divide_work after the refusal")
	}
	if instructions := liveSystem(worker); !strings.Contains(instructions, dividePromptHeading) {
		t.Fatalf("the running worker's instructions never gained the divide page: %q", clip(instructions, 400))
	}
}

// AND THE CONTROL: WORK NOBODY CALLED WIDE GAINS NOTHING. The same door, the
// same reading beside the same worker, and a judge that says no — the belt and
// the instructions are what narrow work has always had, so the door arms what
// was judged and not everything that passes it.
func TestATaskTheJudgeCallsNarrowNeverGainsTheDivideVerb(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newBesideCompleter(`{"refuse": true, "why": "unused"}`)
	completer.judge = `{"parallelizable":false,"why":"one job"}`
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	// The judge is the only reader this road reaches, so its no is what the
	// worker waits for — see the test above for why it waits at all.
	completer.hold = completer.judged

	id, _, _, err := session.StartTask(t.Context(), "fix the flaking reconciler test", false)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.node(id)
	completer.request(t, 0)
	worker := node.openRoom().speaker()
	if worker == nil {
		t.Fatal("no worker was in the room after its first request")
	}

	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the narrow task never landed")
	}

	if armed := node.armedBy(); armed != "" {
		t.Fatalf("work the judge called narrow came away armed %q", armed)
	}
	if liveBeltHas(worker, "divide_work") {
		t.Fatal("work nobody called wide carries divide_work: the door arms everything")
	}
	if instructions := liveSystem(worker); strings.Contains(instructions, dividePromptHeading) {
		t.Fatal("work nobody called wide is told how to use a verb it does not have")
	}
	if kids := graph.children(id); len(kids) != 0 {
		t.Fatalf("%d parts exist under work nobody called wide", len(kids))
	}
}

// dividePromptHeading is the divide page's own first heading, read from the page
// rather than written out again here: a test that quoted it would go on passing
// after somebody rewrote the page it is asserting the presence of.
var dividePromptHeading = firstLine(strings.TrimSpace(dividePrompt))

// liveBeltHas asks the belt the agent IS HOLDING, never one rebuilt from its
// config: this file is about a belt that gains a verb while its worker runs, and
// a rebuild would answer about the arming rather than about the worker.
func liveBeltHas(agent *Agent, name string) bool {
	for _, tool := range agent.beltTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// liveSystem is the instructions the agent's next request would carry, read the
// same way and for the same reason.
func liveSystem(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.system
}
