package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

type designingAgent struct {
	*fakeAgent
	answers []harnessAnswer
	lane    chan session.Event
}

func (d *designingAgent) ResolveHarness(id uint64, run bool, model string) {
	d.answers = append(d.answers, harnessAnswer{id: id, run: run, model: model})
}
func (d *designingAgent) HarnessDesigns() <-chan session.Event { return d.lane }

// ONE DOOR, which is the block's own law about where an answer goes
// (session's [session.Agent.ResolveQuestion]). A fake that lacked it took every
// answer and told nothing, which is a green test about a question nobody heard.
func (d *designingAgent) ResolveQuestion(answer session.Answer) error {
	return resolveThroughLanes(d, answer)
}

func designedPage() subharness.Harness {
	return subharness.Harness{Id: subharness.Id{Name: "research-helper", Desc: "Research a topic with cited sources"}, Program: subharness.Program{Nodes: []subharness.Node{{Id: "plan", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "plan the research"}}, {Id: "fetch", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "fetch the sources"}}, {Id: "verify", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "every claim cites a source"}}}, Edges: []subharness.Edge{{"plan", "fetch"}, {"fetch", "verify"}}}, Whitelist: []string{"read", "grep"}, Verify: subharness.Verify{Ladder: subharness.VerifyReport}}
}

// designShown puts one finished design on the lane and pumps it in, the way the
// program loop does.
func designShown(t *testing.T, events ...session.Event) (*designingAgent, *app) {
	t.Helper()
	agent := &designingAgent{
		fakeAgent: &fakeAgent{model: "m"},
		lane:      make(chan session.Event, len(events)+1),
	}
	a := newTestApp(agent)
	a.height = 44
	cmd := a.watchDesigns()
	if cmd == nil {
		t.Fatal("the surface did not open the design lane")
	}
	for _, event := range events {
		agent.lane <- event
	}
	for range events {
		drive(t, a, runCmd(cmd)...)
	}
	return agent, a
}

func TestHarnessProgressCollapsesIntoFeedCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.height = 60
	a.designEvent(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "research a topic"})
	a.designEvent(session.Event{Kind: session.EventHarnessProgress, ID: 7, Goal: "research a topic", Phase: "designing", Attempt: 1, Attempts: 3, ThoughtTail: "choose sources\nthen verify", Hint: "3 steps so far"})
	if got := plain(frame(a)); !strings.Contains(got, "choose sources") || !strings.Contains(got, "3 steps so far") {
		t.Fatalf("live block is absent:\n%s", got)
	}
	p := designedPage()
	a.designEvent(session.Event{Kind: session.EventHarnessDesignDone, ID: 7, Harness: &p})
	got := plain(frame(a))
	for _, want := range []string{"research-helper", "plan the research", "check — every claim cites a source", session.HarnessChangeKey + "  change it"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card is missing %q:\n%s", want, got)
		}
	}
	if a.asksHarness() {
		t.Fatal("the feed card became a modal question")
	}
}

// THE LANE ALSO CARRIES THE ANNOUNCE, and the announce NAMES THE TASK: one line,
// with the number that opens the design's own room on the end of it.
func TestTheDesignLaneNamesTheTaskItStarted(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind: session.EventHarnessDesign, Text: "triaging flaky tests", Hint: "designing",
		Task: &session.TaskNotice{ID: 4},
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "subharness · designing triaging flaky tests — task 4") {
		t.Fatalf("the announce does not name the task:\n%s", got)
	}
}

// AND AN ANNOUNCE WITH NO NODE ON IT SAYS NOTHING EXTRA. An older engine, or one
// over --host where designing is off, sends no task — and a separator standing
// in front of no number is punctuation pretending to be information.
func TestTheDesignAnnounceWithNoTaskDrawsNoTail(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind: session.EventHarnessDesign, Text: "triaging flaky tests", Hint: "designing",
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "subharness · designing triaging flaky tests") {
		t.Fatalf("the announce was never drawn:\n%s", got)
	}
	if strings.Contains(got, "— task") {
		t.Fatalf("the announce invented a task:\n%s", got)
	}
}

// AND THE NOTE NAMES THE MODEL WRITING THE PAGE when the session resolved one.
// A design is two calls on a model the person did not type — the designer role's
// (internal/roles), which sits high — and while it runs this note is the only
// thing on screen.
func TestTheDesignNoteNamesTheModelDesigning(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind:  session.EventHarnessDesign,
		Text:  "triaging flaky tests",
		Hint:  "designing",
		Model: "moonshot/kimi-k3",
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "subharness · designing with moonshot/kimi-k3 · triaging flaky tests") {
		t.Fatalf("the design note does not name the model designing:\n%s", got)
	}
}

// ── the design as a node ────────────────────────────────────────────────────
//
// A design used to have a chip of its own on the strip, because it had nothing
// else: no id, no room, no row. It is a task now (session's harness_task.go), so
// what is tested here is that it wears the ordinary furniture correctly — the
// phase in place of the state word, and a stop that does not promise a branch.

// designingNode is one node on the roster as the engine publishes a design:
// running, its phase named, and its kind saying what it is.
func designingNode(t *testing.T, phase string) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 30
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID:    4,
		Title: "subharness · " + designedGoal,
		Kind:  session.TaskKindHarness,
		State: session.TaskRunning,
		Doing: phase,
	}})
	return a
}

// designedGoal is a brief that fits the strip's own budget whole, so a test
// reading a row is reading the words and not the cut.
const designedGoal = "triage flaky tests"

// A DESIGN TAKES AN ORDINARY PLACE ON THE ROSTER, which rises for it the way it
// rises for any running node — and on the narrow frame where there is no roster,
// the strip carries it instead (taskstrip.go).
func TestADesignTakesAnOrdinaryPlaceOnTheRoster(t *testing.T) {
	a := designingNode(t, "designing")
	if !a.railShowing() || a.stripShowing() {
		t.Fatalf("a harness being designed did not reach the roster: rail=%v strip=%v",
			a.railShowing(), a.stripShowing())
	}
	if rail := plain(strings.Join(a.railRows(12), "\n")); !strings.Contains(rail, "harness") {
		t.Fatalf("the roster does not name the design:\n%s", rail)
	}
	a.width = 80
	a.touch()
	if !a.stripShowing() {
		t.Fatal("a narrow frame lost the design's only door")
	}
	if row := plain(a.stripRow(a.width)); !strings.Contains(row, "harness") {
		t.Fatalf("the strip does not name the design: %q", row)
	}
	a.width = 120
	a.touch()
	// AND THE CLOCK KEEPS TURNING FOR IT. Nothing else on this surface is moving,
	// so the spinner would freeze at the frame the design started on.
	if !a.tasksAnimating() {
		t.Fatal("the paint clock stopped while a harness was being designed")
	}
}

// THE PHASE IS THE STATE WORD. "running" is this program's word for what a
// design is doing and "designing" is the person's, so the row and the room's
// header both say the second one.
func TestADesignsPhaseIsWhatTheSurfaceSays(t *testing.T) {
	for _, phase := range []string{"designing", "awaiting your look"} {
		a := designingNode(t, phase)
		node := a.tasks[4]
		if node == nil {
			t.Fatal("the design never reached the roster")
		}
		if got := a.roomStateWord(node); got != phase {
			t.Fatalf("the room header says %q, want %q", got, phase)
		}
		rows := a.railUnder(node, 60)
		if len(rows) == 0 || !strings.Contains(plain(rows[0]), phase) {
			t.Fatalf("the rail row does not say %q: %q", phase, plain(strings.Join(rows, " / ")))
		}
		// AND IT DOES NOT WEAR "finishing · " OR "waiting · " IN FRONT OF IT. Those
		// are this surface's words for which part of running a node is in; a phase
		// is not part of running, it is what the node is doing, so the row opens
		// with the phase itself.
		if got := plain(rows[0]); !strings.HasPrefix(got, phase) {
			t.Fatalf("the phase was prefixed with a word about running: %q", got)
		}
	}
}

// STOPPING A DESIGN PROMISES WHAT IS ACTUALLY TRUE OF IT. An ordinary node's
// card says the branch it wrote on is kept; a design has no branch and wrote no
// files, and what a person stopping one needs to know is that the registry is
// untouched.
func TestStoppingADesignDoesNotPromiseABranch(t *testing.T) {
	a := designingNode(t, "designing")
	target := a.stopTaskTarget(a.tasks[4])
	if target.empty() {
		t.Fatal("a running design cannot be stopped")
	}
	if target.id != session.CancelTask+":4" {
		t.Fatalf("the stop is aimed at %q", target.id)
	}
	if target.detail != stopDesignDetail {
		t.Fatalf("the card says %q", target.detail)
	}
	// And an ordinary node is untouched by any of this.
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 5, Title: "Fix the crash", State: session.TaskRunning,
	}})
	if got := a.stopTaskTarget(a.tasks[5]).detail; got != stopTaskDetail {
		t.Fatalf("an ordinary task's card says %q", got)
	}
}

// A SURFACE WITH NO DESIGNER UNDER IT OPENS NO LANE. Every scripted agent in
// this package is one, and none of them may be made un-representable by a
// feature they have never heard of.
func TestASessionWithNoDesignerOpensNoLane(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if cmd := a.watchDesigns(); cmd != nil {
		t.Fatal("a lane was opened on a session that has no designer")
	}
	if a.designLane != nil {
		t.Fatal("the surface kept a lane it never got")
	}
}

// designAsked puts one finished page in the feed with its question up and past
// the settle guard, which is where a person is when they answer it.
func designAsked(t *testing.T) (*designingAgent, *app) {
	t.Helper()
	agent := &designingAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	p := designedPage()
	a.finishHarnessCard(session.Event{ID: 9, Harness: &p})
	a.sel = len(a.entries) - 1
	harnessSettled(t, a)
	return agent, a
}

// THE THREE DIGITS THAT ANSWER, and each of them answers what it says.
//
// THEY USED TO BE `enter`, `e` AND `esc`, which is a grammar this surface no
// longer has: every question the engine hands a person is answered by the number
// beside the answer, `enter` takes the pick, and `esc` means LATER
// (questionkeys.go's one table). A design's page was the last block asking with
// keys of its own.
func TestADesignsThreeAnswersEachDoWhatTheySay(t *testing.T) {
	for _, tc := range []struct {
		name, key, word string
		answered        bool
		run             bool
	}{
		{"save it", session.HarnessSaveKey, "saved as research-helper v1", true, true},
		{"drop it", session.HarnessDropKey, "dropped", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, a := designAsked(t)
			drive(t, a, key(tc.key))
			c := a.entries[a.sel].harness
			if c.state != tc.word {
				t.Fatalf("the card in the feed says %q, want %q", c.state, tc.word)
			}
			if len(agent.answers) != 1 || agent.answers[0].run != tc.run || agent.answers[0].id != 9 {
				t.Fatalf("the engine was told %+v", agent.answers)
			}
			// AND THE QUESTION IS GONE, because the answer is the record.
			if a.questionOpenOn(session.QuestionHarness, 9) != nil {
				t.Fatal("an answered design is still being asked about")
			}
		})
	}
	// AND `esc` IS LATER RATHER THAN A NO. The page keeps standing, the chip
	// keeps counting it, and nothing has been decided about it — which is the one
	// meaning of this key everywhere on the block (question.go's esc law).
	t.Run("esc is later", func(t *testing.T) {
		agent, a := designAsked(t)
		drive(t, a, key("esc"))
		if len(agent.answers) != 0 {
			t.Fatalf("esc resolved the design: %+v", agent.answers)
		}
		if got := a.entries[a.sel].harness.state; got != "" {
			t.Fatalf("esc settled the card: %q", got)
		}
		if a.questionCount() != 1 {
			t.Fatalf("the chip counts %d questions after a question was put off", a.questionCount())
		}
	})
}

// AND `e` DESTROYS NOTHING, which is the whole of what changed about it.
//
// It was labelled "improve" and it called ResolveHarness with FALSE — the same
// call the discard key makes — and then prefilled the message box, so asking for
// a change silently threw away the page you were asking about. It is a door into
// the design's own room now (harnesscard.go), which is where a change is
// actually made, and the card is left exactly as it was: unanswered, on screen,
// still answerable.
func TestAskingToChangeADesignNeverDropsIt(t *testing.T) {
	agent, a := designAsked(t)
	drive(t, a, key(session.HarnessChangeKey))
	c := a.entries[a.sel].harness
	if len(agent.answers) != 0 {
		t.Fatalf("a key that only asks for a change answered the card: %+v", agent.answers)
	}
	if c.state != "" {
		t.Fatalf("the card settled on a key that settles nothing: %q", c.state)
	}
	if strings.Contains(a.input.String(), "Improve harness") {
		t.Fatalf("the prefill that stood in for a rewrite is still here: %q", a.input.String())
	}
	// AND THE QUESTION IS STILL UP, which is what "changes nothing" means on the
	// block: the engine's own door refuses to resolve a design on this answer
	// ([session.AnswerResolves]), so the page is still there to be judged when
	// the rewrite lands.
	if a.questionOpenOn(session.QuestionHarness, 9) == nil {
		t.Fatal("asking for a change closed the question it was asked on")
	}
}

// AND IT ANSWERS NOTHING WHILE THE KEY IS AIMED SOMEWHERE ELSE.
//
// The card used to read `enter`, `e` and `esc` with no guard at all (input.go's
// key order claimed it had every guard `x` has, and it had none), so a bare `e`
// in the middle of "even the tests pass" walked into the design's room with the
// sentence still in the box. The answers are digits on the block now, and the
// block's own law is the guard: EVERY PRINTABLE KEY BELONGS TO THE COMPOSER the
// moment there are words in the box, and a page that takes the whole frame is a
// page whose keys are its own.
func TestADesignCardAnswersNothingWhileTheKeyIsAimedSomewhereElse(t *testing.T) {
	// A sentence in the box: the digit types, and the page is left standing.
	_, a := designAsked(t)
	a.input.setText("give me 100")
	drive(t, a, key(session.HarnessSaveKey))
	if got := a.entries[a.sel].harness.state; got != "" {
		t.Fatalf("a digit typed mid-sentence answered the design: %q", got)
	}
	if got := a.input.String(); got != "give me 1001" {
		t.Fatalf("the digit did not reach the draft: %q", got)
	}
	if a.room != nil {
		t.Fatalf("a digit typed mid-sentence opened a room: %+v", a.room)
	}

	// And on home the three digits are home's, whichever of them it is.
	for _, name := range []string{session.HarnessSaveKey, session.HarnessChangeKey, session.HarnessDropKey} {
		agent, a := designAsked(t)
		a.openHome()
		drive(t, a, key(name))
		if got := a.entries[a.sel].harness.state; got != "" {
			t.Fatalf("%s on home answered the design underneath it: %q", name, got)
		}
		if len(agent.answers) != 0 {
			t.Fatalf("%s on home resolved the design: %+v", name, agent.answers)
		}
	}
	// The digit is the one that proves home GOT it, rather than that nobody did.
	_, a = designAsked(t)
	a.openHome()
	drive(t, a, key(session.HarnessSaveKey))
	if got := a.home.box.String(); got != session.HarnessSaveKey {
		t.Fatalf("the digit reached home's box as %q, want %q", got, session.HarnessSaveKey)
	}
}

// designingRoomAgent is a designer that also has rooms, which is the shape a
// real session has: `e` walks into the design's room, and a surface whose agent
// had never heard of one could not be asked to.
type designingRoomAgent struct {
	*roomFake
	answers []harnessAnswer
	lane    chan session.Event
}

func (d *designingRoomAgent) ResolveHarness(id uint64, run bool, model string) {
	d.answers = append(d.answers, harnessAnswer{id: id, run: run, model: model})
}
func (d *designingRoomAgent) HarnessDesigns() <-chan session.Event { return d.lane }

func (d *designingRoomAgent) ResolveQuestion(answer session.Answer) error {
	return resolveThroughLanes(d, answer)
}

// AND IT WALKS INTO THE ROOM when the design has one, because that is where the
// thread holding the page is listening.
func TestAskingToChangeADesignOpensItsRoom(t *testing.T) {
	a := newTestApp(&designingRoomAgent{roomFake: &roomFake{
		taskFake: &taskFake{fakeAgent: &fakeAgent{model: "m"}},
		lanes:    map[uint64]chan session.Event{},
	}})
	a.tasks = map[uint64]*taskNode{4: {id: 4, title: "design helper", label: "harness · helper"}}
	p := designedPage()
	a.finishHarnessCard(session.Event{ID: 4, Harness: &p, Task: &session.TaskNotice{ID: 4}})
	a.sel = len(a.entries) - 1
	harnessSettled(t, a)
	drive(t, a, key(session.HarnessChangeKey))
	if a.room == nil || a.room.id != 4 {
		t.Fatalf("the design's room did not open: %+v", a.room)
	}
}

// THE FEED'S DESIGN CARD IS THE ONE CARD, IN PLAIN SPEECH AND WITHOUT A BORDER.
//
// It used to draw its own: an ASCII architecture diagram (`[plan]──▶[fetch]`), a
// bullet per node carrying the raw field off it, and rows reading `verify:
// report` and `tools: read · grep` — the ladder's own word and the belt's
// registered ids — all inside a `│ … │` box. Every one of those is a name for
// something inside this binary, drawn at somebody who has never seen inside it,
// and the box is furniture this surface does not own.
func TestTheFeedsDesignCardIsThePlainSpeechCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	p := designedPage()
	c := &harnessCard{id: 1, page: &p}
	got := plain(strings.Join(a.harnessFeedRows(c, 120, false), "\n"))

	// The words are subharness.CardParts's, line for line.
	for _, line := range subharness.CardLines(p) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(got, strings.TrimRight(line, " ")) {
			t.Fatalf("the card's own line %q is not on the feed card:\n%s", line, got)
		}
	}
	// NOT ONE GLYPH OF BOX DRAWING. The no-borders law, and the emphasis law with
	// it: there is no rule to paint an accent along.
	for _, glyph := range []string{"│", "┌", "└", "┐", "┘", "─", "▶", "├", "▼"} {
		if strings.Contains(got, glyph) {
			t.Fatalf("the design card draws box furniture (%q):\n%s", glyph, got)
		}
	}
	// AND NOT ONE WORD OF MACHINERY: no ladder rung, no node kind, no tool id.
	for _, word := range []string{"verify:", "tools:", "report", "agent.loop", "grep", "invariants", "whitelist"} {
		if strings.Contains(got, word) {
			t.Fatalf("the design card says %q, which is a name from inside this binary:\n%s", word, got)
		}
	}
	// The tool words a person reads are there instead.
	for _, word := range []string{"reads files", "searches inside files"} {
		if !strings.Contains(got, word) {
			t.Fatalf("the card does not say what its tools do (%q):\n%s", word, got)
		}
	}
}

// AND THE CARD'S OWN INDENTATION SURVIVES A NARROW FRAME. A lane's steps are
// indented under the thing that opens them, which is the whole of how the card
// says structure — so a frame too narrow for a row CUTS it and never re-flows it.
func TestTheFeedsDesignCardIsCutAndNeverReflowed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	p := designedPage()
	p.Program.Nodes[0].Fields["brief"] = "plan the research and say what each source is for"
	c := &harnessCard{id: 1, page: &p}
	rows := a.harnessFeedRows(c, 40, false)
	for _, row := range rows {
		if width := len([]rune(plain(row))); width > 40 {
			t.Fatalf("a card row ran past the frame (%d cells): %q", width, plain(row))
		}
	}
	// The step that was cut is still ONE row, in its own column, rather than two.
	planned := 0
	for _, row := range rows {
		if strings.Contains(plain(row), "plan") {
			planned++
		}
	}
	if planned != 1 {
		t.Fatalf("the cut step was re-flowed into %d rows:\n%s", planned, plain(strings.Join(rows, "\n")))
	}
}

func TestHarnessStallShape(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	c := &harnessCard{phase: "designing", stalled: true, began: a.now().Add(-52e9)}
	got := plain(strings.Join(a.harnessFeedRows(c, 140, false), "\n"))
	if !strings.Contains(got, "reasoning models answer in one burst at the end") {
		t.Fatal(got)
	}
}
