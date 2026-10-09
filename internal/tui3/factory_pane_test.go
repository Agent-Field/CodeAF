package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryPaneOn puts the cursor on the item with id and answers the pane at
// width and room, plain, one string per row.
func factoryPaneOn(t *testing.T, a *app, id, width, room int) []string {
	t.Helper()
	for at, i := range factoryWalk(a.fp.snap) {
		if a.fp.snap.Items[i].ID == id {
			a.fp.cursor = at
			rows := a.factoryPane(width, room)
			out := make([]string, len(rows))
			for j, r := range rows {
				out[j] = ansi.Strip(r)
			}
			return out
		}
	}
	t.Fatalf("item %d is not on the floor", id)
	return nil
}

// factoryPaneItem finds the fixture item with id.
// factoryNoForge clears what the forge says about the item with id, so a
// ladder test reads the ladder itself; the forge's blocks have their own
// tests (factory_polish_test.go).
func factoryNoForge(t *testing.T, a *app, id int) {
	t.Helper()
	it := factoryPaneItem(t, a, id)
	// Comments read and none: nil would be not read yet, which draws a line.
	it.URL, it.Comments, it.Files, it.CheckRuns, it.Activity = "", []factory.Comment{}, nil, nil, nil
}

func factoryPaneItem(t *testing.T, a *app, id int) *factory.Item {
	t.Helper()
	for i := range a.fp.snap.Items {
		if a.fp.snap.Items[i].ID == id {
			return &a.fp.snap.Items[i]
		}
	}
	t.Fatalf("no item %d in the fixture", id)
	return nil
}

// factoryDoneMark and factoryWaitingMark are a done and a waiting phase's
// marks as the strip draws them.
func factoryDoneMark(a *app) string { m, _ := a.factoryPhaseMark(factory.PhaseDone); return m }

func factoryWaitingMark(a *app) string {
	m, _ := a.factoryPhaseMark(factory.PhaseWaiting)
	return m
}

// factoryPaneW is the peek's width beside the rows at a terminal width.
func factoryPaneW(width int) int { return width - factoryRowsCols(width) - 1 }

// factoryPeekBlocks reads a drawn peek back into its blocks: the rows above
// the action line, trimmed of the lead, split at blank rows. It fails the test
// on a blank row at the top, two blank rows together, or no blank row above
// the action line, which are the ladder's three shapes of drift.
func factoryPeekBlocks(t *testing.T, rows []string) [][]string {
	t.Helper()
	n := len(rows)
	if n < 3 {
		t.Fatalf("a peek of %d rows has no ladder", n)
	}
	body := rows[:n-1]
	end := len(body)
	for end > 0 && strings.TrimSpace(body[end-1]) == "" {
		end--
	}
	if end == len(body) {
		t.Fatalf("no blank row above the action line:\n%s", strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(body[0]) == "" {
		t.Fatalf("the peek starts with a blank row:\n%s", strings.Join(rows, "\n"))
	}
	var blocks [][]string
	var cur []string
	for i, r := range body[:end] {
		if strings.TrimSpace(r) == "" {
			if strings.TrimSpace(body[i-1]) == "" {
				t.Fatalf("two blank rows together at %d:\n%s", i, strings.Join(rows, "\n"))
			}
			blocks = append(blocks, cur)
			cur = nil
			continue
		}
		cur = append(cur, strings.TrimSpace(r))
	}
	return append(blocks, cur)
}

// factoryWantBlocks says the blocks begin, in order, with the given words.
func factoryWantBlocks(t *testing.T, what string, blocks [][]string, heads ...string) {
	t.Helper()
	if len(blocks) != len(heads) {
		t.Fatalf("%s has %d blocks, want %d:\n%q", what, len(blocks), len(heads), blocks)
	}
	for i, h := range heads {
		if !strings.HasPrefix(blocks[i][0], h) {
			t.Fatalf("%s block %d starts %q, want %q:\n%q", what, i+1, blocks[i][0], h, blocks)
		}
	}
}

// EVERY ITEM OF THE FIXTURE DRAWS, in every state, at the two widths that
// draw a peek, as exactly its width and its room; nothing a person reads says
// a word of the machinery, a dollar of nothing, or a label with a colon.
func TestFactoryPaneDrawsEveryStateExactly(t *testing.T) {
	a := factoryPlaceLab(t)
	states := map[factory.State]bool{}
	for _, width := range []int{150, 120} {
		paneW := factoryPaneW(width)
		for _, room := range []int{1, 2, 3, 6, 8, 40} {
			for _, it := range a.fp.snap.Items {
				if it.State == factory.StateDismissed {
					continue
				}
				states[it.State] = true
				rows := factoryPaneOn(t, a, it.ID, paneW, room)
				if len(rows) != room {
					t.Fatalf("%s at %d×%d drew %d rows", it.Ref(), paneW, room, len(rows))
				}
				for i, r := range rows {
					if got := ansi.StringWidth(r); got != paneW {
						t.Fatalf("%s at %d×%d: row %d is %d cells: %q", it.Ref(), paneW, room, i, got, r)
					}
					low := strings.ToLower(r)
					for _, banned := range []string{"verified", "verdict", "auditor", "refuted", "$0", "gate:", "cap:", "effort:", "risk:", "places:"} {
						if strings.Contains(low, banned) {
							t.Fatalf("%s says %q: %q", it.Ref(), banned, r)
						}
					}
				}
				if strings.TrimSpace(rows[0]) == "" {
					t.Fatalf("%s at %d×%d starts with a blank row", it.Ref(), paneW, room)
				}
				// THE ACTION LINE IS PINNED TO THE LAST ROW whenever there are two.
				if room >= 2 {
					if last := strings.TrimSpace(rows[room-1]); !strings.HasPrefix(a.factoryActionWords(it), last[:min(len(last), 8)]) {
						t.Fatalf("%s at %d×%d: the last row is not the action line: %q", it.Ref(), paneW, room, rows[room-1])
					}
				}
			}
		}
	}
	for _, st := range []factory.State{factory.StateNew, factory.StateQueued, factory.StateRunning, factory.StateNeedsYou, factory.StateLanded, factory.StateShipped} {
		if !states[st] {
			t.Errorf("the fixture drew no item in state %q", st)
		}
	}
}

// A NEW ITEM'S LADDER: the title and its meta, the read, the chips, the
// stages it would run and its body, a blank row between each and no facts
// block, because the fixture knows nothing worth one about it.
func TestFactoryPeekNewItemLadder(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryNoForge(t, a, 4)
	rows := factoryPaneOn(t, a, 4, factoryPaneW(150), 30)
	blocks := factoryPeekBlocks(t, rows)
	factoryWantBlocks(t, "the new item", blocks,
		"#1662 fix(media): tree rails on narrow widths",
		"claims are testable; three checks cover them",
		"budget  $3",
		a.factoryPendingMark()+" read",
		"Claims",
		"· rails follow the tree")
	if got := blocks[0][1]; got != "codeaf · pr · M · priya · 1h" {
		t.Fatalf("the meta row is %q", got)
	}
	strip := blocks[3][0]
	mark := a.factoryPendingMark()
	// EVERY CELL OF THE STRIP IS ONE WIDTH, its longest name's (`○ approve`).
	if want := mark + " read     " + mark + " checks   " + mark + " review   " + mark + " approve"; strip != want {
		t.Fatalf("the would-run strip is %q, want %q", strip, want)
	}
	for _, key := range []string{"[t]", "[c]", "[e]"} {
		if strings.Contains(blocks[2][0], key) {
			t.Fatalf("the peek's chips name a key: %q", blocks[2][0])
		}
	}
	if last := strings.TrimSpace(rows[len(rows)-1]); last != "enter open · space select" {
		t.Fatalf("the action line is %q", last)
	}
}

// THE FACTS ARE ONE DIM ROW OF PHRASES SIX CELLS APART, no labels: what the
// item may repeat, that it is thin, that its author is a stranger, and what
// risky ground it touches in either shape the read has had.
func TestFactoryPeekFacts(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 6) // thin, from a stranger
	it.Triage.Dup = "7"
	it.Triage.Risk = []string{"mid risk"}
	gap := strings.Repeat(" ", factoryFactGap)
	want := "mid risk" + gap + "maybe a duplicate of #7" + gap + "thin" + gap + "stranger"
	// Sixty cells is one too few for all four, so the last goes, whole.
	blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 6, factoryPaneW(150), 30))
	if cut := strings.TrimSuffix(want, gap+"stranger"); blocks[2][0] != cut {
		t.Fatalf("the facts row is %q, want %q", blocks[2][0], cut)
	}
	blocks = factoryPeekBlocks(t, factoryPaneOn(t, a, 6, 80, 30))
	if blocks[2][0] != want {
		t.Fatalf("the wide facts row is %q, want %q", blocks[2][0], want)
	}
	if row := a.factoryPeekFacts(*it, 200)[0]; row != a.pal.dim(want) {
		t.Fatalf("the facts row is not dim: %q", row)
	}
	if got := factoryRiskWords([]string{"money", "auth", "", "a schema change"}); strings.Join(got, "|") != "touches money|touches auth|a schema change" {
		t.Fatalf("risk words from a list are %q", got)
	}
	if got := factoryRiskWords("low"); len(got) != 0 {
		t.Fatalf("a low risk drew %q", got)
	}
	// Narrow, the facts drop from the right, whole.
	if got := ansi.Strip(a.factoryPeekFacts(*it, 40)[0]); got != "mid risk"+gap+"maybe a duplicate of #7" {
		t.Fatalf("narrow facts are %q", got)
	}
}

// A RUNNING ITEM'S STAGES ARE FOLLOWED BY THE RUNNING STAGE'S OWN LINE as a
// block of its own, and the running cell wears the accent.
func TestFactoryPeekRunningItemLadder(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryNoForge(t, a, 2)
	blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 2, factoryPaneW(150), 30))
	factoryWantBlocks(t, "the running item", blocks,
		"#1551 filters lost on compact",
		"the filter is read before the tree exists",
		"budget  $5",
		"…",
		"review 1/2 · 3 findings · fixing · 4m left")
	if strip := blocks[3][0]; !strings.Contains(strip, "review 1/2") || !strings.Contains(strip, "write ×3") && !strings.Contains(strip, "test") {
		t.Fatalf("the strip is %q", strip)
	}
	it := *factoryPaneItem(t, a, 2)
	painted := a.factoryPeekStrip(it, 200)
	mark, _ := a.factoryPhaseMark(factory.PhaseRunning)
	if !strings.Contains(painted, a.pal.accent(mark)+" "+a.pal.accent("review 1/2")) {
		t.Fatalf("the running cell is not in the accent: %q", painted)
	}
	if !strings.Contains(painted, a.pal.dim(a.factoryPendingMark())+" "+a.pal.dim("neaten")) {
		t.Fatalf("a pending cell is not dim: %q", painted)
	}
}

// A NEEDS-YOU ITEM'S QUESTION COMES DIRECTLY UNDER THE TITLE, before the read:
// the question led by its amber mark, and its keys.
func TestFactoryPeekNeedsYouLadder(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryNoForge(t, a, 1)
	blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 1, factoryPaneW(150), 30))
	q := a.icon(tokens.GNeedsHuman) + " " + factory.ApproveQuestion("plan")
	factoryWantBlocks(t, "the needs-you item", blocks,
		"#1538 budget caps per task",
		q,
		"touches three packages; wants a plan first",
		"touches money",
		"budget  $8",
		factoryDoneMark(a)+" plan")
	// THE STILL FIXTURE HAS NO ANSWER DOOR, so the question draws no keys;
	// over a floor that can answer, they stand under it.
	if len(blocks[1]) != 1 {
		t.Fatalf("a question with no answer door draws keys: %q", blocks[1])
	}
	a.factory = (&factoryFake{}).seam()
	if keys := factoryPeekBlocks(t, factoryPaneOn(t, a, 1, factoryPaneW(150), 30))[1]; len(keys) != 2 || keys[1] != "[y] continue · [n] send back · [a] in words" {
		t.Fatalf("the question's keys are %q", keys)
	}
	// THE AMBER IS ON THE MARK, the words are ink.
	it := *factoryPaneItem(t, a, 1)
	row := a.factoryPeekQuestion(it, 60)[0]
	if !strings.HasPrefix(row, a.pal.ask(a.icon(tokens.GNeedsHuman))) || !strings.Contains(row, a.pal.ink(factory.ApproveQuestion("plan"))) {
		t.Fatalf("the question is not an amber mark and ink words: %q", row)
	}
}

// A LANDED ITEM'S CLAIMS STAND WHERE THE BODY WOULD, a failed one saying so,
// then the policy rows.
func TestFactoryPeekLandedLadder(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryNoForge(t, a, 9)
	rows := factoryPaneOn(t, a, 9, factoryPaneW(160), 30)
	blocks := factoryPeekBlocks(t, rows)
	factoryWantBlocks(t, "the landed item", blocks,
		"#1661 probes fire once, then retire",
		"budget  $5",
		factoryDoneMark(a)+" plan",
		a.icon(tokens.GSettled)+" fires on first true, never again")
	claims := strings.Join(blocks[3], "\n")
	for _, want := range []string{"survives a codeaf restart — not shown", "go.mod unchanged  policy", "view]"} {
		if !strings.Contains(claims, want) {
			t.Fatalf("the claims are missing %q:\n%s", want, claims)
		}
	}
	if last := strings.TrimSpace(rows[len(rows)-1]); last != "enter proof" {
		t.Fatalf("the still fixture's landed keys are %q", last)
	}
	a.factory = (&factoryFake{}).seam()
	rows = factoryPaneOn(t, a, 9, factoryPaneW(150), 30)
	if last := strings.TrimSpace(rows[len(rows)-1]); !strings.HasPrefix(last, "enter proof · e approve with changes · B request changes") {
		t.Fatalf("the action line is %q", last)
	}
}

// A QUEUED ITEM SAYS WHAT FREES IT and a shipped one when it merged and what
// it cost, each as the block under the stages.
func TestFactoryPeekQueuedAndShipped(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 3, factoryPaneW(150), 30), "\n")
	if !strings.Contains(text, "queued · benches full · a bench frees it") {
		t.Fatalf("the queued peek does not say what frees it:\n%s", text)
	}
	text = strings.Join(factoryPaneOn(t, a, 10, factoryPaneW(150), 30), "\n")
	for _, want := range []string{"#1663 spend row shows stale after compact", "merged 06:00 · $1.90", "enter open"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the shipped item is missing %q:\n%s", want, text)
		}
	}
}

// NOTHING IS DRAWN FOR NOTHING: an item with no read, no facts, no cap and no
// body has no block for any of them, and no blank row where one would be.
func TestFactoryPeekEmptyBlocksVanish(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 8)
	it.Triage.Read, it.Body, it.Cap = "", "", 0
	blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 8, factoryPaneW(150), 30))
	// THE THINKING CHIP WITH NO WORD OF ITS OWN IS NOT DRAWN: a bare
	// `thinking  —` floated mid-peek and read as a manager thinking about
	// nothing (owner's screenshot, 2026-10-09).
	factoryWantBlocks(t, "the bare item", blocks, "#1540 meter crashes", a.factoryPendingMark()+" plan")
	if text := strings.Join(factoryPaneOn(t, a, 8, factoryPaneW(150), 30), "\n"); strings.Contains(text, wordThinking) {
		t.Fatalf("the bare item's peek says thinking:\n%s", text)
	}
}

// THE BODY IS SIX ROWS AT MOST, its last row cut with the ellipsis and a dim
// `▾ more`; J and K scroll it a row, pgdn and pgup a page, and the mark goes
// once the end is in view.
func TestFactoryPeekBodyScrolls(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	var lines []string
	for i := 1; i <= 14; i++ {
		lines = append(lines, "line "+itoa(i)+" of the body")
	}
	// Two spaces at a line's end are Markdown's hard break, so each line
	// of the body is a row of its own.
	factoryPaneItem(t, a, 8).Body = strings.Join(lines, "  \n")
	factoryOn(t, a, 8)
	body := func() []string {
		blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 8, factoryPaneW(150), 34))
		return blocks[len(blocks)-1]
	}
	got := body()
	more := a.icon(tokens.GExpanded) + " more"
	if len(got) != factoryPeekBodyRows || got[0] != "line 1 of the body" || !strings.HasSuffix(got[5], more) || !strings.Contains(got[5], "line 6 of the body "+a.icon(tokens.GEllipsis)) {
		t.Fatalf("the cut body is %q", got)
	}
	drive(t, a, key("J"))
	if got := body(); got[0] != "line 2 of the body" {
		t.Fatalf("J did not scroll a row: %q", got)
	}
	drive(t, a, key("K"), key("K"))
	if got := body(); got[0] != "line 1 of the body" {
		t.Fatalf("K past the top moved the body: %q", got)
	}
	drive(t, a, key("pgdown"))
	if got := body(); got[0] != "line 7 of the body" {
		t.Fatalf("pgdown did not scroll a page: %q", got)
	}
	drive(t, a, key("pgdown"), key("pgdown"))
	if got := body(); got[0] != "line 9 of the body" || strings.Contains(strings.Join(got, "\n"), more) {
		t.Fatalf("the end of the body is %q", got)
	}
	drive(t, a, key("pgup"))
	if got := body(); got[0] != "line 3 of the body" {
		t.Fatalf("pgup did not scroll back a page: %q", got)
	}
	// ANOTHER ITEM STARTS AT ITS TOP.
	drive(t, a, key("down"), key("up"))
	if got := body(); got[0] != "line 1 of the body" {
		t.Fatalf("coming back to the item kept its scroll: %q", got)
	}
}

// THE TALK ROW IS DRAWN ONLY WHEN THE ITEM HAS A CONVERSATION, as the last
// block above the action line.
func TestFactoryPeekTalkRow(t *testing.T) {
	a := factoryPlaceLab(t)
	if text := strings.Join(factoryPaneOn(t, a, 4, factoryPaneW(150), 30), "\n"); strings.Contains(text, a.icon(tokens.GActionCommunicate)+" "+wordChat) {
		t.Fatalf("an item with no conversation draws a chat row:\n%s", text)
	}
	factoryPaneItem(t, a, 4).Talk = "chat-1"
	factoryNoForge(t, a, 4)
	blocks := factoryPeekBlocks(t, factoryPaneOn(t, a, 4, factoryPaneW(150), 30))
	if last := blocks[len(blocks)-1]; len(last) != 1 || last[0] != a.icon(tokens.GActionCommunicate)+" "+wordChat {
		t.Fatalf("the talk row is %q", last)
	}
}

// AT THE PLAIN FLOOR THE PEEK IS BYTES A SCREEN READER CAN SAY: no colour, no
// weight, and every mark from the ASCII column of the vocabulary.
func TestFactoryPaneAtThePlainFloorHasNoSGR(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, true)
	a.linear = true
	for _, it := range a.fp.snap.Items {
		if it.State == factory.StateDismissed {
			continue
		}
		for at, i := range factoryWalk(a.fp.snap) {
			if a.fp.snap.Items[i].ID == it.ID {
				a.fp.cursor = at
			}
		}
		for _, r := range a.factoryPane(62, 40) {
			if strings.Contains(r, "\x1b[") {
				t.Fatalf("%s at the plain floor carries SGR: %q", it.Ref(), r)
			}
		}
	}
}

// A DISMISSED ITEM IS NOT ON THE ROWS, but asked for, it reads as a new one.
func TestFactoryPaneDrawsADismissedItemAsNew(t *testing.T) {
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 8)
	it.State = factory.StateDismissed
	if got := a.factoryActionWords(it); got != "enter open" {
		t.Fatalf("a dismissed item's keys are %q", got)
	}
	a.factory = (&factoryFake{}).seam()
	if got := a.factoryActionWords(it); !strings.Contains(got, "r run") || strings.Contains(got, "space select") {
		t.Fatalf("a dismissed item's keys over a launch are %q", got)
	}
}

// A STAGE WHOSE CONDITION DOES NOT FIT THE ITEM IS OFF THE PEEK'S STRIP AND
// SKIPPED ON THE ITEM PAGE, with its reason; on an item it fits it is neither.
func TestFactoryPaneSkipsAStageThatDoesNotFit(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 8) // ready 75
	it.Stages = append(append([]factory.Stage{}, it.Stages...), factory.Stage{Name: "ask", Ask: "ask the author what is missing", When: "thin", On: true})
	last := func() factoryStageView {
		views := a.factoryItemStages(*it)
		return views[len(views)-1]
	}
	if v := last(); !v.skipped {
		t.Fatal("on a ready item the thin stage is not skipped")
	}
	if label, _ := a.factoryStageLabel(last()); !strings.Contains(label, "skipped") {
		t.Fatalf("the skipped stage's label is %q", label)
	}
	if strip := ansi.Strip(a.factoryPeekStrip(*it, 200)); strings.Contains(strip, "ask") {
		t.Fatalf("the peek's strip draws a stage that will not run: %q", strip)
	}
	it.Triage.Readiness = 40
	if v := last(); v.skipped {
		t.Fatal("on a thin item the thin stage is still skipped")
	}
	if strip := ansi.Strip(a.factoryPeekStrip(*it, 200)); !strings.Contains(strip, "ask") {
		t.Fatalf("the peek's strip leaves off a stage that will run: %q", strip)
	}
}

// FITS reads the item's triage, and a word it was never taught fits.
func TestFactoryFits(t *testing.T) {
	it := factory.Item{Triage: factory.Triage{Readiness: 80, Size: "M", Area: "tui"}}
	for _, c := range []struct {
		when string
		want bool
	}{
		{"", true}, {"always", true}, {"thin", false}, {"large", false},
		{"touches auth", false}, {"has ui", true}, {"on a full moon", true},
	} {
		if got := factory.Fits(factory.Stage{When: c.when}, it); got != c.want {
			t.Errorf("Fits(%q) = %v, want %v", c.when, got, c.want)
		}
	}
	it.Triage = factory.Triage{Readiness: 30, Size: "L", Area: "billing"}
	for _, when := range []string{"thin", "large", "touches auth"} {
		if !factory.Fits(factory.Stage{When: when}, it) {
			t.Errorf("Fits(%q) refused an item it describes", when)
		}
	}
}

// A GITHUB ITEM WHOSE COMMENTS ARE NOT READ YET SAYS SO, dim, and one read
// with none says nothing: the two are not the same output.
func TestFactoryCommentsNotReadYetIsNotNone(t *testing.T) {
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 2)
	it.Comments = nil
	if got := a.factoryCommentsBlock(it, 60, 0); len(got) != 1 || ansi.Strip(got[0]) != "comments not read yet" || got[0] != a.pal.dim("comments not read yet") {
		t.Fatalf("not read yet drew %q", got)
	}
	it.Comments = []factory.Comment{}
	if got := a.factoryCommentsBlock(it, 60, 0); got != nil {
		t.Fatalf("read and none drew %q", got)
	}
	it.Origin, it.Comments = factory.OriginTerminal, nil
	if got := a.factoryCommentsBlock(it, 60, 0); got != nil {
		t.Fatalf("an item never on github drew %q", got)
	}
}
