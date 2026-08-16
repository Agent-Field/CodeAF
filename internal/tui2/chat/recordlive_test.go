package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The user-review wave, item by item: the doors, the previews, the padding, the
// seam, the creation gap and the clock.

// statusBoard is the exact shape the reader photographed: one planned job whose
// PLANNER narrated its progress five times, and three parts under it. Every one
// of those five rows is delivery-SHAPED by columns, which is why they all used
// to arrive dressed as commitment cards.
func statusBoard() *boardBackend {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-6", Title: "cooling stocks", Status: store.Running,
			CreatedSeq: 60, UpdatedSeq: 60,
			Brief: "Rank the twenty best names in the sector and write the report."},
		store.Node{ID: "job-6/a", Parent: "job-6", Title: "Candidate summaries",
			Status: store.Done, CreatedSeq: 61, UpdatedSeq: 62, Summary: "eleven candidates."},
		store.Node{ID: "job-6/b", Parent: "job-6", Title: "Compile Cooling Data",
			Status: store.Running, CreatedSeq: 62, UpdatedSeq: 63},
		store.Node{ID: "job-6/c", Parent: "job-6", Title: "Write full report",
			Status: store.Pending, CreatedSeq: 63, UpdatedSeq: 63},
	)
	rows := []store.Message{}
	for i, body := range []string{
		"reading the request",
		"exploring approaches · 1 of 3",
		"exploring approaches · 3 of 3",
		"setting working standards · 3 of 4",
		"setting working standards · 4 of 4",
	} {
		rows = append(rows, store.Message{
			Seq: int64(70 + i), SessionID: testSession, Role: store.RoleSystem,
			NodeID: "job-6", Body: body,
		})
	}
	backend.node["job-6"] = rows
	return backend
}

// EXACTLY THREE DRESSED ELEMENTS PER RECORD PAGE (user review, 2026-08-11):
// one top rule, one living tree, one result card — and the planner's five status
// rows are ONE line, the newest, and never five cards.
//
// THE FIRST OF THE THREE CHANGED DRESS (amends §5's "commitment ground"). It was
// a commitment CARD until this wave, and the ground was a claim it could not
// make: §4 spends that plane on "made and still running", which a settled record
// may not say, and the page wore §4's one ground twice while saying the job's
// name and its whole receipt in both halves. §16's word-in-line seam says the
// same facts in one row with no ground at all — so a COMMITMENT on this page is
// now a defect and the count below is zero rather than one.
func TestARecordPageDrawsOneRuleOneTreeAndOneStatusLine(t *testing.T) {
	app := newTestApp(statusBoard(), &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-6", "cooling stocks")

	commitments, deliveries, rules, trees, statuses := 0, 0, 0, 0, 0
	for i := 0; i < app.view.transcript.Len(); i++ {
		switch block := app.view.transcript.Block(i).(type) {
		case *messageBlock:
			switch block.card {
			case dressCommitment:
				commitments++
			case dressDelivery:
				deliveries++
			}
		case *ruleBlock:
			rules++
		case *recordTreeBlock:
			trees++
		case *recordStatusBlock:
			statuses++
		}
	}
	if rules != 1 || trees != 1 || statuses != 1 {
		t.Fatalf("the page drew %d rules, %d trees and %d status lines, want one of each:\n%s",
			rules, trees, statuses, ansi.Strip(app.Frame(120, 40)))
	}
	if commitments != 0 {
		t.Fatalf("the page wore the commitment ground %d times — a settled record may not claim it:\n%s",
			commitments, ansi.Strip(app.Frame(120, 40)))
	}
	if deliveries != 0 {
		t.Fatalf("a running job drew %d delivery cards", deliveries)
	}
	// The rule is the page's FIRST row and it carries the job's name — once.
	rule, ok := app.view.transcript.Block(0).(*ruleBlock)
	if !ok || rule.rule.Title != "cooling stocks" {
		t.Fatalf("the page does not open on a rule titled for its job: %#v",
			app.view.transcript.Block(0))
	}
	// NO STATE GLYPH ON THE BOUNDARY (§15's delete test): the tree says what each
	// part is doing and the result card says how it ended, so a lifecycle here
	// would be the same fact in a third place — claimed, on a rule, for both
	// halves of what it separates.
	head := ansi.Strip(rule.Rows(120)[0])
	for _, glyph := range []string{
		tokens.GlyphQueued, tokens.GlyphWorking, tokens.GlyphSettled, tokens.GlyphFailed,
	} {
		if strings.Contains(head, glyph) {
			t.Fatalf("the top rule wears the state glyph %q: %q", glyph, head)
		}
	}

	// The PAGE's own rows, not the frame: the rail draws the same job's name one
	// column over, and a law about this page's blocks is asserted on them.
	page := recordRows(t, app, 120)
	if !strings.Contains(page, "setting working standards · 4 of 4") {
		t.Fatalf("the page does not carry the newest status:\n%s", page)
	}
	for _, gone := range []string{"reading the request", "1 of 3", "3 of 3", "3 of 4"} {
		if strings.Contains(page, gone) {
			t.Fatalf("a superseded status is still on the page (%q):\n%s", gone, page)
		}
	}
	// ZERO REPEATED TITLES. The job's name belongs to its one card.
	if n := strings.Count(page, "cooling stocks"); n != 1 {
		t.Fatalf("the job's title is drawn %d times, want one:\n%s", n, page)
	}
	// And the tree has one row per part.
	tree, ok := recordTree(app)
	if !ok || len(tree.rows) != 3 {
		t.Fatalf("the tree does not hold the three parts: %+v", tree)
	}
}

// A machinery row is machinery on every surface: status, ruler and flag rows
// never take a card's dress on a record page.
func TestMachineryRowsNeverWearACardOnTheRecordPage(t *testing.T) {
	backend := statusBoard()
	backend.node["job-6"] = append(backend.node["job-6"],
		store.Message{Seq: 80, SessionID: testSession, Role: store.RoleAgent,
			NodeID: "job-6/b", Body: "ruler: median 19 turns, 5 of 23 overran"},
		store.Message{Seq: 81, SessionID: testSession, Role: store.RoleUser,
			NodeID: "job-6", Body: "focus on the top five"},
	)
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-6", "cooling stocks")

	for i := 1; i < app.view.transcript.Len(); i++ {
		block, ok := app.view.transcript.Block(i).(*messageBlock)
		if ok && block.card != dressNone {
			t.Fatalf("block %d wears a card dress under the page's own card: %q",
				i, block.ID())
		}
	}
	// THE READER'S OWN SENTENCE IS NOT MACHINERY and survives as speech (§1's
	// three sentence classes).
	if !strings.Contains(recordRows(t, app, 120), "focus on the top five") {
		t.Fatal("the record dropped the reader's own steer")
	}
}

// THE PROMPT CARD OPENS MORE OF THE ASK ON THE RECORD PAGE than it does in the
// conversation: "the top card can have a lot more lines before view more as
// tasks are pretty large".
func TestTheRecordsPromptCardOpensMoreOfTheAsk(t *testing.T) {
	long := "Bring the wisp browser to parity with the reference. " +
		strings.Repeat("Every front matters and each one has its own acceptance. ", 8)
	backend := atomicBoard()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-4" {
			backend.nodes[i].Brief = long
		}
	}
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-4", "haiku")

	charge, ok := app.view.transcript.Block(1).(*messageBlock)
	if !ok || charge.ID() != chargeAskID {
		t.Fatalf("the page does not carry its ask under the rule: %T", app.view.transcript.Block(1))
	}
	rows := len(charge.Rows(80))
	if rows < 8 {
		t.Fatalf("the record's prompt card shows only %d rows of a long ask:\n%s",
			rows, strings.Join(charge.Rows(80), "\n"))
	}
	// The budget is a CAP and not a licence: a very long ask still folds.
	huge := strings.Repeat("Another paragraph of the ask that nobody wants inline. ", 60)
	backend2 := atomicBoard()
	for i := range backend2.nodes {
		if backend2.nodes[i].ID == "job-4" {
			backend2.nodes[i].Brief = huge
		}
	}
	app2 := newTestApp(backend2, &fakeCommander{}, nil)
	poll(t, app2)
	openRecord(t, app2, "job-4", "haiku")
	capped, _ := app2.view.transcript.Block(1).(*messageBlock)
	if capped == nil || !capped.collapsible {
		t.Fatal("a very long ask did not fold at all")
	}
	if got := len(capped.Rows(80)); got > 2*recordPromptRows {
		t.Fatalf("the prompt card grew to %d rows despite its budget", got)
	}
}

// -- the chat card's live preview ------------------------------------------------

// A RUNNING PART ON THE CHAT CARD CARRIES THE ONE LINE ITS WORKER LAST WROTE,
// and it FOLLOWS the recorder — "no 1 line update as its running".
func TestTheChatCardShowsAndFollowsItsRunningPartsRecorder(t *testing.T) {
	backend := statusBoard()
	// The commissioning row the card is born from, and the task it named.
	backend.nodes = append(backend.nodes, store.Node{
		ID: "task-901", Title: "cooling stocks", Status: store.Running,
		CreatedSeq: 90, UpdatedSeq: 90,
		StartedAt: fixedNow().Add(-30 * time.Second)})
	backend.nodes = append(backend.nodes, store.Node{
		ID: "task-901/x", Parent: "task-901", Title: "Compile", Status: store.Running,
		CreatedSeq: 91, UpdatedSeq: 91})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 901, Body: "Here's my reading: rank the cooling stocks."})

	first := "── turn 1  finish=tool_calls ──\ntext: pulling the price history.\n"
	commander := &tracingCommander{traces: map[string]string{"task-901/x": first}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)

	card := liveCardOf(t, app, "task-901")
	if len(card.parts) == 0 {
		t.Fatalf("the card has no parts to preview: %+v", card.parts)
	}
	// The read is armed because the thread is the visible lens and a card is
	// live; a window with neither pays nothing.
	cmd := app.readCardTracesCmd()
	if cmd == nil {
		t.Fatal("a live card armed no recorder read")
	}
	msg, ok := cmd().(traceReadMsg)
	if !ok || !msg.cards {
		t.Fatalf("the card read produced %T", cmd())
	}
	app.applyTraceRead(msg)

	before := previewOf(t, card, "task-901/x")
	if !strings.Contains(before, "pulling the price history") {
		t.Fatalf("the running part previews nothing: %q", before)
	}
	if !strings.Contains(ansi.Strip(strings.Join(card.Rows(90), "\n")), before) {
		t.Fatalf("the preview is not on the card:\n%s", strings.Join(card.Rows(90), "\n"))
	}

	// THE RECORDER GROWS AND THE LINE FOLLOWS IT. No journal move is involved:
	// a recorder append writes nothing a poll can see.
	commander.traces["task-901/x"] = first + "  → 40B: ok\ntext: writing the ranking.\n"
	msg, _ = app.readCardTracesCmd()().(traceReadMsg)
	app.applyTraceRead(msg)
	after := previewOf(t, card, "task-901/x")
	if after == before {
		t.Fatalf("the preview did not follow the recorder: still %q", before)
	}
	if !strings.Contains(after, "writing the ranking") {
		t.Fatalf("the preview is not the latest human line: %q", after)
	}

	// BATTERY LAW: a reader who walked into a room pays nothing for the cards
	// they are no longer looking at.
	app.view = &mainView{kind: viewNode, node: "job-6"}
	if app.readCardTracesCmd() != nil {
		t.Fatal("a window off the thread lens still read the cards' recorders")
	}
}

// A PREVIEW IS ONLY EVER UNDER A RUNNING PART.
func TestOnlyARunningPartOfAChatCardWearsAPreview(t *testing.T) {
	block := &messageBlock{}
	block.parts = []jobPart{
		{Node: "a", Name: "Done", Life: rail.LifeSettled, Preview: "must not draw"},
		{Node: "b", Name: "Live", Life: rail.LifeWorking, Preview: "reading spec.md"},
		{Node: "c", Name: "Queued", Life: rail.LifeQueued, Preview: "nor this"},
	}
	rows := block.partRows(nil, 80, blocks.Plain)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "reading spec.md") {
		t.Fatalf("the running part shows no preview:\n%s", joined)
	}
	for _, gone := range []string{"must not draw", "nor this"} {
		if strings.Contains(joined, gone) {
			t.Fatalf("a part that is not running drew a preview (%q):\n%s", gone, joined)
		}
	}
	if len(rows) != 4 {
		t.Fatalf("the card drew %d rows for three parts and one preview:\n%s", len(rows), joined)
	}
	// One line, ellipsized at the row's measure, never wrapped.
	long := block.partRows(nil, 40, blocks.Plain)
	if len(long) != 4 {
		t.Fatalf("a narrow card grew the preview to more rows:\n%s", strings.Join(long, "\n"))
	}
	for _, row := range long {
		if blocks.Width(row) > 40 {
			t.Fatalf("a row overran the measure: %q", row)
		}
	}
}

// liveCardOf finds the conversation's card for one job.
func liveCardOf(t *testing.T, app *App, job string) *messageBlock {
	t.Helper()
	for i := 0; i < app.transcript.Len(); i++ {
		block, ok := app.transcript.Block(i).(*messageBlock)
		if ok && block.card != dressNone && block.job == job {
			return block
		}
	}
	t.Fatalf("the conversation holds no card for %q", job)
	return nil
}

func previewOf(t *testing.T, card *messageBlock, node string) string {
	t.Helper()
	for _, part := range card.parts {
		if part.Node == node {
			return part.Preview
		}
	}
	t.Fatalf("the card has no part %q: %+v", node, card.parts)
	return ""
}

// -- the card's own geometry -----------------------------------------------------

// THE CARD HAS AIR INSIDE ITS GROUND, on all four sides: "the title and inside
// item does not seem to have proper padding in this card".
func TestTheCardsGroundCarriesItsOwnPadding(t *testing.T) {
	app, _ := deliveredApp(t, tokens.TrueColor)
	card := lastCard(t, app)
	rows := card.Rows(90)
	if !card.grounded() {
		t.Fatal("a truecolor card drew no ground, so this test proves nothing")
	}
	// One padding row as the ground opens and one before it closes (§16's
	// PADDING RHYTHM), inside the plane and carrying the card's own edge.
	first, last := ansi.Strip(rows[0]), ansi.Strip(rows[len(rows)-2])
	if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), blocks.AccentEdge)) != "" {
		t.Fatalf("the card's first row is not its opening pad: %q", first)
	}
	if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(last), blocks.AccentEdge)) != "" {
		t.Fatalf("the card's last row is not its closing pad: %q", last)
	}
	// And every row keeps a cell of ground to the right of its longest word, so
	// a receipt never ends at the plane's own corner (§19).
	for _, row := range rows[:len(rows)-1] {
		plain := ansi.Strip(row)
		if blocks.Width(plain) != 90 {
			t.Fatalf("a card row is %d cells wide, not the plane's 90: %q",
				blocks.Width(plain), plain)
		}
		if strings.TrimRight(plain, " ") != plain && !strings.HasSuffix(plain, "  ") {
			t.Fatalf("a card row has no air at its right edge: %q", plain)
		}
	}
}

// THE SEAM IS THE SAME QUIET MARK AT EVERY PROFILE, which the ground shade it
// replaced never was — at 16 colours and at NoColor no rung is drawn at all, so
// the old seam simply vanished.
func TestTheCardsSeamDegradesToTheSameMark(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI16, tokens.NoColor} {
		app, _ := deliveredApp(t, profile)
		card := lastCard(t, app)
		// The reading is what puts a seam on a card: it separates what was asked
		// from what came of it, so a card with no ask has no boundary to draw.
		card.adoptPrompt("Here's my reading: audit the poll loop for wasted wakeups.")
		rows := card.Rows(90)
		if card.seamLine < 0 || card.seamLine >= len(rows) {
			t.Fatalf("%v: the card drew no seam", profile)
		}
		seam := ansi.Strip(rows[card.seamLine])
		if !strings.Contains(seam, tokens.GlyphTreeDash+tokens.GlyphTreeDash) {
			t.Fatalf("%v: the seam is not a rule: %q", profile, seam)
		}
		if from := strings.Index(seam, tokens.GlyphTreeDash); blocks.Width(seam[:from]) < cardLane+bodyIndent {
			t.Fatalf("%v: the seam is not inset: %q", profile, seam)
		}
	}
}

// -- the clock ---------------------------------------------------------------------

// THE CARD'S CLOCK NEVER GOES BACKWARDS. The board measures an elapsed at its
// snapshot's instant and the poll folds it in later, so an unconditional
// re-latch restarted the count from a figure measured in the past — four times a
// second, which is exactly the jitter the reader reported ("the animations
// inside card seems to be not smooth or weird").
func TestTheCardsClockOnlyEverMovesForward(t *testing.T) {
	app, backend := commissionedApp(t)
	card := liveCardOf(t, app, "task-"+commissionCommand)
	clock := app.transcript.Clock()

	seen := time.Duration(0)
	for step := 0; step < 6; step++ {
		clock.Latch(fixedNow().Add(time.Duration(step) * 400 * time.Millisecond))
		shown, ok := card.liveElapsed()
		if !ok {
			t.Fatal("the card lost its clock")
		}
		if shown < seen {
			t.Fatalf("step %d: the clock went backwards, %s → %s", step, seen, shown)
		}
		seen = shown
		// A poll lands between frames, re-measuring the same elapsed the board
		// measured before — which is what the stale re-latch used to believe.
		backend.journal++
		poll(t, app)
	}
}

// ONE CLOCK DRIVES EVERY MOVING CELL IN A FRAME (8.1.3), so two cards show the
// same spinner frame and a repaint inside one step is byte-identical.
func TestEveryLiveCellOnAFrameSharesOneClock(t *testing.T) {
	app, _ := commissionedApp(t)
	clock := app.transcript.Clock()
	for i := 0; i < app.transcript.Len(); i++ {
		block, ok := app.transcript.Block(i).(*messageBlock)
		if !ok || block.clock == nil {
			continue
		}
		if block.clock != clock {
			t.Fatalf("block %q animates off a clock of its own", block.ID())
		}
	}
	// Its step is the house grid, so a card carrying a motion is counted in the
	// motion's own frames rather than in seconds (§18.1).
	// Latched on the grid, because floor(now/interval) is what the phase lock is
	// made of and a base that straddled a step would be testing the arithmetic
	// rather than the design.
	base := fixedNow().Truncate(blocks.DefaultInterval)
	clock.Latch(base)
	first := clock.Step()
	clock.Latch(base.Add(blocks.DefaultInterval / 3))
	if clock.Step() != first {
		t.Fatal("a repaint inside one house step produced a new frame")
	}
	clock.Latch(base.Add(blocks.DefaultInterval))
	if clock.Step() == first {
		t.Fatal("a whole house step did not advance the clock")
	}
}

// recordRows is the open record page's own rows, painted away — the document
// rather than the screen, so the rail beside it is not part of the assertion.
func recordRows(t *testing.T, app *App, width int) string {
	t.Helper()
	if app.view == nil || app.view.transcript == nil {
		t.Fatal("no record page is open")
	}
	var out []string
	for i := 0; i < app.view.transcript.Len(); i++ {
		out = append(out, app.view.transcript.Block(i).Rows(width)...)
	}
	return ansi.Strip(strings.Join(out, "\n"))
}

// -- the tick chain ------------------------------------------------------------

// clockedApp is a window whose `now` a test can move, so a frame can be asked
// for at two instants without any input event in between.
func clockedApp(backend Backend, now *time.Time) *App {
	return New(Options{
		Backend: backend, Commander: &fakeCommander{model: "anthropic/claude-k3"},
		Session: testSession, Profile: tokens.NoColor,
		Now:       func() time.Time { return *now },
		PollEvery: time.Millisecond,
		Root:      "/home/someone/aforge-v2", Home: "/home/someone",
	})
}

// THE TICK CHAIN, END TO END: becomes-live arms with NO input, stays-live
// re-arms itself, goes-idle disarms.
//
// THE DEFECT THIS PINS, in the reader's own words: "animation seems to happen
// only when I hover on something". The chain sustains itself once started, and
// it was started from three places — the window opening, a post landing, a
// stream batch arriving — every one of which is an INPUT. A job card becomes
// live on NONE of them: it arrives on a poll, and the poll's branch armed
// nothing. From then on the only frames were the ones some other event happened
// to repaint, which is a pointer moving.
func TestALiveCardArmsTheClockWithNoInputAndKeepsTicking(t *testing.T) {
	now := fixedNow()
	backend := board()
	backend.nodes = append(backend.nodes, store.Node{
		ID: "task-901", Title: "cooling stocks", Status: store.Running,
		CreatedSeq: 90, UpdatedSeq: 90, StartedAt: now.Add(-30 * time.Second)})
	backend.nodes = append(backend.nodes, store.Node{
		ID: "task-901/x", Parent: "task-901", Title: "Compile", Status: store.Running,
		CreatedSeq: 91, UpdatedSeq: 91})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 901, Body: "Here's my reading: rank the cooling stocks."})
	app := clockedApp(backend, &now)

	// BECOMES LIVE. The poll is driven through the REAL chain — the command the
	// runtime would have run, and the message it would have delivered — because
	// the defect was in which branch returned which command, and a test that
	// called applyPoll directly would have proved nothing about that.
	app.polling = true
	result, ok := app.pollCmd()().(pollResultMsg)
	if !ok {
		t.Fatal("the poll did not answer with a result")
	}
	if app.ticking {
		t.Fatal("the window was already ticking before anything was live")
	}
	if _, cmd := app.Update(result); cmd == nil {
		t.Fatal("the poll returned no command at all")
	}
	if !app.threadIsLive() {
		t.Fatal("a card with a running part does not read as live")
	}
	if !app.ticking {
		t.Fatal("a card that became live on a poll armed no animation clock")
	}

	// STAYS LIVE. Every tick schedules the next one, with no input anywhere.
	for step := 0; step < 4; step++ {
		now = now.Add(blocks.DefaultInterval)
		if _, cmd := app.Update(animTickMsg{}); cmd == nil {
			t.Fatalf("step %d: the tick did not re-arm the chain", step)
		}
		if !app.ticking {
			t.Fatalf("step %d: the chain disarmed itself while work was live", step)
		}
	}

	// AND THE FRAMES ACTUALLY MOVE. No key, no pointer, no journal move: only
	// the clock, which is the whole of what the reader could not see happening.
	now = fixedNow().Truncate(blocks.DefaultInterval)
	app.Update(animTickMsg{})
	first := ansi.Strip(app.Frame(100, 24))
	now = now.Add(3 * blocks.DefaultInterval)
	app.Update(animTickMsg{})
	second := ansi.Strip(app.Frame(100, 24))
	if first == second {
		t.Fatalf("three house steps changed nothing on screen:\n%s", first)
	}

	// GOES IDLE. Nothing running, nothing armed — the battery law's own half.
	for i := range backend.nodes {
		if strings.HasPrefix(backend.nodes[i].ID, "task-901") {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 99
		}
	}
	backend.journal++
	app.polling = true
	result, _ = app.pollCmd()().(pollResultMsg)
	app.Update(result)
	app.ticking = false
	if app.threadIsLive() {
		t.Fatal("a settled job still reads as live")
	}
	if cmd := app.startTick(); cmd != nil {
		t.Fatal("a window with nothing moving armed the animation clock")
	}
}

// THE SKELETON IS LIVE BEFORE ANY NODE EXISTS. Its only content is a breathing
// dot, so a liveness question that asked for a measured clock answered "nothing
// is moving" over the one card whose whole content was a motion — which is the
// creation gap the reader reported twice.
func TestTheSkeletonCardCountsAsLive(t *testing.T) {
	now := fixedNow()
	backend := board()
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 902, Body: "Here's my reading: something the graph has not caught up with."})
	app := clockedApp(backend, &now)

	app.polling = true
	result, _ := app.pollCmd()().(pollResultMsg)
	app.Update(result)

	card := liveCardOf(t, app, "task-902")
	if !card.provisional || !card.breathing {
		t.Fatalf("the skeleton is not breathing: %+v", card.phase)
	}
	if !app.threadIsLive() {
		t.Fatal("a card that is nothing but a motion does not read as live")
	}
	if !app.ticking {
		t.Fatal("the skeleton armed no animation clock")
	}
	// And the breathe actually breathes, with no input at all.
	now = fixedNow().Truncate(blocks.DefaultInterval)
	app.Update(animTickMsg{})
	app.Frame(100, 24)
	first := ansi.Strip(strings.Join(card.Rows(80), "\n"))
	now = now.Add(6 * blocks.DefaultInterval)
	app.Update(animTickMsg{})
	app.Frame(100, 24)
	second := ansi.Strip(strings.Join(card.Rows(80), "\n"))
	if first == second {
		t.Fatalf("the creating-task line did not move:\n%s", first)
	}
	if !strings.Contains(second, creatingWord) {
		t.Fatalf("the skeleton stopped saying what is happening:\n%s", second)
	}
}
