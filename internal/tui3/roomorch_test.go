package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"os"
	"path/filepath"
)

// ── THE ADAPTIVE RUN'S PAGE ─────────────────────────────────────────────────
//
// What these hold shut, in the order a person meets them: the shape (a snapshot
// becomes layers, and the layers are the launch order), the crystallizing (a
// node that arrived is marked, once), the drill-in (a chip is a card and a
// card's needs are doors), the gate (the run is out of fuel and three keys say
// what happens next), and the steer lane (words go to the planner, and the page
// says so before the planner has said anything back).

// orchFake is a session with an adaptive run under it: the three doors of
// [orchAgent], over snapshots a test rewrites between polls.
//
// It is a widening of [fakeAgent] rather than a fake of its own for the reason
// [roomFake] widens [taskFake]: the surface asserts the run doors separately
// from everything else it needs, so a fake that could not be a plain session
// would be testing a shape the surface never requires.
type orchFake struct {
	*fakeAgent
	snaps    map[string]orchestrate.Snapshot
	steered  []string
	answers  []string
	steerErr error
	journals map[string]string
}

func (f *orchFake) OrchestrateNodeJournal(runID, nodeID string) (string, bool) {
	path, ok := f.journals[runID+":"+nodeID]
	return path, ok
}

func (f *orchFake) OrchestrateSnapshot(id string) (orchestrate.Snapshot, bool) {
	snap, ok := f.snaps[id]
	return snap, ok
}

func (f *orchFake) SteerOrchestrate(id, text string) error {
	if f.steerErr != nil {
		return f.steerErr
	}
	f.steered = append(f.steered, id+": "+text)
	return nil
}

func (f *orchFake) ResolveOrchestrate(id, answer string) (string, error) {
	f.answers = append(f.answers, id+": "+answer)
	return "", nil
}

// orchRun4 is the shape every test below starts from: a plan, two fetches that
// need it, and a write-up that needs both. One node in each of the three states
// a run actually shows.
func orchRun4() orchestrate.Snapshot {
	return orchestrate.Snapshot{
		Goal: "answer the retry question",
		Nodes: []orchestrate.NodeStatus{
			{Node: orchestrate.Node{ID: "plan", Goal: "decide what to read"},
				State: orchestrate.Done, Digest: "three RFCs and the client", Cost: 0.02},
			{Node: orchestrate.Node{ID: "rfcs", Goal: "read the three RFCs", Needs: []string{"plan"}},
				State: orchestrate.Running, Cost: 0.11},
			{Node: orchestrate.Node{ID: "client", Goal: "read our client", Needs: []string{"plan"}},
				State: orchestrate.Queued},
			{Node: orchestrate.Node{ID: "write", Goal: "write the answer", Needs: []string{"rfcs", "client"}},
				State: orchestrate.Queued},
		},
		Fuel: orchestrate.Fuel{Cap: 2, Spent: 0.87},
	}
}

// orchApp opens a page on one run at the wide tier.
func orchApp(t *testing.T, snap orchestrate.Snapshot) (*app, *orchFake) {
	t.Helper()
	agent := &orchFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		snaps:     map[string]orchestrate.Snapshot{"r1": snap},
	}
	a := newTestApp(agent)
	// 170 columns, because the page is drawn at the BODY's width and the roster's
	// permanent column takes thirty off it (task.go's [app.railShowing]): what
	// these tests mean by "wide" is the 140-column page they were written
	// against.
	a.width, a.height = 170, 30
	a.openOrchRoom("r1", snap.Goal)
	a.touch()
	if a.orchOf() == nil {
		t.Fatal("the run's page did not open")
	}
	return a, agent
}

// orchPoll drives one poll the way the program loop does.
func orchPollNow(t *testing.T, a *app) {
	t.Helper()
	if a.room == nil {
		t.Fatal("there is no page to poll")
	}
	drive(t, a, orchPollMsg{gen: a.room.gen})
}

// orchLines is the page as a reader sees it, blank rows dropped: the assertions
// below are about what is said and in what order, never about the spacing
// between two things that are both there.
func orchLines(a *app) []string {
	var out []string
	for _, line := range strings.Split(roomText(a), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return out
}

func orchLineAt(t *testing.T, a *app, want string) int {
	t.Helper()
	for i, line := range orchLines(a) {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("the page never says %q:\n%s", want, strings.Join(orchLines(a), "\n"))
	return -1
}

// THE SNAPSHOT IS A SHAPE, AND THE SHAPE IS THE LAUNCH ORDER. A node sits one
// layer below the deepest thing it needs, which is exactly the rule the
// scheduler launches by — so a person reading the page top to bottom is reading
// what can run now, what can run next, and what is waiting on both.
func TestTheGraphLaysItsNodesOutInTopologicalLayers(t *testing.T) {
	a, _ := orchApp(t, orchRun4())

	plan := orchLineAt(t, a, "plan")
	rfcs := orchLineAt(t, a, "rfcs")
	client := orchLineAt(t, a, "client")
	write := orchLineAt(t, a, "write")
	if client != rfcs+1 {
		t.Fatalf("two nodes of one layer are not consecutive rows: %d and %d\n%s",
			rfcs, client, strings.Join(orchLines(a), "\n"))
	}
	if !(plan < rfcs && client < write) {
		t.Fatalf("the layers are out of order (plan %d, frontier %d/%d, write %d):\n%s",
			plan, rfcs, client, write, strings.Join(orchLines(a), "\n"))
	}

	// THE GLYPHS ARE THE STATES, one cell each, and the ramp is readable without
	// colour: empty, half, solid.
	page := strings.Join(orchLines(a), "\n")
	for _, want := range []string{
		orchGlyphDone + " plan", orchGlyphRunning + " rfcs", orchGlyphQueued + " client",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the graph never draws %q:\n%s", want, page)
		}
	}
}

// THE EDGES ARE WORDS ON THE ROW. The old wide tier drew bare strokes under
// chips, which pointed at columns and said nothing; every node row now carries
// what it waits on in its dim tail, and a node nothing gated carries no tail
// at all — the emptiness law applied to an edge list.
func TestEveryNodeRowNamesWhatItWaitsOn(t *testing.T) {
	a, _ := orchApp(t, orchRun4())

	lines := orchLines(a)
	if strings.Contains(strings.Join(lines, "\n"), "│") {
		t.Fatalf("the page still draws connector strokes:\n%s", strings.Join(lines, "\n"))
	}
	for row, want := range map[string]string{
		"rfcs":   orchNeedsHead + " plan",
		"client": orchNeedsHead + " plan",
		"write":  orchNeedsHead + " rfcs client",
	} {
		if line := lines[orchLineAt(t, a, row)]; !strings.Contains(line, want) {
			t.Fatalf("the %s row does not say %q:\n%q", row, want, line)
		}
	}
	if line := lines[orchLineAt(t, a, "plan")]; strings.Contains(line, orchNeedsHead+" ") {
		t.Fatalf("a node nothing gated carries a needs tail:\n%q", line)
	}
}

// THE GRAPH CRYSTALLIZES: an amendment lands, the page grows a chip, and the
// chip says it is new — for one interval, because the growing is the signal and
// the word is only the punctuation under it.
func TestANewNodeIsMarkedForOneIntervalAndThenIsOrdinary(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	if strings.Contains(roomText(a), orchNewWord) {
		t.Fatalf("a page opened with everything marked new:\n%s", roomText(a))
	}

	snap := orchRun4()
	snap.Nodes = append(snap.Nodes, orchestrate.NodeStatus{
		Node:  orchestrate.Node{ID: "compare", Goal: "compare the two", Needs: []string{"rfcs"}},
		State: orchestrate.Queued,
	})
	agent.snaps["r1"] = snap
	orchPollNow(t, a)

	lines := orchLines(a)
	if line := lines[orchLineAt(t, a, "compare")]; !strings.Contains(line, orchNewWord) {
		t.Fatalf("the node that just arrived is not marked new:\n%q", line)
	}
	if line := lines[orchLineAt(t, a, "plan")]; strings.Contains(line, orchNewWord) {
		t.Fatalf("a node that was always there is marked new:\n%q", line)
	}

	// One more read, nothing added: the marker is spent.
	orchPollNow(t, a)
	if strings.Contains(roomText(a), orchNewWord) {
		t.Fatalf("the new marker outlived its interval:\n%s", roomText(a))
	}
	// And the chip is still there, in the layer its needs put it in.
	if orchLineAt(t, a, "compare") <= orchLineAt(t, a, "rfcs") {
		t.Fatal("the added node did not settle under the node it needs")
	}
}

// THE PLANNER'S NOTES ARE A SECTION OF THEIR OWN, under the graph and under
// their own dim heading. They used to be interleaved at the layer boundaries,
// where rows of talk between rows of work were the main thing that made the
// page unreadable.
func TestPlannerNotesAreGatheredUnderTheirOwnHeading(t *testing.T) {
	snap := orchRun4()
	snap.Notes = []string{"the client matters more than the RFCs"}
	a, _ := orchApp(t, snap)

	head := orchLineAt(t, a, orchPlannerHead)
	note := orchLineAt(t, a, "the client matters more")
	if head <= orchLineAt(t, a, "write") || note != head+1 {
		t.Fatalf("the narration is not a section under the graph:\n%s",
			strings.Join(orchLines(a), "\n"))
	}

	// A note that arrives on the LANE lands on the page too, without waiting for
	// the next snapshot to admit it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventOrchestrateNote, ID: 1, Text: "narrowing to timeouts",
	}})
	if !strings.Contains(roomText(a), "narrowing to timeouts") {
		t.Fatalf("a planner note off the lane never reached the open page:\n%s", roomText(a))
	}
}

// THE HEADER CARRIES THE TANK, and it carries it at every width the header is
// drawn at: a run spends on its own initiative, so the one number that says how
// much of the person's decision is left cannot be the thing that gets cut.
func TestTheHeaderCarriesTheGoalTheGaugeAndTheState(t *testing.T) {
	a, _ := orchApp(t, orchRun4())

	head := plain(a.roomHead(a.width))
	for _, want := range []string{"answer the retry question", "$0.87 / $2.00", stateWorking.String()} {
		if !strings.Contains(head, want) {
			t.Fatalf("the header is missing %q:\n%q", want, head)
		}
	}
	// Narrow the frame until the goal cannot fit: the gauge survives it.
	a.width = 52
	a.touch()
	head = plain(a.roomHead(a.width))
	if !strings.Contains(head, "$0.87 / $2.00") {
		t.Fatalf("the gauge was cut before the goal was:\n%q", head)
	}
}

// A CHIP IS A DOOR. Enter opens the card, the card says the four things a person
// walked in for, and esc comes back to the graph with the shape untouched.
func TestAChipOpensItsCardAndEscComesBack(t *testing.T) {
	a, _ := orchApp(t, orchRun4())

	drive(t, a, key("down")) // the first chip
	if got := a.orchOf().pick; got.node != "plan" {
		t.Fatalf("the cursor opened on %+v, want the first chip", got)
	}
	drive(t, a, key("enter"))
	if got := a.orchOf().card; got != "plan" {
		t.Fatalf("enter opened the card of %q", got)
	}
	page := roomText(a)
	for _, want := range []string{
		"decide what to read", orchDigestHead, "three RFCs and the client", "$0.02", orchCardBack,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the card never says %q:\n%s", want, page)
		}
	}

	drive(t, a, key("esc"))
	if a.orchOf().card != "" {
		t.Fatal("esc did not close the card")
	}
	if a.room == nil {
		t.Fatal("esc out of a card left the page as well")
	}
	if !strings.Contains(roomText(a), orchGlyphRunning+" rfcs") {
		t.Fatalf("the graph did not come back:\n%s", roomText(a))
	}
	// One more esc leaves the page altogether, which is the room's own key.
	drive(t, a, key("esc"))
	if a.room != nil {
		t.Fatal("esc on the graph did not leave the page")
	}
}

// A CARD'S NEEDS ARE LINKS AND NOT TEXT: "what did this depend on" is answered
// by GOING there, which is how anybody reads a result backwards.
func TestACardsNeedsAreNavigable(t *testing.T) {
	a, _ := orchApp(t, orchRun4())

	a.orchCardOpen("write")
	a.touch()
	page := roomText(a)
	if !strings.Contains(page, orchNeedsHead) {
		t.Fatalf("the card has no needs block:\n%s", page)
	}
	links := a.orchCardLinks()
	if len(links) != 3 || links[0].node != "rfcs" || links[1].node != "client" || links[2].transcript != "write" {
		t.Fatalf("the card's links are %+v, want its two needs and transcript", links)
	}

	drive(t, a, key("down")) // onto the second need
	drive(t, a, key("enter"))
	if got := a.orchOf().card; got != "client" {
		t.Fatalf("following a need opened %q, want client", got)
	}
	if !strings.Contains(roomText(a), "read our client") {
		t.Fatalf("the need's own card did not open:\n%s", roomText(a))
	}
}

// A NODE THAT IS ITSELF A RUN EXPANDS INTO ITS OWN SNAPSHOT, and the trail
// across the top says where you are. Esc comes back out one level.
func TestANestedRunExpandsAndTheTrailSaysWhereYouAre(t *testing.T) {
	snap := orchRun4()
	a, agent := orchApp(t, snap)
	agent.snaps["rfcs"] = orchestrate.Snapshot{
		Goal: "read the three RFCs",
		Nodes: []orchestrate.NodeStatus{
			{Node: orchestrate.Node{ID: "rfc7231"}, State: orchestrate.Done},
			{Node: orchestrate.Node{ID: "rfc9110", Needs: []string{"rfc7231"}}, State: orchestrate.Running},
		},
		Fuel: orchestrate.Fuel{Cap: 1, Spent: 0.4},
	}

	a.orchCardOpen("rfcs")
	a.touch()
	if !strings.Contains(roomText(a), orchRunHead) {
		t.Fatalf("a node that is a run does not say so on its card:\n%s", roomText(a))
	}
	links := a.orchCardLinks()
	if len(links) < 2 || links[len(links)-2].run != "rfcs" {
		t.Fatalf("the card has no link into the nested run: %+v", links)
	}
	a.orchOf().link = len(links) - 2
	drive(t, a, key("enter"))

	if got := a.orchOf().id; got != "rfcs" {
		t.Fatalf("the page is on run %q, want the nested one", got)
	}
	if !strings.Contains(roomText(a), "rfc9110") {
		t.Fatalf("the inner snapshot is not drawn:\n%s", roomText(a))
	}
	head := plain(a.roomHead(a.width))
	for _, want := range []string{"answer the retry question", "read the three RFCs", "$0.40 / $1.00"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the trail or the inner gauge is missing %q:\n%q", want, head)
		}
	}

	drive(t, a, key("esc"))
	if got := a.orchOf().id; got != "r1" {
		t.Fatalf("esc left the nested run on %q, want the outer one", got)
	}
	if a.room == nil {
		t.Fatal("esc out of a nested run closed the page")
	}
}

// ── THE GATE ────────────────────────────────────────────────────────────────

// orchPauseEvent is the session saying a run has spent its tank.
func orchPause() session.Event {
	return session.Event{Kind: session.EventOrchestratePause, ID: 1, Text: "$2.00 of $2.00"}
}

// THE PAUSE RAISES A QUESTION ON THE RUN'S OWN PAGE, and it brings the page with
// it: a gate nobody can see is a run parked forever.
func TestThePauseGateRendersAndRoutesEachAnswer(t *testing.T) {
	for _, one := range []struct {
		steps int
		want  string
	}{
		{0, orchTopUpAnswer(2)},
		{1, orchFinish},
		{2, orchStop},
	} {
		snap := orchRun4()
		snap.Paused = true
		a, agent := orchApp(t, snap)
		drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})

		if a.orchOf() == nil || a.orchOf().gate == nil {
			t.Fatalf("[%s] the pause raised no gate", one.want)
		}
		page := roomText(a)
		for _, want := range []string{
			glyphAsk, orchGateLead, "$2.00 of $2.00",
			orchAnswerWord(orchTopUpAnswer(2)), orchAnswerWord(orchFinish), orchAnswerWord(orchStop),
		} {
			if !strings.Contains(page, want) {
				t.Fatalf("[%s] the gate never says %q:\n%s", one.want, want, page)
			}
		}
		// A PAUSED RUN'S UNSTARTED NODES WEAR THE PAUSE: nothing new launches while
		// the gate is up, so an empty circle would be the wrong fact.
		if !strings.Contains(page, orchGlyphPaused+" client") {
			t.Fatalf("[%s] a queued node does not wear the pause:\n%s", one.want, page)
		}
		// THE CURSOR IS ON THE QUESTION the moment it is raised: it is the one
		// thing on the page somebody has to answer.
		if got := a.orchOf().pick; got.answer != orchTopUpAnswer(2) {
			t.Fatalf("[%s] the cursor is on %+v, want the gate's first answer", one.want, got)
		}
		for i := 0; i < one.steps; i++ {
			drive(t, a, key("down"))
		}
		drive(t, a, key("enter"))
		if len(agent.answers) != 1 || agent.answers[0] != "r1: "+one.want {
			t.Fatalf("[%s] the gate answered %v, want %q", one.want, agent.answers, one.want)
		}
		if a.orchOf().gate != nil {
			t.Fatalf("[%s] the question is still up after it was answered", one.want)
		}
		if !strings.Contains(roomText(a), orchActLead+orchAnswerWord(one.want)) {
			t.Fatalf("[%s] the page does not say what was answered:\n%s", one.want, roomText(a))
		}
	}
}

// A PAUSE OUTLIVES THE EVENT THAT ANNOUNCED IT.
//
// The gate is raised by the lane, and a lane is a thing that happens once: a
// page walked out of and re-opened builds a fresh [orchRun], so a run still
// sitting on its cap used to come back with "paused" in its header and no
// question anywhere under it — a run nobody could answer from the surface they
// were standing on. This is the reopen half of the room's own bug.
func TestReopeningAPausedRunRaisesTheGateFromTheSnapshot(t *testing.T) {
	snap := orchRun4()
	snap.Paused = true
	snap.Fuel = orchestrate.Fuel{Cap: 2, Spent: 2}
	a, agent := orchApp(t, snap)
	drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})

	// Out to the conversation and back in — the gesture the report is about.
	a.closeRoom()
	a.openOrchRoom("r1", "")
	a.touch()

	run := a.orchOf()
	if run == nil || run.gate == nil {
		t.Fatalf("the re-opened page lost the question a paused run is waiting on:\n%s",
			roomText(a))
	}
	page := roomText(a)
	// The spend sentence is the run's own, so the re-raised question reads exactly
	// as the lane's did rather than in the header's "/" form.
	for _, want := range []string{orchGateLead, "$2.00 of $2.00", orchAnswerWord(orchTopUpAnswer(2)),
		orchAnswerWord(orchFinish), orchAnswerWord(orchStop)} {
		if !strings.Contains(page, want) {
			t.Fatalf("the re-raised gate never says %q:\n%s", want, page)
		}
	}
	// It is the SAME question and not a picture of one: the answers route.
	if got := run.pick; got.answer != orchTopUpAnswer(2) {
		t.Fatalf("the cursor is on %+v, want the gate's first answer", got)
	}
	drive(t, a, key("enter"))
	if len(agent.answers) != 1 || agent.answers[0] != "r1: "+orchTopUpAnswer(2) {
		t.Fatalf("the re-raised gate answered %v", agent.answers)
	}

	// AND IT IS NOT ASKED TWICE. The tank is topped up on the far side of a poll,
	// so for one interval the shape still says paused — and a page that re-raised
	// there would be asking a person to answer what they just answered.
	orchPollNow(t, a)
	if a.orchOf().gate != nil {
		t.Fatalf("the answered gate came back on the next poll:\n%s", roomText(a))
	}
	// The run spends again, and then hits the next cap: that IS a new question.
	agent.snaps["r1"] = orchRun4()
	orchPollNow(t, a)
	paused := orchRun4()
	paused.Paused = true
	agent.snaps["r1"] = paused
	orchPollNow(t, a)
	if a.orchOf().gate == nil {
		t.Fatalf("the next pause raised no question:\n%s", roomText(a))
	}
}

// THE GATE CLAIMS NO LETTERS AT ALL, which is the whole reason its answers are
// rows rather than key chips: it cannot suspend the box the way a modal offer
// does, so any letter it claimed would be a letter somebody was typing at the
// planner. This is the test that keeps the two apart.
func TestTheGateNeverEatsASentenceBeingTyped(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})

	for _, r := range "fits" {
		drive(t, a, key(string(r)))
	}
	if got := a.input.String(); got != "fits" {
		t.Fatalf("the box holds %q — the gate ate the sentence", got)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("typing answered the gate: %v", agent.answers)
	}
	if a.orchOf().gate == nil {
		t.Fatal("the gate came down without being answered")
	}
}

// A PRESS ON THE GATE ANSWERS IT TOO, by column, the way every other question on
// this surface answers a pointer.
func TestThePauseGateAnswersAPress(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})

	rows := a.roomRows(a.bodyWidth())
	at, x := -1, 1
	for i, r := range rows {
		if strings.Contains(plain(r.text), orchAnswerWord(orchFinish)) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the gate's answers are not on the page:\n%s", roomText(a))
	}
	// The row's screen position, resolved the way the frame resolves it: the
	// page hangs from the top and the slack falls below it.
	y := a.bodyTop() + (at - a.roomOffsetFor(len(rows), a.viewHeight()))
	if !a.orchPress(x, y) {
		t.Fatal("the press landed on no target at all")
	}
	if len(agent.answers) != 1 || agent.answers[0] != "r1: "+orchFinish {
		t.Fatalf("the press answered %v, want finish", agent.answers)
	}
}

// ── STEERING ────────────────────────────────────────────────────────────────

// TYPING IN THE PAGE GOES TO THE PLANNER, and the page says so the moment it is
// accepted — not whenever the next snapshot admits it.
func TestSteeringARunReachesThePlannerAndIsEchoedAtOnce(t *testing.T) {
	a, agent := orchApp(t, orchRun4())

	typeLine(t, a, "skip the client, the RFCs are enough")
	if len(agent.steered) != 1 || agent.steered[0] != "r1: skip the client, the RFCs are enough" {
		t.Fatalf("the planner heard %v", agent.steered)
	}
	if !strings.Contains(roomText(a), orchSteerLead+"skip the client") {
		t.Fatalf("the page did not echo what was steered:\n%s", roomText(a))
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the box still holds %q after the words were taken", got)
	}
	// And the page does not say it twice once the snapshot catches up.
	snap := orchRun4()
	snap.Steer = []string{"skip the client, the RFCs are enough"}
	agent.snaps["r1"] = snap
	orchPollNow(t, a)
	if got := strings.Count(roomText(a), orchSteerLead); got != 1 {
		t.Fatalf("the steered line is drawn %d times, want once:\n%s", got, roomText(a))
	}
}

// A REFUSAL RAISES THE ROOM'S OWN GUARD rather than losing the words or sending
// them somewhere nobody pointed them (room.go's whole steer-guard law).
func TestARefusedSteerRaisesTheGuardAndKeepsTheWords(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	agent.steerErr = errors.New("run r1 has finished, there is no planner to talk to")

	typeLine(t, a, "narrow it")
	if !a.guarding() {
		t.Fatal("a refused steer raised no question")
	}
	if got := a.input.String(); got != "narrow it" {
		t.Fatalf("the box holds %q — the refusal lost the words", got)
	}
}

// ── THE NARROW AND PHONE TIERS ──────────────────────────────────────────────

// UNDER THE WIDE TIER THE EDGES ARE WRITTEN OUT IN WORDS: a connector under a
// wrapped layer would point at a chip that is no longer above it, and an edge a
// person cannot see is an edge that is not on the page.
func TestTheNarrowTierWritesEveryEdgeOutInWords(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.width = 70
	a.touch()

	page := roomText(a)
	if strings.Contains(page, "│") {
		t.Fatalf("the narrow tier is still drawing connectors:\n%s", page)
	}
	for _, want := range []string{
		orchNeedsHead + " plan", orchNeedsHead + " rfcs client",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the narrow tier never says %q:\n%s", want, page)
		}
	}
	// One node per row, so the goal rides the node's own line.
	if !strings.Contains(page, "rfcs read the three RFCs") {
		t.Fatalf("the narrow tier lost the goals:\n%s", page)
	}
}

// AT THE PHONE TIER EVERY CHIP IS THREE ROWS, and all three answer the same
// press: a finger covers about three rows, and a one-row target opens the node
// above or below the one somebody meant.
func TestThePhoneTierGivesEveryChipThreeRows(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.width, a.height = 44, 40
	a.touch()

	rows := a.roomRows(a.bodyWidth())
	run := a.orchOf()
	counts := map[string]int{}
	for i := range rows {
		if i >= len(run.spots) {
			break
		}
		for _, spot := range run.spots[i] {
			if spot.node != "" {
				counts[spot.node]++
			}
		}
	}
	for _, id := range []string{"plan", "rfcs", "client", "write"} {
		if counts[id] < 3 {
			t.Fatalf("%s answers %d rows at the phone tier, want three:\n%s",
				id, counts[id], roomText(a))
		}
	}
	// A press anywhere in the target opens that chip, including its last row.
	at := -1
	for i := range rows {
		if i < len(run.spots) && len(run.spots[i]) > 0 && run.spots[i][0].node == "rfcs" {
			at = i
		}
	}
	y := a.bodyTop() + at - a.roomOffsetFor(len(rows), a.viewHeight())
	if !a.orchPress(1, y) {
		t.Fatal("the last row of a phone chip is not pressable")
	}
	if got := a.orchOf().card; got != "rfcs" {
		t.Fatalf("the press opened %q", got)
	}
}

// ── THE GOLDENS ─────────────────────────────────────────────────────────────
//
// Two widths, written out whole. They are here so that a change to the page's
// grammar — a glyph, a lead, a gap, an order — has to be made on purpose: the
// diff of one of these is the change, stated.

// squeezeRows collapses the right-alignment padding so the golden states the
// grammar — glyphs, order, words, tails — without every terminal width baked
// into it as a run of spaces.
func squeezeRows(page string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(page, "\n"), "\n") {
		for strings.Contains(line, "   ") {
			line = strings.ReplaceAll(line, "   ", "  ")
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	return strings.Join(out, "\n")
}

func TestTheGraphsGoldenAtTheWideTier(t *testing.T) {
	snap := orchRun4()
	snap.Notes = []string{"the client matters more than the RFCs"}
	a, _ := orchApp(t, snap)

	want := strings.Join([]string{
		"work",
		"  ● plan decide what to read  $0.02",
		"  ◐ rfcs read the three RFCs  needs plan · $0.11",
		"  ○ client read our client  needs plan",
		"  ○ write write the answer  needs rfcs client",
		"",
		"planner",
		"· the client matters more than the RFCs",
	}, "\n")
	if got := squeezeRows(roomText(a)); got != want {
		t.Fatalf("the wide graph changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestTheGraphsGoldenAtThePhoneTier(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.width, a.height = 44, 40
	a.touch()

	want := strings.Join([]string{
		"work",
		"  ● plan  $0.02",
		"  decide what to read",
		"  ↳ needs: —",
		"  ◐ rfcs  $0.11",
		"  read the three RFCs",
		"  ↳ needs: plan",
		"  ○ client",
		"  read our client",
		"  ↳ needs: plan",
		"  ○ write",
		"  write the answer",
		"  ↳ needs: rfcs, client",
	}, "\n")
	if got := squeezeRows(roomText(a)); got != want {
		t.Fatalf("the phone graph changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A FINISHED RUN PUTS ITS ANSWER ON THE PAGE, under the graph that reached it:
// the claim, and the chips it cites still above it.
func TestAFinishedRunDrawsItsSynthesis(t *testing.T) {
	snap := orchRun4()
	snap.Done = true
	snap.Answer = "Retries are capped at three (rfcs), and our client ignores the cap (client)."
	for i := range snap.Nodes {
		snap.Nodes[i].State = orchestrate.Done
	}
	a, _ := orchApp(t, snap)

	page := roomText(a)
	for _, want := range []string{orchAnswerHead, "Retries are capped at three (rfcs)"} {
		if !strings.Contains(page, want) {
			t.Fatalf("a finished run never says %q:\n%s", want, page)
		}
	}
	if !strings.Contains(plain(a.roomHead(a.width)), orchDoneWord) {
		t.Fatalf("the header does not say the run is done:\n%q", plain(a.roomHead(a.width)))
	}
	// The graph is still above it: an answer with no shape under it is a report,
	// and this page is a place.
	if strings.Index(page, "plan") > strings.Index(page, orchAnswerHead) {
		t.Fatalf("the answer is drawn above the graph that reached it:\n%s", page)
	}
}

// A RUN THIS SESSION CANNOT SEE SAYS SO, and it still draws what the LANE gave
// it: the notes and the gate are what a page has before its first poll lands.
func TestAPageWithNoSnapshotStillSaysWhatTheLaneSaid(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.openOrchRoom("nobody", "")
	a.touch()
	if !strings.Contains(roomText(a), orchUnknownWord) {
		t.Fatalf("a page on an unknown run says nothing about it:\n%s", roomText(a))
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventOrchestrateFuel, Text: "$1.60 of $2.00",
	}})
	if !strings.Contains(plain(a.roomHead(a.width)), "$1.60 of $2.00") {
		t.Fatalf("the lane's own gauge is not on the header:\n%q", plain(a.roomHead(a.width)))
	}
}

// A SESSION WITH NO ORCHESTRATOR UNDER IT KEEPS EVERYTHING ELSE and says so in
// one note — the build guard room.go states for its own doors.
func TestASessionWithNoRunDoorsOpensNoPage(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.openOrchRoom("r1", "whatever")
	if a.room != nil {
		t.Fatal("a page opened on a session with no orchestrator")
	}
	// The note wraps at the test frame's width, so the assertion is on the head
	// of the sentence rather than on where the wrap fell.
	if !strings.Contains(plain(strings.Join(plainRows(a), "\n")), "adaptive runs unavailable") {
		t.Fatalf("the surface said nothing about the missing doors:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// ── the planner's model ─────────────────────────────────────────────────────

// THE PLANNER IS NAMED BESIDE THE GAUGE, because the two are one fact: the tank
// is being spent by a judgement, and with a tier configured that judgement is a
// model that appears nowhere else in the conversation.
func TestTheRunsHeaderNamesThePlannerModel(t *testing.T) {
	snap := orchRun4()
	snap.Planner = "moonshot/kimi-k3"
	a, _ := orchApp(t, snap)

	head := plain(a.roomHead(a.width))
	if !strings.Contains(head, "planner: moonshot/kimi-k3 · $0.87 / $2.00") {
		t.Fatalf("the header does not name the planner beside the gauge:\n%q", head)
	}
	// The page does not say it twice: at every tier but the phone the header is
	// where it lives.
	if strings.Contains(roomText(a), orchPlannerLead) {
		t.Fatalf("the planner is drawn on the page as well as the header:\n%s", roomText(a))
	}
}

// AND A RUN NOBODY NAMED A PLANNER FOR DROPS THE SEGMENT — the emptiness law:
// a label with nothing behind it is worse than the width it costs.
func TestARunWithNoPlannerModelDrawsNoSegment(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	head := plain(a.roomHead(a.width))
	if strings.Contains(head, orchPlannerLead) {
		t.Fatalf("the header invented a planner:\n%q", head)
	}
	if !strings.Contains(head, "$0.87 / $2.00") {
		t.Fatalf("the gauge went with it:\n%q", head)
	}
}

// AT THE PHONE TIER THE SEGMENT MOVES ONTO THE PAGE, dim, above the chips. It
// is moved and not dropped: the header sheds the GOAL first, which is a sentence
// still readable in the conversation, and never the model spending the money.
func TestThePhoneDrawsThePlannerOnThePageInstead(t *testing.T) {
	snap := orchRun4()
	snap.Planner = "moonshot/kimi-k3"
	a, _ := orchApp(t, snap)
	a.width, a.height = 40, 30
	a.room.dirty = true
	a.touch()

	head := plain(a.roomHead(a.width))
	if strings.Contains(head, orchPlannerLead) {
		t.Fatalf("the phone header kept the planner segment:\n%q", head)
	}
	if !strings.Contains(head, "$0.87 / $2.00") {
		t.Fatalf("the phone header dropped the gauge:\n%q", head)
	}
	lines := orchLines(a)
	if len(lines) == 0 || !strings.Contains(lines[0], "planner: moonshot/kimi-k3") {
		t.Fatalf("the planner is not the first row of the phone's page:\n%s", strings.Join(lines, "\n"))
	}
}

// THE OFFER SCALES WITH THE TANK, AND THE CARD SAYS HOW IT IS ANSWERED. A
// ten-dollar run that hit its cap is offered five more — half the decision the
// person already made, never a fixed dollar — and under the three rows sits the
// hint that names the gestures, because a card that takes no letter keys and
// suspends nothing has to say so itself: a person once read the quiet rows as
// prose and started typing an answer.
func TestTheGateOfferScalesAndNamesItsGestures(t *testing.T) {
	snap := orchRun4()
	snap.Paused = true
	snap.Fuel = orchestrate.Fuel{Cap: 10, Spent: 10.04}
	a, _ := orchApp(t, snap)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventOrchestratePause, ID: 1, Text: "$10.04 of $10.00"}})

	run := a.orchOf()
	if run == nil || run.gate == nil {
		t.Fatal("the pause raised no gate")
	}
	if got := run.gateAnswers()[0]; got != "topup:5" {
		t.Fatalf("a $10 tank offers %q, want topup:5 — half the cap", got)
	}
	page := roomText(a)
	for _, want := range []string{"add $5", "enter answers", "steer the planner"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the gate never says %q:\n%s", want, page)
		}
	}
	// The cursor opens on the scaled offer, and enter takes it as spelled.
	if got := run.pick; got.answer != "topup:5" {
		t.Fatalf("the cursor is on %+v, want the scaled top-up", got)
	}
}

// A TOOL CALL ON A NODE'S TRANSCRIPT OPENS ON A CLICK, exactly as it does in a
// room. The transcript's rows used to be re-minted as bare text — the deck
// renderer's hit information thrown away — so a person inside a node saw calls
// they could not open however they pressed. The rows go on the page whole now,
// and the expansion doors act on the transcript's own deck.
func TestANodeTranscriptsToolCallsOpenOnAClick(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	path := filepath.Join(t.TempDir(), "write.jsonl")
	journal := strings.Join([]string{
		`{"type":"message","role":"user","content":"write the answer"}`,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"retry\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"the grep found three hits"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}
	agent.journals = map[string]string{"r1:write": path}
	a.orchOpenTranscript("write")
	a.touch()

	rows := a.roomRows(a.bodyWidth())
	at := -1
	for i, r := range rows {
		if r.hit == hitTool {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no clickable tool row on the transcript:\n%s", roomText(a))
	}
	if strings.Contains(roomText(a), "the grep found three hits") {
		t.Fatalf("the call is already open before anybody clicked:\n%s", roomText(a))
	}
	y := roomRowY(a, at)
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if !strings.Contains(roomText(a), "the grep found three hits") {
		t.Fatalf("the click did not open the call:\n%s", roomText(a))
	}

	// And a re-read of a growing journal keeps it open: the fresh parse
	// carries the expansion across by index.
	grown := journal + "\n" + `{"type":"message","role":"assistant","content":"done."}`
	if err := os.WriteFile(path, []byte(grown), 0o644); err != nil {
		t.Fatal(err)
	}
	a.orchReadTranscript()
	a.touch()
	if !strings.Contains(roomText(a), "the grep found three hits") {
		t.Fatalf("the journal growing snapped the expansion shut:\n%s", roomText(a))
	}
}
