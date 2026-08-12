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

// -- the job block (§3): one block per job, evolving in place -----------------

// runningJobApp is [boardApp] with a clock on the running job: the fixture's
// nodes carry no start stamp, and a job block's receipt is the board's own
// elapsed continued — no measurement, no cell, which is §16's EMPTINESS and not
// something to fake.
func runningJobApp(t *testing.T) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].StartedAt = fixedNow().Add(-20 * time.Second)
		}
	}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

// jobStatus is one of the lines a running job posts about itself — the exact
// shape the reporter's own journal holds fifteen of, anchored to one node.
func jobStatus(backend *boardBackend, node, body string) store.Message {
	return backend.add(store.Message{
		SessionID: testSession, Role: store.RoleSystem, NodeID: node, Body: body,
	})
}

// THE DEFECT. "the thread shows two consecutive blocks, each repeating the job
// title line with one status line under it." §3 is one block per job, updating
// in place; measured on the reporter's journal, `task-9196` posted fifteen such
// rows and the thread drew fifteen blocks.
func TestConsecutiveStatusesForOneJobAreOneEvolvingBlock(t *testing.T) {
	app, backend := boardApp(t)
	before := app.transcript.Len()

	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	after := app.transcript.Len()
	if after != before+1 {
		t.Fatalf("the first status did not open a block: %d → %d", before, after)
	}
	opened := app.transcript.Block(after - 1).ID()

	jobStatus(backend, "job-1", "reading the issue")
	poll(t, app)
	if app.transcript.Len() != after {
		t.Fatalf("the second status minted a sibling block: %d → %d", after, app.transcript.Len())
	}

	// NEWEST SHOWN, SUPERSEDED DROPPED. The block says what is happening now,
	// once, under one title.
	rows := blockRows(t, app, app.transcript.Len()-1, 80)
	if !strings.Contains(rows, "reading the issue") {
		t.Fatalf("the block does not show the newest status:\n%s", rows)
	}
	if strings.Contains(rows, "preparing the repository") {
		t.Fatalf("a superseded status is still on screen:\n%s", rows)
	}
	if n := strings.Count(rows, "wisp-parity"); n != 1 {
		t.Fatalf("the job's name is stated %d times in one block:\n%s", n, rows)
	}
	// THE BLOCK KEPT ITS IDENTITY, which is what makes it the same block
	// evolving rather than a replacement wearing the same words: a fold the
	// reader opened, the copy chip's hover and the transcript's own anchor all
	// key on the id, and an id that moved would throw the reader's answer away
	// several times a minute (13.16).
	if got := app.transcript.Block(app.transcript.Len() - 1).ID(); got != opened {
		t.Fatalf("the job block was re-minted as %q, want the original %q", got, opened)
	}
}

// AND THE DELIVERY TAKES ITS PLACE. §3's last clause: "delivered: the block
// becomes the delivery card in place" — not a card under the machinery that
// preceded it.
func TestADeliveryReplacesTheJobsOwnBlock(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	jobStatus(backend, "job-1", "reading the issue")
	poll(t, app)
	at := app.transcript.Len()

	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		NodeID: "job-1", Body: "the parity work is done.\nNavCtx now matches the reference."})
	poll(t, app)
	if app.transcript.Len() != at {
		t.Fatalf("the delivery landed under the machinery instead of replacing it: %d → %d",
			at, app.transcript.Len())
	}
	rows := blockRows(t, app, app.transcript.Len()-1, 80)
	if !strings.Contains(rows, "the parity work is done") {
		t.Fatalf("the delivery card is not what stands there:\n%s", rows)
	}
	if strings.Contains(rows, "reading the issue") {
		t.Fatalf("the machinery survived under the delivery:\n%s", rows)
	}
}

// A SENTENCE BETWEEN THEM CHANGES NOTHING. The card belongs to the TASK, not to
// the tail: "It stays and I keep chatting", in the reader's own words. Two
// tasks running at once interleave their progress — the reporter's journal
// alternates two tasks' rows for pages — so a rule that only looked at the last
// block would mint a fresh card every time the other task spoke.
func TestAStatusAcrossOtherTalkStillUpdatesTheOneCard(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	at := app.transcript.Len()
	card := app.transcript.Block(at - 1).ID()

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "how is it going"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "still going"})
	jobStatus(backend, "job-1", "reading the issue")
	poll(t, app)

	// Two new rows of conversation, and NOT a third for the task.
	if got := app.transcript.Len(); got != at+2 {
		t.Fatalf("the task minted another block across a turn of talk: %d → %d", at, got)
	}
	index, ok := app.transcript.IndexOf(card)
	if !ok {
		t.Fatal("the task's card was re-minted rather than updated")
	}
	if index != at-1 {
		t.Fatalf("the card moved from %d to %d — it must stay at the position of the ask",
			at-1, index)
	}
	if rows := blockRows(t, app, index, 80); !strings.Contains(rows, "reading the issue") {
		t.Fatalf("the card did not take the newest status:\n%s", rows)
	}
}

// A CHILD'S OWN ROWS BELONG TO ITS TASK. Every node under a task — a part that
// finished, a ruler line, a splitting line, a flag — resolves to the task the
// reader commissioned and folds into its one card. In the reporter's
// screenshots each of these was a top-level block of its own.
func TestAChildsRowsFoldIntoItsTasksCard(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	at := app.transcript.Len()

	for _, line := range []string{
		"splitting the remaining work -- 1 pieces queued",
		"ruler: median 19 turns, 4 of 18 overran — the ruler holds",
	} {
		jobStatus(backend, "job-1/h2", line)
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		NodeID: "job-1", Body: "⚑ H2: the repository's own checks passed"})
	poll(t, app)

	if got := app.transcript.Len(); got != at {
		t.Fatalf("a child's rows minted %d top-level blocks", got-at)
	}
	// AND NONE OF THAT WORDING IS ANYWHERE IN THE CONVERSATION except as the
	// task's current status line — which is the one place §1 allows it.
	frame := ansi.Strip(app.Frame(120, 40))
	if strings.Contains(frame, "ruler: median") {
		t.Fatalf("a ruler line is rendering as content in the main thread:\n%s", frame)
	}
	if strings.Contains(frame, "splitting the remaining work") {
		t.Fatalf("a splitting line is rendering as content in the main thread:\n%s", frame)
	}
}

// A LEARNING MOMENT IS A VOICE AND KEEPS ITS OWN LINE — but it never wears a
// job's title and receipt as a header, which is what made a sentence about a
// workflow print "20 best stocks ranked · $0.27" above it.
func TestALearningMomentKeepsItsLineAndDropsTheJobsHeader(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	at := app.transcript.Len()

	jobStatus(backend, "job-1", "· learned — preflight the model id before launching a build")
	poll(t, app)
	if got := app.transcript.Len(); got != at+1 {
		t.Fatalf("a learning moment did not get its own line: %d → %d", at, got)
	}
	rows := blockRows(t, app, app.transcript.Len()-1, 80)
	if !strings.Contains(rows, "preflight the model id") {
		t.Fatalf("the learning moment lost its words:\n%s", rows)
	}
	if strings.Contains(rows, "wisp-parity") {
		t.Fatalf("a learning moment wore the job's name as a header:\n%s", rows)
	}
}

// TWO JOBS ARE TWO BLOCKS. The collapse is per job and never per row kind.
func TestTwoJobsKeepTwoBlocks(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	jobStatus(backend, "job-2", "reading the issue")
	poll(t, app)
	rows := blockRows(t, app, app.transcript.Len()-1, 80)
	if strings.Contains(rows, "preparing the repository") {
		t.Fatalf("one job's status collapsed into another job's block:\n%s", rows)
	}
}

// -- the job block's live clock (§11: numbers tick) ---------------------------

// SOMETHING MOVES WHILE WORK RUNS, AND IT IS THE NUMBER. §18.2 refuses the
// spinner to "an agent row, or anything durable", so a job block's motion is
// its clock: frame N and frame N+1 differ while the job runs, and are identical
// once it settles.
func TestAJobBlocksClockTicksWhileTheJobRuns(t *testing.T) {
	app, backend := runningJobApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)

	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the last block is not a job block")
	}
	if !block.working {
		t.Fatal("a running job's block does not read as working")
	}
	if !block.hasElapsed {
		t.Fatal("the job block carries no clock to tick")
	}

	clock := app.transcript.Clock()
	// Aligned to the house grid, because that is what the claim below is about:
	// two repaints inside ONE step are identical, and a base picked at random
	// inside a step would straddle the boundary a third of the time.
	base := fixedNow().Truncate(blocks.DefaultInterval)
	draw := func(at time.Time) string {
		clock.Latch(at)
		return strings.Join(block.Rows(80), "\n")
	}
	// PHASE-LOCKED, NOT FROZEN. A repaint landing inside ONE house step (§18.1's
	// 120ms) produces byte-identical rows, which is what makes a mid-step frame
	// cost zero dirty rows. It used to be safe to say "inside one second"
	// because the clock was the only thing moving on this block; a live subtree
	// twig now carries the spinner §18.2 gives it, and the spinner advances on
	// the grid.
	first := draw(base)
	if same := draw(base.Add(blocks.DefaultInterval / 3)); same != first {
		t.Fatalf("the row changed inside one house step — it is repainting for nothing:\n%s\n%s",
			first, same)
	}
	if later := draw(base.Add(9 * time.Second)); later == first {
		t.Fatalf("a running job's clock did not move in nine seconds:\n%s", first)
	}
	// The CARD's own glyph is exactly where it was: §18.2 refuses the spinner to
	// "anything durable", and a job block stands in the transcript for the life
	// of the job and beyond it. What may turn is a twig under it, which is work
	// in flight rather than a thing that exists.
	if cardGlyphOf(first) != cardGlyphOf(draw(base.Add(9*time.Second))) {
		t.Fatal("a durable job row animated its glyph")
	}

	// The block stays FINALIZED throughout. A live block here would put the
	// transcript's live seam above every row after it, and a detach would eat
	// the conversation below it.
	if !block.IsFinalized() {
		t.Fatal("the job block declared itself live and would swallow the rows after it")
	}
}

// A SETTLED JOB'S BLOCK STANDS STILL, and the window stops paying for it.
func TestASettledJobsBlockDoesNotTickAndDisarmsTheClock(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-2", "reading the issue") // job-2 is done in the fixture
	poll(t, app)

	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the last block is not a job block")
	}
	if block.working {
		t.Fatal("a settled job's block reads as working")
	}
	clock := app.transcript.Clock()
	base := fixedNow()
	clock.Latch(base)
	first := strings.Join(block.Rows(80), "\n")
	clock.Latch(base.Add(time.Hour))
	if later := strings.Join(block.Rows(80), "\n"); later != first {
		t.Fatalf("a settled job's row moved:\n%s\n%s", first, later)
	}
}

// THE WAKE-UP IS ARMED BY WORK AND BY NOTHING ELSE (the battery law).
func TestTheWindowTicksForRunningWorkAndNotForSettledWork(t *testing.T) {
	app, backend := runningJobApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	if app.turn.active {
		t.Fatal("the fixture has a live turn; the clock would arm for that instead")
	}
	if !app.threadIsLive() {
		t.Fatal("a thread holding a running job's block does not read as live")
	}
	if cmd := app.startTick(); cmd == nil {
		t.Fatal("the window did not arm for a running job")
	}
	app.ticking = false

	// The same window with the job finished: nothing moves, nothing wakes.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Status = store.Done
		}
	}
	backend.journal++
	poll(t, app)
	if app.threadIsLive() {
		t.Fatal("the thread kept reading as live after the job finished")
	}
}

// CALM FREEZES THE GLYPHS AND KEEPS THE CLOCK ARMED. A window that stopped
// waking would freeze its elapsed cells too, which is a job that has been
// running for four minutes saying `12s` forever.
func TestCalmStillArmsTheClockAndMarksItCalm(t *testing.T) {
	app, backend := runningJobApp(t)
	app.linear = true
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)

	cmd := app.startTick()
	if cmd == nil {
		t.Fatal("a calm window stopped waking, so every elapsed cell in it froze")
	}
	if !app.transcript.Clock().Calm {
		t.Fatal("the calm profile never reached the clock, so the glyphs kept moving")
	}
}

// blockRows renders one transcript block at a width, plain.
func blockRows(t *testing.T, app *App, index, width int) string {
	t.Helper()
	block := app.transcript.Block(index)
	if block == nil {
		t.Fatalf("no block at %d of %d", index, app.transcript.Len())
	}
	return strings.Join(block.Rows(width), "\n")
}

// glyphOf is the first printable rune of a rendering — the state cell.
func glyphOf(rows string) string {
	for _, line := range strings.Split(rows, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return string([]rune(trimmed)[0])
		}
	}
	return ""
}

// cardGlyphOf is the state cell of a CARD's title row — the first row that
// carries a glyph and a name, past the card's own leading air.
func cardGlyphOf(rows string) string {
	for _, line := range strings.Split(ansi.Strip(rows), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == blocks.AccentEdge {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, blocks.AccentEdge))
		return string([]rune(trimmed)[0])
	}
	return ""
}

// -- the whole lifecycle, replayed --------------------------------------------

// ONE TASK IS AT MOST TWO BLOCKS IN THE MAIN THREAD, EVER: the commitment card
// while it runs, and the delivery card when the WHOLE task settles.
//
// This replays the exact shape of the reporter's own journal for `task-9400` —
// a commissioning, per-child progress, a splitting line, a ruler line, flags,
// child completions, then the row-0 result — and asserts the count at every
// step. On the live build that sequence drew eight or more top-level blocks.
func TestAWholeTaskLifecycleIsAtMostTwoBlocks(t *testing.T) {
	app, backend := boardApp(t)
	before := app.transcript.Len()

	// The head hands the work over and says what it understood.
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 42, Body: "Here's my reading: rank the twenty best stocks to buy now."})
	poll(t, app)

	for _, row := range []struct{ node, body string }{
		{"job-1", "preparing the repository"},
		{"job-1/h2", "splitting the remaining work -- 1 pieces queued"},
		{"job-1/h2", "ruler: median 19 turns, 4 of 18 overran — the ruler holds"},
		{"job-1/keycutter", "Nuclear & Uranium Candidates: 8 candidates compiled"},
		{"job-1", "⚑ Candidate summaries: the repository's own checks passed"},
		{"job-1/h2", "Compile Cooling Stock Data — done"},
	} {
		jobStatus(backend, row.node, row.body)
	}
	poll(t, app)

	running := app.transcript.Len()
	if running > before+2 {
		t.Fatalf("a running task put %d blocks in the conversation, the law allows at most 2",
			running-before)
	}

	// The whole task settles, and its ending takes the card's place.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Status = store.Done
		}
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, NodeID: "job-1",
		Body: "Twenty tickers ranked, 7 BUY and 13 HOLD.\n\nEach carries a thesis and a verdict."})
	poll(t, app)

	if got := app.transcript.Len(); got != running {
		t.Fatalf("the delivery landed beside the card instead of in its place: %d → %d",
			running, got)
	}
	frame := ansi.Strip(app.Frame(120, 44))
	for _, never := range []string{
		"ruler: median", "splitting the remaining work", "⚑ Candidate summaries",
		"Compile Cooling Stock Data", "Nuclear & Uranium Candidates",
	} {
		if strings.Contains(frame, never) {
			t.Fatalf("node-level machinery is content in the main thread (%q):\n%s", never, frame)
		}
	}
	if !strings.Contains(frame, "Twenty tickers ranked") {
		t.Fatalf("the task's own result is not in the conversation:\n%s", frame)
	}
}

// THE DELIVERY CARD CARRIES THE WHOLE RECEIPT AND IS A DOOR. The reader asked
// for it by name: "the result has token cost, model etc., and I can click it to
// go into the task."
//
// The four cells are asserted at their source ([taskReceipt]) because one of
// them — the token count — comes from a ledger the board only reads for jobs
// somebody has OPENED (scope.go's readReceipts), so a fixture that has never
// entered a room cannot produce it. That gap is a wiring change and is reported
// as one; what is provable here is that the card spends every figure it is
// given and invents none of the ones it is not.
func TestTheDeliveryCardsReceiptIsTheWholeReceipt(t *testing.T) {
	facts := jobFacts{
		Name: "perf-audit", Life: rail.LifeSettled, Harness: "swe",
		Cost: 0.20, HasCost: true, Elapsed: 4 * time.Minute, HasElapsed: true,
	}
	full := receiptBoard{
		burned: 41_000,
		models: []string{"anthropic/claude-k3-20260801", "anthropic/claude-k3", "openai/gpt-6"},
	}
	got := taskReceipt(facts, full, "job-2")
	for _, want := range []string{"4m", "$0.20", "41K tok", "swe", "claude-k3", "gpt-6"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the receipt is missing %q: %q", want, got)
		}
	}
	// The two spellings of one model collapse to one word: a receipt that said
	// `k3 · k3` would be counting one model as two.
	if n := strings.Count(got, "claude-k3"); n != 1 {
		t.Fatalf("one model is named %d times: %q", n, got)
	}
	// THE HARNESS STANDS BESIDE THE MODEL, in v1's own order (`swe · kimi-k2`,
	// internal/tui/cards.go's cardChoiceReceipt): the figures are a column a
	// reader scans down and the two names close it.
	if want := "swe " + tokens.GlyphSeparator + " claude-k3"; !strings.Contains(got, want) {
		t.Fatalf("the harness does not stand beside the model: %q", got)
	}
	// A job with no chosen worker adds no cell and leaves no separator behind.
	generalist := facts
	generalist.Harness = ""
	if plain := taskReceipt(generalist, full, "job-2"); strings.Contains(plain, "swe") ||
		strings.Contains(plain, tokens.GlyphSeparator+" "+tokens.GlyphSeparator) {
		t.Fatalf("a generalist job left a worker cell behind: %q", plain)
	}

	// EVERY FIGURE ABSENT WHEN UNKNOWABLE, and money drawing its own absence.
	bare := taskReceipt(jobFacts{Name: "perf-audit"}, receiptBoard{}, "job-2")
	if strings.Contains(bare, "0.00") || strings.Contains(bare, "tok") {
		t.Fatalf("an unknowable receipt invented a figure: %q", bare)
	}
	if !strings.Contains(bare, tokens.GlyphSpend+tokens.GlyphMissing) {
		t.Fatalf("money did not draw its own absence: %q", bare)
	}
}

// AND THE CARD IS A DOOR ONTO ITS TASK.
func TestTheDeliveryCardIsADoorOntoItsTask(t *testing.T) {
	app, backend := receiptApp(t)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, NodeID: "job-2",
		Body: "The audit is done.\n\nThree hot paths, one fix each."})
	poll(t, app)

	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok || block.job != "job-2" {
		t.Fatalf("the card does not name the task it opens: %+v", block)
	}
	rows := blockRows(t, app, app.transcript.Len()-1, 120)
	for _, want := range []string{"perf-audit", "$0.20", "The audit is done"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the delivery card is missing %q:\n%s", want, rows)
		}
	}
}

// receiptBoard answers every optional read a receipt can ask for.
type receiptBoard struct {
	burned int
	models []string
}

func (b receiptBoard) jobFacts(string) (jobFacts, bool) { return jobFacts{}, false }
func (b receiptBoard) jobModels(string) []string        { return b.models }
func (b receiptBoard) jobTokens(string) (int, bool)     { return b.burned, b.burned > 0 }

// A FAILED TASK GETS THE SAME CARD IN CORAL, WITH THE REASON. A failure drawn
// green is the one mistake this row can make (§4's failed variant).
func TestAFailedTaskGetsTheSameCardInCoral(t *testing.T) {
	app, backend := boardApp(t)
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Status = store.Failed
		}
	}
	backend.journal++
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, NodeID: "job-1",
		Body: "did not finish — the provider dropped the stream"})
	poll(t, app)

	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the failure did not land as a card")
	}
	if block.head.GlyphHue != blocks.HueBroken {
		t.Fatalf("a failed task's card is not coral: hue %v", block.head.GlyphHue)
	}
	if rows := blockRows(t, app, app.transcript.Len()-1, 100); !strings.Contains(rows, "dropped the stream") {
		t.Fatalf("the card does not say why it failed:\n%s", rows)
	}
}

// receiptApp is [boardApp] with a settled job that has a clock, a bill, a token
// count and a model behind it — everything a receipt is made of.
func receiptApp(t *testing.T) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-2" {
			backend.nodes[i].StartedAt = fixedNow().Add(-4 * time.Minute)
			backend.nodes[i].FinishedAt = fixedNow()
		}
	}
	backend.usage["job-2"] = store.JobUsage{Runs: 1, Cost: 0.20}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

// -- the commitment card (§3, and the reader's first sentence) ----------------

// "I give a task … then I get back a card saying the task name and the actual
// prompt it is using — that's the row-0 parent task. It stays and I keep
// chatting."
//
// What it used to be is the second half of this test: `↦ commissioned: Here's
// my reading: …`, a dim chrome row wearing machinery vocabulary (§14) for the
// one line the reader is watching for.
func TestTheCommitmentIsACardWithTheTaskNameAndItsPrompt(t *testing.T) {
	app, _ := commissionedApp(t)

	rows := blockRows(t, app, app.transcript.Len()-1, 100)
	if !strings.Contains(rows, "wisp-parity") {
		t.Fatalf("the commitment card is not named by its task:\n%s", rows)
	}
	if !strings.Contains(rows, "Bring the wisp browser to parity") {
		t.Fatalf("the card does not carry the head's reading:\n%s", rows)
	}
	if strings.Contains(rows, "commissioned") {
		t.Fatalf("the card still wears the machinery word:\n%s", rows)
	}
	// THE MARK IS NOT THE READING. `Here's my reading:` is how the producer
	// introduces the sentence; what the card shows is the sentence (§14 — the
	// mark names a mechanism, and the card's own shape already says the head
	// read something).
	if strings.Contains(rows, readingMark) {
		t.Fatalf("the card is printing the producer's own mark:\n%s", rows)
	}
	// The reading stands on ONE line, with the door under it — in the ONE
	// disclosure grammar ([blocks.Disclose]), which is a mark, a count and a
	// unit and never a verb.
	if !strings.Contains(rows, blocks.Disclose(false, 1, "line", "lines")) {
		t.Fatalf("the card's reading has no door onto the rest of it:\n%s", rows)
	}
	block, _ := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !block.collapsible {
		t.Fatalf("the card's prompt line has no door onto the rest of the reading")
	}
	if !block.working {
		t.Fatal("the commitment card of a running task does not read as working")
	}
}

// AND IT IS THE SAME CARD FOR THE WHOLE LIFE OF THE TASK: the statuses land in
// it, the reading survives them, and the delivery finishes it in place.
func TestTheCommitmentCardCarriesItsPromptThroughToDelivery(t *testing.T) {
	app, backend := commissionedApp(t)
	at := app.transcript.Len()
	card := app.transcript.Block(at - 1).ID()

	// The child's telemetry first, the task's own progress after it: the card
	// shows the NEWEST status and both of them land inside it.
	jobStatus(backend, "task-"+commissionCommand+"/h2", "ruler: median 19 turns — the ruler holds")
	jobStatus(backend, "task-"+commissionCommand, "preparing the repository")
	poll(t, app)
	if got := app.transcript.Len(); got != at {
		t.Fatalf("two statuses minted %d blocks beside the commitment card", got-at)
	}
	if _, ok := app.transcript.IndexOf(card); !ok {
		t.Fatal("the commitment card was replaced rather than updated")
	}
	rows := blockRows(t, app, at-1, 100)
	if !strings.Contains(rows, "Bring the wisp browser to parity") {
		t.Fatalf("the reading was lost on the first status:\n%s", rows)
	}
	if !strings.Contains(rows, "preparing the repository") {
		t.Fatalf("the card does not show what is happening:\n%s", rows)
	}
	if strings.Contains(rows, "ruler: median") {
		t.Fatalf("a superseded status is still on the card:\n%s", rows)
	}
	if !strings.Contains(rows, "wisp-parity") {
		t.Fatalf("the card stopped wearing its task's name:\n%s", rows)
	}

	// It settles into the delivery card, in place, still carrying the reading.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "task-"+commissionCommand {
			backend.nodes[i].Status = store.Done
		}
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		NodeID: "task-" + commissionCommand,
		Body:   "NavCtx now matches the reference on all four fronts."})
	poll(t, app)
	if got := app.transcript.Len(); got != at {
		t.Fatalf("the delivery landed beside the card: %d → %d", at, got)
	}
	rows = blockRows(t, app, at-1, 100)
	for _, want := range []string{"wisp-parity", "Bring the wisp browser to parity", "NavCtx now matches"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the finished card is missing %q:\n%s", want, rows)
		}
	}
	if strings.Contains(rows, "preparing the repository") {
		t.Fatalf("a superseded status survived into the delivery card:\n%s", rows)
	}
}

// A COMMISSIONING WHOSE TASK THE BOARD CANNOT NAME YET IS THE CARD'S SKELETON
// (user-amended 2026-08-11).
//
// It is the same state the reader timed as dead air — the commissioning row is
// journaled the instant the head hands work over, and the task node it names
// lands one snapshot later — so what the thread shows in that interval is the
// frame of the card that is about to exist: the head's own reading as its title,
// the reading under it, and `creating task…` breathing. Never a fake: the row it
// is drawn from is in the journal.
func TestACommissioningWithNoKnownTaskDrawsTheCardsSkeleton(t *testing.T) {
	app, backend := boardApp(t)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 777, Body: "Here's my reading: something the board never heard of."})
	poll(t, app)
	at := app.transcript.Len() - 1
	block, ok := app.transcript.Block(at).(*messageBlock)
	if !ok {
		t.Fatalf("the commissioning is not a message block: %T", app.transcript.Block(at))
	}
	if block.card != dressCommitment || !block.provisional {
		t.Fatalf("the commissioning did not draw the card's skeleton: %+v", block.card)
	}
	rows := blockRows(t, app, at, 100)
	for _, want := range []string{"something the board never heard of", creatingWord} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the skeleton is missing %q:\n%s", want, rows)
		}
	}
	// NO RECEIPT AND NO SUBTREE. Neither is knowable yet, and §16's EMPTINESS
	// says an absent figure renders as absence rather than as a zero.
	if strings.Contains(rows, tokens.GlyphSpend) {
		t.Fatalf("the skeleton invented a receipt:\n%s", rows)
	}
	if len(block.parts) != 0 {
		t.Fatalf("the skeleton invented a subtree: %+v", block.parts)
	}

	// AND THE REAL CARD REPLACES IT IN PLACE. Same block, same id, so nothing
	// jumps at the one moment the reader is watching.
	backend.nodes = append(backend.nodes, store.Node{
		ID: "task-777", Title: "navctx rework", Status: store.Running,
		CreatedSeq: 90, UpdatedSeq: 90})
	backend.journal++
	poll(t, app)
	if got := app.transcript.Len() - 1; got != at {
		t.Fatalf("the named card landed beside the skeleton: %d → %d", at, got)
	}
	if same := app.transcript.Block(at); same != blocks.Block(block) {
		t.Fatalf("the named card is a different block: %T", same)
	}
	if block.provisional {
		t.Fatal("the card is still wearing its provisional title")
	}
	if rows := blockRows(t, app, at, 100); !strings.Contains(rows, "navctx rework") {
		t.Fatalf("the card did not take the task's name:\n%s", rows)
	}
}

// commissionedApp is a window whose running task has just been handed over, with
// the head's reading on the row that handed it. The command's own sequence is
// what names the task it spliced, so the fixture's node is renamed to match.
func commissionedApp(t *testing.T) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	// The node a splice at command N mints is `task-N`; the fixture spells that
	// convention out rather than relying on the renderer to guess it, which is
	// the same check [jobOf] makes against the board before it believes a link.
	backend.nodes = append(backend.nodes,
		store.Node{ID: "task-" + commissionCommand, Title: "wisp-parity", Status: store.Running,
			CreatedSeq: 40, UpdatedSeq: 40, StartedAt: fixedNow().Add(-20 * time.Second)},
		store.Node{ID: "task-" + commissionCommand + "/h2", Parent: "task-" + commissionCommand,
			Title: "H2", Status: store.Running, CreatedSeq: 41, UpdatedSeq: 41},
	)
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 4242,
		Body: "Here's my reading: Bring the wisp browser to parity with the reference " +
			"on four fronts, starting with NavCtx.\n\nEvery front gets its own check."})
	poll(t, app)
	return app, backend
}

// commissionCommand is the command the fixture's task was spliced at.
const commissionCommand = "4242"
