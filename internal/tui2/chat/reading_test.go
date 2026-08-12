package chat

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// modelBoard is [boardBackend] that can also say which models billed a job —
// the read a receipt's model words come from.
type modelBoard struct {
	*boardBackend
	models map[string][]string
}

func (m *modelBoard) NodeModels(id string) ([]string, error) { return m.models[id], nil }

// THE HEAD'S READING, ON THE CARD (§3, and the reader's own amendment: "we need
// to separate assumptions with proper bullet list or something … make sure we
// utilize all structures in UI").
//
// Three claims live here. The reading is shown ONCE — the reported defect was a
// card drawing its own one-line preview and then the whole text of it
// immediately underneath. What opens is STRUCTURED into the zones the producer
// already writes. And a body that does not answer the grammar is drawn as the
// plain paragraph it always was, never mangled into zones it does not have.

// theReading is exactly what internal/resident's compileReceipt writes, with
// internal/head/compiler.go's verbatim splice inside it.
const theReading = "Here's my reading: Deliver a working Python script that fits a stochastic " +
	"model to stock price data and reports its parameters.\n\n" +
	"Verbatim request:\nbuild me a stochastic stock fitter with ML\n" +
	"Assumed: daily close prices from a public source are acceptable\n" +
	"Assumed: a geometric Brownian motion baseline is the starting point\n" +
	"Correct me anytime — changing course costs nothing."

// readingApp is a window whose running task was just commissioned with one
// reading. The task carries whatever parts the caller asked for.
func readingApp(t *testing.T, body string, parts ...store.Node) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "task-" + commissionCommand, Title: "Stochastic stock fitter with ML",
			Status: store.Running, CreatedSeq: 40, UpdatedSeq: 40,
			StartedAt: fixedNow().Add(-2 * time.Minute)})
	backend.nodes = append(backend.nodes, parts...)
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 4242, Body: body})
	poll(t, app)
	return app, backend
}

// unstart backs one node out to the state it was in before the scheduler
// claimed it: pending, with no start stamp. It is the only way to reach the
// PLANNING state from a fixture whose task is already running, and it moves the
// journal so the next poll actually re-reads.
func unstart(backend *boardBackend, id string) {
	for i := range backend.nodes {
		if backend.nodes[i].ID == id {
			backend.nodes[i].Status = store.Pending
			backend.nodes[i].StartedAt = time.Time{}
		}
	}
	backend.journal++
}

// harnessed writes the splice's worker choice onto one node, which is where a
// job's harness lives for every node the splice admitted.
func harnessed(backend *boardBackend, id, worker string) {
	for i := range backend.nodes {
		if backend.nodes[i].ID == id {
			backend.nodes[i].Provenance.Subharness = worker
		}
	}
	backend.journal++
}

// part is one child of the fixture's task.
func part(id, title string, status store.Status, seq int64) store.Node {
	return store.Node{ID: "task-" + commissionCommand + "/" + id, Parent: "task-" + commissionCommand,
		Title: title, Status: status, CreatedSeq: seq, UpdatedSeq: seq}
}

// THE DEFECT: the ellipsized preview AND its own full text, on one card.
func TestTheReadingIsDrawnOnceAndNeverTwice(t *testing.T) {
	app, _ := readingApp(t, theReading)
	block := lastCard(t, app)

	shut := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if n := strings.Count(shut, "Deliver a working Python script"); n != 1 {
		t.Fatalf("the collapsed card states the reading %d times:\n%s", n, shut)
	}
	preview := rowWith(t, shut, "Deliver a working Python script")
	if !strings.HasSuffix(preview, tokens.GlyphEllipsis) {
		t.Fatalf("the collapsed preview is not clipped: %q", preview)
	}
	// And it IS a preview: the zones behind it are not on screen yet.
	if strings.Contains(shut, "daily close prices") {
		t.Fatalf("the collapsed card is showing what its door is holding:\n%s", shut)
	}

	block.SetExpanded(true)
	open := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if n := strings.Count(open, "Deliver a working Python script"); n != 1 {
		t.Fatalf("the opened card states the reading %d times:\n%s", n, open)
	}
	if strings.Contains(open, preview) {
		t.Fatalf("the opened card is still showing the clipped preview %q:\n%s", preview, open)
	}
	if !strings.Contains(open, "reports its parameters") {
		t.Fatalf("the opened card did not show the whole sentence:\n%s", open)
	}
}

// rowWith is the first row of a rendering that carries a phrase, trimmed.
func rowWith(t *testing.T, rows, phrase string) string {
	t.Helper()
	for _, row := range strings.Split(rows, "\n") {
		if strings.Contains(row, phrase) {
			return strings.TrimSpace(row)
		}
	}
	t.Fatalf("no row carries %q:\n%s", phrase, rows)
	return ""
}

// THE ZONES. The goal leads, the reader's own words are quoted in their own
// voice, each assumption is a bullet at the child indent, and the offer to be
// corrected is a dim hint at the card's foot.
func TestTheReadingsZonesAreDrawnAsZones(t *testing.T) {
	app, _ := readingApp(t, theReading)
	block := lastCard(t, app)
	block.SetExpanded(true)
	rows := strings.Split(ansi.Strip(strings.Join(block.Rows(90), "\n")), "\n")

	var goal, quote, bullets, hint int
	for i, row := range rows {
		trimmed := strings.TrimSpace(row)
		switch {
		case strings.Contains(row, "Deliver a working Python script"):
			goal = i
		case strings.Contains(row, "build me a stochastic stock fitter"):
			quote = i
			if !strings.Contains(row, tokens.GlyphProseQuote) {
				t.Fatalf("the reader's own words are not quoted in their own voice: %q", row)
			}
		case strings.HasPrefix(trimmed, tokens.GlyphSeparator+" ") &&
			(strings.Contains(row, "daily close prices") ||
				strings.Contains(row, "geometric Brownian")):
			bullets++
			if at := columnOf(row, tokens.GlyphSeparator); at != cardLane+bodyIndent+2*partIndent-2 {
				t.Fatalf("an assumption's bullet is at column %d, want the child indent at %d: %q",
					at, cardLane+bodyIndent+2*partIndent-2, row)
			}
		case strings.Contains(row, "Correct me anytime"):
			hint = i
		}
	}
	if goal == 0 {
		t.Fatalf("the goal is not the reading's lead:\n%s", strings.Join(rows, "\n"))
	}
	if bullets != 2 {
		t.Fatalf("the two assumptions became %d bullets:\n%s", bullets, strings.Join(rows, "\n"))
	}
	if !(goal < quote && quote < hint) {
		t.Fatalf("the zones are out of order — goal %d, quote %d, hint %d:\n%s",
			goal, quote, hint, strings.Join(rows, "\n"))
	}
	// The hint is the card's LAST word, under everything it is an offer about.
	if hint < len(rows)-3 {
		t.Fatalf("the offer to be corrected is not at the card's foot (row %d of %d):\n%s",
			hint, len(rows), strings.Join(rows, "\n"))
	}
	// The producer's own mark is machinery and never reaches a cell (§14).
	whole := strings.Join(rows, "\n")
	for _, mark := range []string{readingMark, verbatimMark, assumedMark} {
		if strings.Contains(whole, mark) {
			t.Fatalf("the producer's mark %q is on the card:\n%s", mark, whole)
		}
	}
}

// A BODY WITHOUT THE GRAMMAR IS NEVER MANGLED. It keeps the plain wrapped
// paragraph it always had, whole.
func TestAReadingWithoutTheGrammarStaysPlainText(t *testing.T) {
	const plain = "Here's my reading: rework the navigation context so the browser " +
		"restores its own state, and keep the existing tests green while doing it.\n\n" +
		"There is a second paragraph and it is not a zone."
	app, _ := readingApp(t, plain)
	block := lastCard(t, app)
	block.SetExpanded(true)
	open := ansi.Strip(strings.Join(block.Rows(90), "\n"))

	if !strings.Contains(open, "There is a second paragraph") {
		t.Fatalf("an unmatched reading lost half of itself:\n%s", open)
	}
	if strings.Contains(open, tokens.GlyphSeparator+" There is") {
		t.Fatalf("an unmatched reading was bulleted into zones it does not have:\n%s", open)
	}
	if got := parseReading(plain); got.matched {
		t.Fatalf("a body with no zones claimed the grammar: %+v", got)
	}
}

// parseReading's own unit, at the boundary: a mark with nothing behind it is
// not a grammar.
func TestAMarkWithNothingBehindItIsNotAGrammar(t *testing.T) {
	for _, body := range []string{
		"Here's my reading: just a sentence.",
		"nothing marked at all",
		"",
	} {
		if got := parseReading(body); got.matched {
			t.Errorf("parseReading(%q) claimed a grammar: %+v", body, got)
		}
	}
	got := parseReading(theReading)
	if !got.matched {
		t.Fatal("the producer's own shape did not match the grammar")
	}
	if len(got.assumed) != 2 {
		t.Fatalf("the assumptions came out as %q", got.assumed)
	}
	if !strings.Contains(got.verbatim, "stochastic stock fitter") {
		t.Fatalf("the verbatim block came out as %q", got.verbatim)
	}
	if !strings.HasPrefix(got.hint, amendMark) {
		t.Fatalf("the closing offer came out as %q", got.hint)
	}
	if strings.Contains(got.goal, readingMark) {
		t.Fatalf("the goal kept the mark that introduced it: %q", got.goal)
	}
}

// -- the birth phases (§3, and "we need to ensure we are giving all info") ----

// A TASK WITH NOTHING UNDER IT YET BREATHES `planning…`. §3 spells this one out
// and it had no producer at all before this lane: the card went from
// commissioned straight to a subtree with nothing in between.
//
// "NOTHING UNDER IT YET" IS TWO FACTS AND NOT ONE (2026-08-11): no parts, and
// the root's own row not yet claimed. The fixture's task runs, so this backs it
// out to the state the phase actually describes; the atom that RUNS with no
// parts is the test below.
func TestATaskWithNoPartsBreathesPlanning(t *testing.T) {
	app, backend := readingApp(t, theReading)
	unstart(backend, "task-"+commissionCommand)
	poll(t, app)
	block := lastCard(t, app)
	if block.phase != planningWord {
		t.Fatalf("a task with no parts says %q, want %q", block.phase, planningWord)
	}
	if !block.breathing {
		t.Fatal("the planning line does not breathe")
	}
	rows := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(rows, planningWord) {
		t.Fatalf("the phase line is not on the card:\n%s", rows)
	}

	// THE BREATHE IS §18.2's, PHASE-LOCKED TO THE HOUSE CLOCK: identical inside
	// one step, moved across a full period, and it is the MARK that moves and
	// not the words.
	clock := app.transcript.Clock()
	base := fixedNow().Truncate(blocks.DefaultInterval)
	draw := func(at time.Time) string {
		clock.Latch(at)
		return ansi.Strip(strings.Join(block.Rows(90), "\n"))
	}
	first := draw(base)
	if same := draw(base.Add(blocks.DefaultInterval / 3)); same != first {
		t.Fatalf("the breathe changed inside one house step:\n%s\n%s", first, same)
	}
	moved := false
	for step := 1; step <= blocks.PulseSteps; step++ {
		if draw(base.Add(time.Duration(step)*blocks.DefaultInterval)) != first {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatalf("the planning line never breathed across a full period:\n%s", first)
	}
}

// THE DEFECT (2026-08-11): an ATOMIC task breathed `planning…` for its whole
// life. A job that comes out as one leaf has no parts by construction, and the
// phase read only the parts — so the commonest job there is told the reader it
// was being planned while it was already working.
//
// What replaces it is silence plus a fact: the phase says nothing (§15 — the
// glyph carries the life, and there is no tree below to narrate) and the
// receipt names the harness the work runs on, in v1's own word.
func TestARunningAtomSaysWhoIsWorkingInsteadOfPlanning(t *testing.T) {
	app, backend := readingApp(t, theReading)
	harnessed(backend, "task-"+commissionCommand, "swe")
	poll(t, app)
	block := lastCard(t, app)

	if len(block.parts) != 0 {
		t.Fatalf("the fixture grew parts and is no longer an atom: %+v", block.parts)
	}
	if block.phase != "" {
		t.Fatalf("a running atom still claims a phase: %q", block.phase)
	}
	if block.breathing {
		t.Fatal("a running atom still breathes a phase it does not have")
	}
	rows := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if strings.Contains(rows, planningWord) {
		t.Fatalf("the card is still saying it is planning:\n%s", rows)
	}
	if !strings.Contains(rows, "swe") {
		t.Fatalf("the card never says which harness runs the work:\n%s", rows)
	}
	// THE WORD IS THE STORE'S AND THE GRAMMAR IS THE RECEIPT'S: the harness
	// stands in the telemetry row beside the money, not on a panel of its own.
	receipt := rowWith(t, rows, "swe")
	if !strings.Contains(receipt, tokens.GlyphSeparator) {
		t.Fatalf("the harness is not a receipt cell: %q", receipt)
	}
	// A generalist job says nothing at all — the silence is what makes the word
	// mean something on the job that has one (§16).
	plain, plainBackend := readingApp(t, theReading)
	plainBackend.journal++
	poll(t, plain)
	bare := ansi.Strip(strings.Join(lastCard(t, plain).Rows(90), "\n"))
	if strings.Contains(bare, "swe") {
		t.Fatalf("a job with no chosen worker invented one:\n%s", bare)
	}
}

// PARTS LANDING GROW THE TREE AND MOVE THE COUNT, between two snapshots and
// with no message written at all. §3 forbids the denominator: what the line says
// is how many exist SO FAR.
func TestPartsLandingGrowTheTreeAndTheCount(t *testing.T) {
	app, backend := readingApp(t, theReading)
	block := lastCard(t, app)

	backend.nodes = append(backend.nodes, part("a", "Fetch prices", store.Pending, 44))
	backend.journal++
	poll(t, app)
	one := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(one, settingUp) || !strings.Contains(one, "1 part so far") {
		t.Fatalf("the first part did not move the phase line:\n%s", one)
	}
	if !strings.Contains(one, "Fetch prices") {
		t.Fatalf("the first part is not a row of the tree:\n%s", one)
	}

	backend.nodes = append(backend.nodes, part("b", "Fit GBM", store.Pending, 45))
	backend.journal++
	poll(t, app)
	two := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(two, "2 parts so far") {
		t.Fatalf("the count did not move between snapshots:\n%s", two)
	}
	if !strings.Contains(two, "Fit GBM") {
		t.Fatalf("the second part did not join the tree:\n%s", two)
	}
	// NEVER A FRACTION (§3: "a dynamic graph cannot promise a denominator").
	if regexp.MustCompile(`\d+\s*/\s*\d+`).MatchString(two) {
		t.Fatalf("the phase line promised a denominator:\n%s", two)
	}
	// AND NEVER THE BANNED VOCABULARY (§14, §3's "NO 'N workers' anywhere").
	for _, banned := range []string{"worker", "node", "atomic", "seq"} {
		if strings.Contains(strings.ToLower(two), banned) {
			t.Fatalf("the card is speaking machinery (%q):\n%s", banned, two)
		}
	}
}

// THE PHASE LINE GOES WHEN WORK RUNS. The tree below it is saying the same
// thing, row by row, and a label doing structure's job is §15's own defect.
func TestThePhaseLineLeavesOnceWorkRuns(t *testing.T) {
	app, backend := readingApp(t, theReading,
		part("a", "Fetch prices", store.Pending, 44),
		part("b", "Fit GBM", store.Pending, 45))
	block := lastCard(t, app)
	if !strings.Contains(ansi.Strip(strings.Join(block.Rows(90), "\n")), settingUp) {
		t.Fatal("queued parts did not produce the setting-up phase")
	}

	for i := range backend.nodes {
		if backend.nodes[i].ID == "task-"+commissionCommand+"/a" {
			backend.nodes[i].Status = store.Running
		}
	}
	backend.journal++
	poll(t, app)

	running := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if block.phase != "" {
		t.Fatalf("the phase line survived the first running part: %q", block.phase)
	}
	if strings.Contains(running, settingUp) || strings.Contains(running, planningWord) {
		t.Fatalf("a phase word is still on a running card:\n%s", running)
	}
	if !strings.Contains(running, "Fetch prices") {
		t.Fatalf("the running part left the tree:\n%s", running)
	}
	// The spinner is legal on a part in flight and on nothing else here (§18.2).
	if !block.partsAlive() {
		t.Fatal("a running part does not read as alive, so its row cannot spin")
	}
}

// -- the live receipt ---------------------------------------------------------

// THE MONEY MOVES WITH THE SNAPSHOT. "the cost updates and runtime etc. should
// be running and realtime in the main chat" — and a card dressed once from a
// journal row read its bill once and then never again.
func TestTheCardsReceiptIsRereadAcrossSnapshots(t *testing.T) {
	app, backend := readingApp(t, theReading)
	block := lastCard(t, app)

	before := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(before, tokens.GlyphSpend+tokens.GlyphMissing) {
		t.Fatalf("an unbilled task did not draw its money as absent:\n%s", before)
	}
	version := block.Version()

	backend.usage["task-"+commissionCommand] = store.JobUsage{Runs: 1, Cost: 0.42}
	backend.journal++
	poll(t, app)

	after := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(after, "$0.42") {
		t.Fatalf("the bill landed and the card did not move:\n%s", after)
	}
	if block.Version() == version {
		t.Fatal("the receipt changed without the version moving, so the cache would hold the old bytes")
	}
	// A snapshot that changed nothing costs nothing: no version move, no repaint.
	steady := block.Version()
	backend.journal++
	poll(t, app)
	if block.Version() != steady {
		t.Fatal("a snapshot that changed nothing still bumped the card's version")
	}
}

// -- the slug scan ------------------------------------------------------------

// NO PROVIDER SLUG REACHES A TRANSCRIPT FRAME (§14's never-shown tier, and the
// reader who met `deepseek-v4-flash-latest · glm-5.2` in a job receipt).
//
// It is a SHAPE scan and not a list of names: what a reader recognizes as an
// identifier is the vendor slash, the moving-pointer suffix, the release stamp
// and the variant colon. A model word may legitimately carry a version
// (`claude-sonnet-4`, `glm-5.2`), so a version is not a shape this refuses.
func TestNoModelSlugReachesATranscriptFrame(t *testing.T) {
	shapes := []struct {
		name string
		re   *regexp.Regexp
	}{
		{"a vendor-prefixed id", regexp.MustCompile(`\b[a-z0-9]+/[a-z0-9][a-z0-9._-]*\b`)},
		{"a moving-pointer suffix", regexp.MustCompile(`\b[a-z0-9][a-z0-9.-]*-latest\b`)},
		{"a release stamp", regexp.MustCompile(`\b[a-z][a-z0-9.-]*-\d{8}\b`)},
		{"a variant suffix", regexp.MustCompile(`\b[a-z][a-z0-9.-]*:(free|online|off|low|medium|high)\b`)},
	}

	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "task-" + commissionCommand, Title: "stochastic fitter", Status: store.Done,
			CreatedSeq: 40, UpdatedSeq: 40, StartedAt: fixedNow().Add(-time.Minute),
			FinishedAt: fixedNow()})
	board := &modelBoard{boardBackend: backend, models: map[string][]string{
		"task-" + commissionCommand: {
			"deepseek/deepseek-v4-flash-latest",
			"zhipu/glm-5.2",
			"anthropic/claude-sonnet-4-20250514:high",
		},
	}}
	app := newTestApp(board, &fakeCommander{model: "deepseek/deepseek-v4-flash-latest"}, nil)
	poll(t, app)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 4242, Body: theReading})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		NodeID: "task-" + commissionCommand, Model: "deepseek/deepseek-v4-flash-latest",
		Body: "The fitter is written and the parameters are reported."})
	poll(t, app)

	frame := ansi.Strip(app.Frame(120, 40))
	for _, row := range strings.Split(frame, "\n") {
		// A path is not a slug and legitimately carries slashes; the artifact
		// law says a deliverable is named by its path (12.5).
		if strings.Contains(row, "/tmp") || strings.Contains(row, "~/") ||
			strings.Contains(row, "workspace") {
			continue
		}
		for _, shape := range shapes {
			if hit := shape.re.FindString(row); hit != "" {
				t.Errorf("%s reached a frame (%q): %q", shape.name, hit, row)
			}
		}
	}
	// AND THE HUMANE WORD IS THERE, so the scan is not passing on an empty room:
	// the receipt names who did the work, it just names them in words.
	if !strings.Contains(frame, "deepseek-v4-flash") {
		t.Fatalf("the receipt named nobody at all, so the scan above proved nothing:\n%s", frame)
	}
}

// ONE MODEL IS ONE WORD, deduped AFTER shortening: two provider ids can collapse
// onto one humane word, and a receipt that said `k3 · k3` would be counting one
// model as two.
func TestModelWordsDedupeAfterShortening(t *testing.T) {
	got := modelWords([]string{
		"anthropic/claude-k3-20260801",
		"anthropic/claude-k3",
		"deepseek/deepseek-v4-flash-latest",
		"deepseek/deepseek-v4-flash",
	})
	want := []string{"claude-k3", "deepseek-v4-flash"}
	if len(got) != len(want) {
		t.Fatalf("modelWords collapsed to %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("modelWords = %q, want %q", got, want)
		}
	}
}
