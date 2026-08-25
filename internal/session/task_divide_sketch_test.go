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
	"fmt"
	"os"
	"path/filepath"
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

	said := nest.node.divideFromSketch(context.Background())

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
	reviewer := sketchReviewer("the auth test", "the release notes")
	nest := newDivideNestFrom(t, drawnSpec(batchSketch("A | B",
		"A is the flaking auth test, B is the release notes for 2.4")),
		0, reviewer, nil)

	nest.node.divideFromSketch(context.Background())

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

	nest.node.divideFromSketch(context.Background())

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

	if said := nest.node.divideFromSketch(context.Background()); said != "" {
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

	nest.node.divideFromSketch(context.Background())

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

			if said := nest.node.divideFromSketch(context.Background()); said != "" {
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

	if said := nest.node.divideFromSketch(context.Background()); said != "" {
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

	if said := nest.node.divideFromSketch(context.Background()); said == "" {
		t.Fatal("the first ask handed nothing out")
	}
	if said := nest.node.divideFromSketch(context.Background()); said != "" {
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

	said := nest.node.divideFromSketch(context.Background())

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

	if said := nest.node.divideFromSketch(context.Background()); said != "" {
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
			proposal, ok := test.drawn.proposal("the work so far")
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
	proposal, ok := drawn.proposal("the work so far")
	if !ok {
		t.Fatal("the drawing proposed nothing")
	}
	for _, want := range []string{"A | B", "A is the auth test", "WHAT WAS ASKED", personSentence} {
		if !strings.Contains(proposal.Evidence, want) {
			t.Fatalf("the evidence is %q, want %q in it", proposal.Evidence, want)
		}
	}
}

// ── the seam: before the worker's first request ─────────────────────────────

// THE PARTS EXIST BEFORE THE WORKER IS ASKED ANYTHING. That is the whole timing
// requirement, and it is the difference between a node that coordinates from the
// start and one that grinds through the work alone and discovers its parts never
// happened.
//
// It is asserted from the OUTSIDE — a real node run by the real runner — because
// there is no honest way to test an ordering from inside the function that owns
// it: what the worker's very first request carries is the fact.
func TestThePartsAreHandedOutBeforeTheWorkersFirstRequest(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &firstAskCompleter{}
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	// ONLY THE PARENT RUNS FOR REAL. Its parts are what this test counts, and
	// three more workers in three more worktrees would be measuring the frontier
	// rather than the seam. THEY ARE DELIBERATELY LEFT UNSTARTED, which is also
	// half of what is being shown: the parent does not finish while its parts are
	// outstanding, so this test never waits for it to.
	graph.run = func(node *TaskNode) {
		if node.parent == 0 {
			graph.runOwned(node)
		}
	}

	id := graph.reserve()
	spec := drawnSpec(countedSketch("A | B | C",
		"A is the flaking auth test, B is the http client major version, C is the release notes for 2.4"))
	spec.model = "test/model"
	graph.admit(id, spec)

	first := ""
	for waited := 0; waited < 100 && first == ""; waited++ {
		time.Sleep(50 * time.Millisecond)
		first = completer.firstAsk()
	}
	if first == "" {
		t.Fatal("the worker was never asked anything")
	}

	if kids := graph.children(id); len(kids) != 3 {
		t.Fatalf("the node was asked its brief with %d parts under it, want the 3 its drawing named", len(kids))
	}
	if !strings.Contains(first, "split into 3 parts:") {
		t.Fatalf("the worker's FIRST request does not know its parts exist: %q", first)
	}
	if !strings.Contains(first, briefAskHeading) {
		t.Fatalf("the worker's first request lost the person's own words: %q", first)
	}
	// AND THE NODE IS STILL OPEN, holding itself for reports that have not come:
	// the parent-stays law, reached by the same tail loop a mid-run division
	// reaches (task_run.go's [runTaskChild]).
	if parent := graph.node(id); parent.stateNow().settled() {
		t.Fatalf("the divided work landed %s with its parts still outstanding", parent.stateNow())
	}
}

// firstAskCompleter keeps the first thing the node's worker was ever asked, and
// answers everything with one word so the run ends at once.
type firstAskCompleter struct {
	mu    sync.Mutex
	first string
}

func (c *firstAskCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	// THE ERRANDS ARE NOT THE WORKER. A namer and the division's own reviewer are
	// side-calls this session makes about the work; what is under test is the first
	// thing the WORKER was asked, and a filter that missed them would answer with
	// the review's question.
	if system != titleSystem && system != taskNameSystem && system != divideReviewBrief {
		var asked strings.Builder
		for _, message := range messages {
			if message.Role == "user" {
				asked.WriteString(messageText(message))
				asked.WriteString("\n")
			}
		}
		c.mu.Lock()
		if c.first == "" {
			c.first = asked.String()
		}
		c.mu.Unlock()
	}
	return textResponse("Done."), nil
}

func (c *firstAskCompleter) firstAsk() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.first
}

// ── the manual knows ────────────────────────────────────────────────────────

func TestTheManualSaysAHandedOverTurnStartsAlreadyDivided(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "manual", "chat", "tasks.md"))
	if err != nil {
		t.Fatalf("reading the manual page: %v", err)
	}
	for _, want := range []string{"already divided", "one worker"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("the manual never says %q about a turn handed over with parts in it", want)
		}
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
