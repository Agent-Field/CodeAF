package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ANSWER HIERARCHY, PINNED (hierarchy.go).
//
// Every want here is asked of the STRUCTURE — which block was followed by work,
// which one the turn ended on — and never of the words in it. A test that
// recognised narration by reading it would be agreeing with the bug the file was
// written to end.

// sgrOf is the escape one tier writes, asked of the palette rather than spelled
// out, so a retune moves one line in styles.go and no line here (settle_test.go's
// [liveSGR] states the rule).
func sgrOf(paint func(string) string) string {
	painted := paint("x")
	if at := strings.Index(painted, "x"); at > 0 {
		return painted[:at]
	}
	return ""
}

// hierarchyLab is one ordinary agentic turn, entry for entry: the question,
// narration, a thought, two calls, and the answer the turn ended on.
func hierarchyLab() []entry {
	return []entry{
		{kind: entryUser, text: "why is the parser slow?", turn: 1},
		{kind: entryAssistant, text: "Let me check the config first.", turn: 1, settled: true},
		{kind: entryThinking, text: "the prefix is read twice", turn: 1, settled: true},
		{kind: entryTool, tool: "read", text: "parser.go", turn: 1, status: toolOK},
		{kind: entryTool, tool: "bash", text: "go test", turn: 1, status: toolOK},
		{kind: entryAssistant, text: "It reads the length prefix twice.", turn: 1, settled: true},
	}
}

// rowWithText is the laid-out row a phrase landed on, painted.
func rowWithText(t *testing.T, a *app, phrase string) row {
	t.Helper()
	for _, r := range rows(a) {
		if strings.Contains(plain(r.text), phrase) {
			return r
		}
	}
	t.Fatalf("no row carries %q:\n%s", phrase, strings.Join(plainRows(a), "\n"))
	return row{}
}

// 1. LIVE DEMOTION. Prose that more work opened under is not the answer, and the
// surface says so in the two channels it has: the column and the lightness.
func TestNarrationRecedesWhenWorkOpensUnderIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = hierarchyLab(), config.WorkOpen
	a.touch()

	narration := rowWithText(t, a, "Let me check the config")
	if !strings.HasPrefix(plain(narration.text), "  ") {
		t.Fatalf("narration kept the answer's margin: %q", plain(narration.text))
	}
	if !strings.Contains(narration.text, sgrOf(a.pal.narr)) {
		t.Fatalf("narration kept the body ink: %q", narration.text)
	}

	answer := rowWithText(t, a, "reads the length prefix twice")
	if strings.HasPrefix(plain(answer.text), " ") {
		t.Fatalf("the answer was pushed into the work column: %q", plain(answer.text))
	}
	if strings.Contains(answer.text, sgrOf(a.pal.narr)) {
		t.Fatalf("the answer was demoted with its narration: %q", answer.text)
	}
}

// A DEMOTED BLOCK CARRIES NO WEIGHT. MARKDOWN OWNS WEIGHT, so a heading inside
// narration would render heavier than the answer under it and invert the very
// hierarchy the demotion states.
func TestDemotedNarrationDropsItsMarkdown(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = hierarchyLab(), config.WorkOpen
	a.entries[1].text = "## Plan\n\nI will **read** the parser."
	a.touch()

	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "## Plan") {
		t.Fatalf("the heading's own characters were rendered away:\n%s", body)
	}
	if strings.Contains(rowWithText(t, a, "## Plan").text, sgrOf(a.pal.bold)) {
		t.Fatalf("a demoted heading was drawn bold: %q", rowWithText(t, a, "## Plan").text)
	}
}

// AND IT DROPS THE MID-STREAM MARKDOWN CUT WITH IT. A block demoted while it was
// half promoted must not come back as half rendered markdown and half plain
// (render.go's [entry.mdCut]).
func TestADemotedBlockForgetsItsMarkdownCut(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	const head = "Checking the parser.\n"
	a.entries = []entry{
		{kind: entryUser, text: "go", turn: 1},
		{kind: entryAssistant, text: head + "It reads the prefix", turn: 1, mdCut: len(head)},
		{kind: entryTool, tool: "read", text: "parser.go", turn: 1, status: toolOK},
	}
	a.workMode = config.WorkOpen
	a.touch()

	got := strings.Join(plainRows(a), "\n")
	if !strings.Contains(got, "Checking the parser.") || !strings.Contains(got, "It reads the prefix") {
		t.Fatalf("the cut block lost half of itself:\n%s", got)
	}
	painted := strings.Join(func() []string {
		out := make([]string, 0, len(a.rows))
		for _, r := range rows(a) {
			out = append(out, r.text)
		}
		return out
	}(), "\n")
	if strings.Contains(painted, liveSGR()) {
		t.Fatalf("a demoted block kept the growing edge's ink:\n%s", painted)
	}
}

// 2. PROMOTION AT SETTLE, and the one blank row that opens above it.
func TestAPromotedAnswerTakesOneBreathAboveIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = hierarchyLab(), config.WorkOpen
	a.touch()

	lines := plainRows(a)
	at := -1
	for i, line := range lines {
		if strings.Contains(line, "reads the length prefix twice") {
			at = i
		}
	}
	if at < 1 {
		t.Fatalf("the answer is not on screen:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[at-1]) != "" {
		t.Fatalf("no breath above the answer:\n%s", strings.Join(lines, "\n"))
	}
	if at > 1 && strings.TrimSpace(lines[at-2]) == "" {
		t.Fatalf("the breath was drawn twice:\n%s", strings.Join(lines, "\n"))
	}
}

// AND A FOLDED TURN GETS THE SAME BREATH. The chip is the work, and an answer
// wedged against it reads as a line of the chip.
func TestAFoldedTurnsAnswerTakesTheBreathToo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = hierarchyLab(), config.WorkFold
	a.touch()

	lines := plainRows(a)
	// AND THE NARRATION WENT INTO THE CHIP WITH THE CLUSTER IT NARRATED. It is
	// the same run of blocks either way — the chip covers everything from the
	// first thing the turn did to the answer it reached — so a person opening the
	// fold gets the sentence back beside the calls it was about.
	if strings.Contains(strings.Join(lines, "\n"), "Let me check the config") {
		t.Fatalf("the narration stayed outside its own cluster's chip:\n%s", strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if !strings.Contains(line, "reads the length prefix twice") {
			continue
		}
		if i < 1 || strings.TrimSpace(lines[i-1]) != "" {
			t.Fatalf("the answer is wedged against the chip:\n%s", strings.Join(lines, "\n"))
		}
		return
	}
	t.Fatalf("the folded turn lost its answer:\n%s", strings.Join(lines, "\n"))
}

// 3. A TURN WITH NO WORK IN IT IS UNTOUCHED BY THE HIERARCHY: no gutter and no
// tier on either block. What it does get is the SPACING pass's two blanks — one
// row of air at the top of the conversation, and the change-of-speaker gap
// between the question and its reply (render.go's wasUser) — so the byte
// comparison is against exactly that shape and nothing looser.
func TestATurnWithNoWorkIsUntouchedByTheHierarchy(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "what is 2+2?", turn: 1},
		{kind: entryAssistant, text: "Four.", turn: 1, settled: true},
	}
	a.touch()

	want := append([]string{""}, a.renderEntry(0, &a.entries[0], a.width)...)
	want = append(want, "")
	want = append(want, a.settledMarkdown(1, &a.entries[1], a.width)...)
	var got []string
	for _, r := range rows(a) {
		got = append(got, r.text)
	}
	if len(got) != len(want) {
		t.Fatalf("a work-free turn drew %d rows, not the %d its two blocks and two blanks render to:\n%q",
			len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is not byte-identical:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// 4. AN INTERRUPTED TURN PROMOTES NOTHING, live.
func TestAnInterruptedTurnPromotesNothing(t *testing.T) {
	_, a := wired([]session.Event{text(session.EventTextDelta, "Looking at the parser now")})
	typeLine(t, a, "why is it slow?")
	drive(t, a, frameMsg{})
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "Looking at the parser") {
		t.Fatalf("the reply never arrived:\n%s", strings.Join(plainRows(a), "\n"))
	}

	drive(t, a, key("esc"))
	a.workMode = config.WorkOpen
	a.touch()

	partial := rowWithText(t, a, "Looking at the parser")
	if !strings.HasPrefix(plain(partial.text), "  ") || !strings.Contains(partial.text, sgrOf(a.pal.narr)) {
		t.Fatalf("a stopped turn promoted its last paragraph: %q", partial.text)
	}

	// AND PERMANENTLY: the next question does not launder the one before it.
	typeLine(t, a, "never mind, what is 2+2?")
	a.touch()
	partial = rowWithText(t, a, "Looking at the parser")
	if !strings.Contains(partial.text, sgrOf(a.pal.narr)) {
		t.Fatalf("the stopped turn was promoted by the turn after it: %q", partial.text)
	}
}

// AND THE CHIP SAYS WHO STOPPED IT — facts, in the person's own terms, with no
// verdict and no mark of success anywhere near it.
func TestAStoppedTurnsChipStatesWhatHappened(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = hierarchyLab(), config.WorkFold
	for i := range a.entries {
		if a.entries[i].kind != entryUser {
			a.entries[i].cut = true
		}
	}
	a.entries = append(a.entries, entry{kind: entryNote, text: "stopped", turn: 1})
	a.touch()

	got := strings.Join(plainRows(a), "\n")
	if !strings.Contains(got, "▸ stopped by you") {
		t.Fatalf("the chip does not say the turn was stopped:\n%s", got)
	}
	if !strings.Contains(got, "2 tool calls") {
		t.Fatalf("the chip dropped the count:\n%s", got)
	}
	if strings.Contains(got, "worked") || strings.Contains(got, "✓") {
		t.Fatalf("a stopped turn was reported as a finished one:\n%s", got)
	}
	// THE MACHINERY IS GONE AND SO IS THE HALF-SENTENCE — the chip is the whole
	// of what a stopped turn leaves, which is the missing answer said plainly.
	if strings.Contains(got, "reads the length prefix twice") {
		t.Fatalf("a stopped turn left prose standing under its chip:\n%s", got)
	}
	// EXCEPT THE SURFACE'S OWN NEWS, which is addressed to the person and is
	// never inside a fold.
	if !strings.Contains(got, "· stopped") {
		t.Fatalf("the interrupt's own line was folded away:\n%s", got)
	}
}

// 5. TURN STRUCTURE, NOT HEURISTICS. A turn that ended on a call reached no
// answer, so nothing in it is promoted — including the prose above the call,
// which is exactly the block a text-sniffing rule would have promoted.
func TestATurnEndingOnAToolCallPromotesNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "go", turn: 1},
		{kind: entryAssistant, text: "Reading the parser.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", text: "parser.go", turn: 1, status: toolOK},
	}
	a.workMode = config.WorkOpen
	a.touch()

	if r := rowWithText(t, a, "Reading the parser"); !strings.HasPrefix(plain(r.text), "  ") {
		t.Fatalf("a turn with no answer promoted its narration: %q", plain(r.text))
	}
	if len(deriveWorkfolds(a.entries, 0)) != 0 {
		t.Fatal("a turn with no answer derived a chip to hide its work behind")
	}
}

// AND A MESSAGE SENT INTO A RUNNING TURN IS A SECOND QUESTION, not work. Steering
// does not open a turn (app.go's [app.submittingShown]), so the turn number alone
// would read the prose before the steer as narration for the answer after it.
func TestASteerEndsTheAnswerItFollows(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "what is in this repo?", turn: 1},
		{kind: entryAssistant, text: "A parser and a surface.", turn: 1, settled: true},
		{kind: entryUser, text: "go deeper please", turn: 1},
		{kind: entryAssistant, text: "The parser reads a length prefix.", turn: 1, settled: true},
	}
	a.touch()

	if r := rowWithText(t, a, "A parser and a surface"); strings.HasPrefix(plain(r.text), " ") {
		t.Fatalf("prose answering the question before a steer was demoted: %q", plain(r.text))
	}
	for _, f := range deriveWorkfolds(a.entries, 0) {
		t.Fatalf("a chip covering two questions was derived: %#v", f)
	}
}

// 6. ONE IDEOLOGY, EVERY CHAT SURFACE. A room derives no chips at all (A ROOM
// FOLDS NOTHING) and still reads its prose the same way, because the hierarchy is
// a property of the blocks and the chip is an affordance of the conversation.
func TestANodesRoomKeepsTheSameAnswerHierarchy(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 40
	a.room = &taskRoom{
		id: 7, title: "the node", live: -1, think: -1,
		unfolded: map[int]bool{}, workOpen: map[int]bool{},
		entries: hierarchyLab(),
	}

	drawn, _ := a.deckRows(a.room.deck(), 60)
	find := func(phrase string) row {
		t.Helper()
		for _, r := range drawn {
			if strings.Contains(plain(r.text), phrase) {
				return r
			}
		}
		t.Fatalf("the room drew no row carrying %q", phrase)
		return row{}
	}
	if r := find("Let me check the config"); !strings.HasPrefix(plain(r.text), "  ") ||
		!strings.Contains(r.text, sgrOf(a.pal.narr)) {
		t.Fatalf("a room promoted its narration: %q", r.text)
	}
	if r := find("reads the length prefix twice"); strings.HasPrefix(plain(r.text), " ") {
		t.Fatalf("a room demoted its answer: %q", plain(r.text))
	}
	if strings.Contains(strings.Join(func() []string {
		out := make([]string, 0, len(drawn))
		for _, r := range drawn {
			out = append(out, plain(r.text))
		}
		return out
	}(), "\n"), "▸ ") {
		t.Fatal("a room folded its work into a chip")
	}
}

// 7. A RESUMED CONVERSATION SHOWS WHAT THE LIVE ONE SHOWED. The hierarchy is
// derived from the entry list and the journal rebuilds that list, so replay gets
// it for nothing — which is the whole argument for a structural rule.
func TestAResumedConversationRebuildsTheSameHierarchy(t *testing.T) {
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{
		{Role: "user", Text: "why is the parser slow?"},
		{Role: "assistant", Text: "Let me check the config first."},
		{Role: "tool", Tool: "read", Hint: "parser.go", Args: "{}", Output: "ok"},
		{Role: "assistant", Text: "It reads the length prefix twice."},
	}}
	a := newTestApp(agent)
	a.workMode = config.WorkOpen
	a.replay()
	a.touch()

	if r := rowWithText(t, a, "Let me check the config"); !strings.HasPrefix(plain(r.text), "  ") ||
		!strings.Contains(r.text, sgrOf(a.pal.narr)) {
		t.Fatalf("a resumed turn promoted its narration: %q", r.text)
	}
	if r := rowWithText(t, a, "reads the length prefix twice"); strings.HasPrefix(plain(r.text), " ") {
		t.Fatalf("a resumed turn demoted its answer: %q", plain(r.text))
	}
}

// 8. THE ROW CACHE FOLLOWS THE HIERARCHY. A block that changed tier is holding
// the rows it drew in the other one, and [app.entryRows] hands those back unless
// the flip says otherwise — which is the defect [stampHierarchy] pairs with.
func TestTheRowCacheFollowsTheHierarchy(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "why is it slow?", turn: 1},
		{kind: entryAssistant, text: "Let me check the config first.", turn: 1, settled: true},
	}
	a.workMode = config.WorkOpen
	a.touch()
	if r := rowWithText(t, a, "Let me check the config"); strings.HasPrefix(plain(r.text), " ") {
		t.Fatalf("the trailing block was not the answer to begin with: %q", plain(r.text))
	}
	if !a.entries[1].built {
		t.Fatal("the block was never cached, so this proves nothing")
	}

	// The work opens under it. Nothing touches the block itself.
	a.entries = append(a.entries, entry{kind: entryTool, tool: "read", text: "parser.go", turn: 1, status: toolOK})
	a.touch()
	if r := rowWithText(t, a, "Let me check the config"); !strings.HasPrefix(plain(r.text), "  ") ||
		!strings.Contains(r.text, sgrOf(a.pal.narr)) {
		t.Fatalf("the block handed back the rows it drew as the answer: %q", r.text)
	}
}
