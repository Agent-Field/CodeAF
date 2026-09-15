package tui3

// THE EVIDENCE AN ANSWER BRINGS, WHEREVER A QUESTION IS DRAWN.
//
// These hold the owner's three picks of 2026-09-11 about evidence: at a hundred
// columns and wider a panel whose answers brought something to look at lays
// them in a list with the evidence of the one the pointer is on beside it
// (preview A); narrower, that evidence unfolds under the pointer's row
// (preview-narrow B); and `o` opens the page as two panes (page A). And they
// hold what keeps those drawings honest: the one chooser decides, the evidence
// follows the pointer, a panel never takes more than half the frame, a press on
// evidence presses nothing, the answer the pointer is on is always on screen,
// and the page's two rules meet one seam.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// tallEvidence is the worked example with a diagram under its pick far taller
// than any panel may be.
func tallEvidence() session.Question {
	q := demoQuestionReading()
	q.Form = session.FormCard
	q.Options[0].Blocks = []session.Block{{Kind: session.BlockDiagram, Title: "all of it",
		Body: strings.TrimRight(strings.Repeat("a row of the drawing\n", 30), "\n")}}
	return q
}

// THE SPLIT IS A RUNG OF THE ONE CHOOSER, AND IT READS WHAT THE ANSWERS CARRY
// AND THE WIDTH — nothing else. The same question takes the split from the width
// two columns stand at and the panel below it; a question whose only evidence is
// the question's own never splits, because a pane that follows the pointer would
// have nothing different to show for each answer.
func TestTheSplitIsARungOfTheOneChooser(t *testing.T) {
	lab := newQuestionLab(t)
	q := questionShown{question: demoQuestionReading(), shown: lab.a.now()}
	if got := lab.a.questionViewOf(q, railSlimFloor); got != viewSplit {
		t.Errorf("at %d columns answers with blocks take view %d, want the split", railSlimFloor, got)
	}
	if got := lab.a.questionViewOf(q, railSlimFloor-1); got != viewPanel {
		t.Errorf("at %d columns answers with blocks take view %d, want the panel", railSlimFloor-1, got)
	}
	attached := demoQuestionReading()
	for i := range attached.Options {
		attached.Options[i].Blocks = nil
	}
	attached.Attach = []session.Block{{Kind: session.BlockText, Body: "the whole decision"}}
	if got := lab.a.questionViewOf(questionShown{question: attached, shown: lab.a.now()}, 140); got != viewPanel {
		t.Errorf("a question whose only evidence is its own takes view %d, want the panel", got)
	}
	found := false
	for _, rung := range questionLadder {
		found = found || rung.view == viewSplit
	}
	if !found {
		t.Fatal("the split is not a rung of the ladder")
	}
}

// THE EVIDENCE FOLLOWS THE POINTER, ON THE PANEL AS ON THE PAGE. The list stays
// where it is and the pane beside it changes to the answer walked to — which is
// the whole of what a pane beside a list is for.
func TestTheEvidenceBesideTheListFollowsThePointer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 140, 60
	lab.raise(demoQuestionReading())
	lab.tick(time.Second)
	first := lab.plain()
	if !strings.Contains(first, "Rows already carry a foreign key") || strings.Contains(first, "One file in the repository") {
		t.Fatalf("the pane should hold the pick's evidence and only its:\n%s", first)
	}
	if !strings.Contains(first, tokens.GlyphFrameSide+" postgres") {
		t.Errorf("the pane should open on the answer's own word, beside the seam:\n%s", first)
	}
	lab.press("down")
	moved := lab.plain()
	if !strings.Contains(moved, "One file in the repository") || strings.Contains(moved, "Rows already carry a foreign key") {
		t.Errorf("walking down should put the second answer's evidence in the pane:\n%s", moved)
	}
	// AND THE LIST CARRIES NO CONSEQUENCE: what an answer leaves true is the
	// pane's first labelled line, and the row beside the pane says it nowhere.
	if strings.Count(moved, "the ledger travels with the checkout") != 1 ||
		!strings.Contains(moved, questionThenWord+"the ledger travels with the checkout") {
		t.Errorf("what taking it leaves true belongs in the pane, labelled:\n%s", moved)
	}
}

// NARROWER THAN TWO PANES, THE EVIDENCE UNFOLDS UNDER THE POINTER'S ROW
// (preview-narrow pick B), and the row gives up what it leaves true to the
// unfolded evidence rather than saying it twice and cutting it the first time.
func TestNarrowerThanTwoPanesTheEvidenceUnfoldsUnderItsRow(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 96, 60
	lab.raise(demoQuestionReading())
	rows := questionPlainRows(lab.rows())
	pick, block, next := -1, -1, -1
	for at, row := range rows {
		switch {
		case strings.Contains(row, "1  postgres"):
			pick = at
			if strings.Contains(row, "share one connection") {
				t.Errorf("the pointer's row should give its consequence to the evidence under it: %q", row)
			}
		case strings.Contains(row, "what it would look like"):
			block = at
		case strings.Contains(row, "2  sqlite beside the project"):
			next = at
		}
	}
	if !(pick >= 0 && pick < block && block < next) {
		t.Fatalf("the evidence should unfold between the pointer's row and the next answer (%d %d %d):\n%s",
			pick, block, next, strings.Join(rows, "\n"))
	}
	if !strings.Contains(strings.Join(rows, "\n"), questionThenWord+"the ledger and the rest of the project share one connection") {
		t.Errorf("the unfolded evidence should carry what taking it leaves true:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(strings.Join(rows, "\n"), tokens.GlyphFrameSide+" postgres") {
		t.Errorf("a narrow panel should not split:\n%s", strings.Join(rows, "\n"))
	}
}

// A PANEL NEVER TAKES MORE THAN HALF THE FRAME, split or unfolded, and a cut is
// said on its last row with its count and the way to the rest.
func TestAPanelNeverTakesMoreThanHalfTheFrame(t *testing.T) {
	for _, width := range []int{140, 110, 96} {
		lab := newQuestionLab(t)
		lab.a.width, lab.a.height = width, 40
		lab.raise(tallEvidence())
		rows := questionPlainRows(lab.rows())
		if len(rows) > lab.a.questionPanelTall()+1 {
			t.Errorf("at %d columns the panel took %d rows of a %d-row frame:\n%s",
				width, len(rows), lab.a.height, strings.Join(rows, "\n"))
		}
		drawn := strings.Join(rows, "\n")
		if !strings.Contains(drawn, questionMoreWord+questionKeyGap+lab.a.questionOpenFullWord()) {
			t.Errorf("at %d columns the cut should say how much is left and how to reach it:\n%s", width, drawn)
		}
		for _, row := range lab.rows() {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("at %d columns a row is %d cells: %q", width, got, ansi.Strip(row))
			}
		}
	}
}

// A PRESS ON THE LIST PRESSES AN ANSWER AND A PRESS ON THE EVIDENCE PRESSES
// NOTHING. The pane is prose and pictures, and a click aimed at a diagram that
// answered the question would be the worst press on this surface.
func TestAPressOnTheEvidenceBesideTheListPressesNothing(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 140, 60
	lab.raise(demoQuestionReading())
	seam := -1
	for _, row := range questionPlainRows(lab.rows()) {
		if at := strings.Index(row, tokens.GlyphFrameSide+" postgres"); at >= 0 {
			seam = ansi.StringWidth(row[:at])
		}
	}
	if seam < 0 {
		t.Fatal("the panel did not split")
	}
	if len(lab.a.questionBands) == 0 {
		t.Fatal("the list recorded no answers to press")
	}
	for _, band := range lab.a.questionBands {
		if band.span.to > seam {
			t.Errorf("answer %d's target reaches past the seam at %d into the evidence: %+v", band.at, seam, band.span)
		}
	}
}

// THE ANSWER THE POINTER IS ON IS ALWAYS ON SCREEN, in one column and in two
// panes, however far down the arrows walk and however tall the evidence under
// it — the defect lane F measured on the old page, where the window moved only
// under the wheel.
func TestTheAnswerThePointerIsOnIsAlwaysOnScreen(t *testing.T) {
	q := tallEvidence()
	q.Form = session.FormRoom
	for i := 0; i < 4; i++ {
		q.Options = append(q.Options, session.AnswerOption{
			Key: itoa(len(q.Options) + 1), Label: "another store, number " + itoa(i),
			Body: "A body long enough to take a row or two of the column.", Consequence: "something follows",
		})
	}
	for _, width := range []int{92, 140} {
		a, _ := standingInAQuestionAt(t, q, width, 22)
		for step := 0; step < len(q.Options); step++ {
			want := questionOptionKeyAt(q, a.qroom.focus) + "  " + strings.TrimSpace(q.Options[a.qroom.focus].Label)
			if drawn := pageText(a); !strings.Contains(drawn, want) {
				t.Fatalf("at %d columns the pointer's answer %q is off the screen:\n%s", width, want, drawn)
			}
			tapNamed(t, a, tea.KeyDown, 0)
		}
		for step := 0; step < len(q.Options); step++ {
			tapNamed(t, a, tea.KeyUp, 0)
		}
		if drawn := pageText(a); !strings.Contains(drawn, "1  postgres") {
			t.Fatalf("at %d columns walking back to the top left the first answer off the screen:\n%s", width, drawn)
		}
	}
}

// THE PAGE STANDS AS TWO PANES FROM THE WIDTH TWO COLUMNS STAND AT — the body's
// width, which is the frame less any task column beside it — and the rule under
// the head and the rule in the foot meet one seam: the junction the foot draws is
// in the column the body's seam runs down.
func TestThePagesTwoRulesMeetOneSeam(t *testing.T) {
	split := 0
	for _, width := range []int{96, 120, 140, 180} {
		a, _ := standingInAQuestionAt(t, demoQuestionReading(), width, 40)
		body := a.bodyWidth()
		drawn := a.questionRoomRows(body)
		rows := make([]string, len(drawn))
		for i, r := range drawn {
			rows[i] = plain(r.text)
		}
		seam := a.qroom.seam
		if want := body >= railSlimFloor; (seam >= 0) != want {
			t.Fatalf("a body %d wide drew seam %d; two panes stand from %d", body, seam, railSlimFloor)
		}
		if seam < 0 {
			continue
		}
		split++
		top := -1
		for at, row := range rows {
			if strings.Contains(row, tokens.GlyphFrameTeeDown) {
				top = at
				if got := string([]rune(row)[seam]); got != tokens.GlyphFrameTeeDown {
					t.Errorf("at %d columns the rule over the panes meets the seam at %d with %q", body, seam, got)
				}
			}
		}
		if top < 0 {
			t.Fatalf("at %d columns there is no rule over the panes:\n%s", body, strings.Join(rows, "\n"))
		}
		for _, row := range rows[top+1:] {
			if got := string([]rune(row)[seam]); got != tokens.GlyphFrameSide {
				t.Errorf("at %d columns the seam breaks: %q", body, row)
				break
			}
		}
		foot := []rune(plain(a.questionFootRows(width)[0]))
		if got := string(foot[seam]); got != tokens.GlyphFrameTeeUp {
			t.Errorf("at %d columns the foot's rule meets the seam with %q: %q", body, got, string(foot))
		}
	}
	if split == 0 {
		t.Fatal("no width drew two panes")
	}
}

// THE PAGE'S FOOT IS ITS OWN RULE, so the legend and its blank are not drawn a
// row above it while the page is up — two rules one row apart, the upper one
// naming keys the page does not take — and the geometry that counts the
// conversation's rows agrees with the frame that draws them.
func TestThePagesFootReplacesTheLegend(t *testing.T) {
	a, _ := standingInAQuestionAt(t, demoQuestionReading(), 140, 40)
	chrome, _, _, _ := a.chrome(a.width)
	for _, row := range chrome {
		if strings.Contains(plain(row), "/ commands") {
			t.Errorf("the conversation's legend is drawn over the page's foot: %q", plain(row))
		}
	}
	if got, want := len(chrome), a.chromeHeight(); got != want {
		t.Errorf("the frame draws %d chrome rows and the geometry charges %d", got, want)
	}
}

// THE KEYS THE PAGE DID NOT DRAW ARE STILL THE SURFACE'S. The page takes the
// keys it prints on its foot and nothing else — so `ctrl+g`, which stows the
// task column, works while a question is open on the page, and the cells the
// column gives back are what the split is measured in
// ([app.questionRoomBeside] reads [app.bodyWidth]).
func TestTheColumnsOwnKeyStillWorksWhileThePageIsUp(t *testing.T) {
	a, _ := standingInAQuestionAt(t, demoQuestionReading(), 120, 40)
	if _, took := tapNamed(t, a, 'g', tea.ModCtrl); took {
		t.Fatal("the page took ctrl+g: a key it never printed is a key the surface under it owes")
	}
}

// A CLOCK THAT WILL ANSWER SAYS SO IN EVERY DRAWING, AND AT THE WIDTH IT WAS
// WRITTEN FOR. #954 put that sentence inside the panel's frame, which is right;
// the split and the page lay their own rows, so each has to place it — and
// neither pane will do: a column half the panel wide cuts it mid-word, and the
// pane beside the list is ONE answer's case, where a sentence about the whole
// decision reads as that answer's. So it crosses the seam on the panel and
// stands with the reason on the page.
func TestTheClockSaysWhatCanBeDoneAboutItAtTheFramesOwnWidth(t *testing.T) {
	clocked := func() session.Question {
		q := demoQuestionReading()
		q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: 30 * time.Second}
		return q
	}
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 140, 60
	q := clocked()
	q.Deadline = lab.a.now().Add(30 * time.Second)
	lab.raise(q)
	rows := questionPlainRows(lab.rows())
	drawn := strings.Join(rows, "\n")
	if !strings.Contains(drawn, questionClockAsideWord) {
		t.Fatalf("the split dropped the clock's own line:\n%s", drawn)
	}
	// AND IT CROSSES THE SEAM, WHOLE. A row inside a pane carries three of the
	// frame's sides — its own two edges and the seam between the panes — and
	// this row has two, which is the sentence at the frame's own width.
	for _, row := range rows {
		if !strings.Contains(row, questionClockAsideWord) {
			continue
		}
		if got := strings.Count(row, tokens.GlyphFrameSide); got != 2 {
			t.Errorf("the clock's line is inside a pane (%d frame sides on the row): %q", got, row)
		}
	}

	page, _ := standingInAQuestionAt(t, clocked(), 140, 40)
	page.qroom.head.question.Deadline = page.now().Add(30 * time.Second)
	page.questionRoomTouched()
	if got := pageText(page); !strings.Contains(got, questionClockAsideWord) {
		t.Errorf("the page dropped the clock's own line:\n%s", got)
	}
}

// TestThePagesTwoRulesEndInTheSameColumnWithTheTaskColumnUp is the drawing bug
// the whole screens table hid: every split shot stowed the column, and the e2e
// scenario stows it too.
//
// THE PAGE'S TWO RULES ARE TWO EDGES OF ONE OBJECT. The body draws at
// [app.bodyWidth], which is charged for the task column, and the foot is chrome
// drawn at the full frame width — so the top rule stopped at the body's edge
// while the foot's ran on under the rail. It only shows where the body is still
// wide enough to split WITH the column standing, which is a 140-cell terminal.
func TestThePagesTwoRulesEndInTheSameColumnWithTheTaskColumnUp(t *testing.T) {
	a, _ := standingInAQuestionAt(t, demoQuestionReading(), 140, 40)
	if a.railAway {
		t.Fatal("this test needs the task column UP; the lab stowed it")
	}
	body := a.bodyWidth()
	if !a.questionRoomBeside(body) {
		t.Fatalf("the body is %d cells and did not split; the case this test is about cannot arise", body)
	}
	rows := a.questionRoomRows(body)
	top := ""
	for _, r := range rows {
		if strings.Contains(plain(r.text), tokens.GlyphFrameTeeDown) {
			top = plain(r.text)
			break
		}
	}
	if top == "" {
		t.Fatalf("the page drew no top rule at %d cells:\n%s", body, pageText(a))
	}
	foot := plain(a.questionFootRows(a.width)[0])
	if ansi.StringWidth(top) != ansi.StringWidth(foot) {
		t.Errorf("the page's rules end in different columns: top %d, foot %d\ntop:  %q\nfoot: %q",
			ansi.StringWidth(top), ansi.StringWidth(foot), top, foot)
	}
	// AND THE JUNCTIONS STAND IN ONE COLUMN, which is the seam the body drew
	// rather than one worked out a second time. Measured in CELLS and not bytes:
	// every one of these glyphs is three bytes wide and one column wide.
	column := func(row, glyph string) int {
		at := strings.Index(row, glyph)
		if at < 0 {
			return -1
		}
		return ansi.StringWidth(row[:at])
	}
	if got, want := column(foot, tokens.GlyphFrameTeeUp), column(top, tokens.GlyphFrameTeeDown); got != want {
		t.Errorf("the junctions are in different columns: top %d, foot %d\ntop:  %q\nfoot: %q", want, got, top, foot)
	}
	if a.questionRoomSeam() != a.qroom.seam {
		t.Errorf("the foot's seam (%d) is not the seam the body stored (%d)", a.questionRoomSeam(), a.qroom.seam)
	}
}

// TestACardDrawingAQuestionsAnswersUnfoldsNoEvidence holds the bound on the door
// two other surfaces draw these rows through.
//
// A CARD CANNOT GROW BY EVERY DIAGRAM AN ANSWER BROUGHT. The task record's
// landing card ([app.taskRecordLandingRows]) and home's errand pane both draw
// [app.questionPanelBody], and neither owns its height — so the split and the
// unfold are the panel's and the page's, which do.
func TestACardDrawingAQuestionsAnswersUnfoldsNoEvidence(t *testing.T) {
	lab := newQuestionLab(t)
	q := demoQuestionReading()
	shown := questionShown{question: q, shown: lab.a.now(), pick: questionPointerStart(q)}
	body := strings.Join(questionPlainRows(lab.a.questionPanelBody(shown, 96)), "\n")
	// The case's own labels are what the evidence draws, wherever it is drawn.
	for _, word := range []string{questionThenWord, questionWhyWord, questionConfidenceLabel} {
		if strings.Contains(body, strings.TrimSpace(word)) {
			t.Errorf("a card's rows carry an answer's case (%q):\n%s", word, body)
		}
	}
	// AND EVERY ANSWER IS STILL THERE — bounding the evidence may not cost the
	// list the thing it is for.
	for _, option := range q.Options {
		if !strings.Contains(body, strings.TrimSpace(option.Label)) {
			t.Errorf("a card's rows dropped the answer %q:\n%s", option.Label, body)
		}
	}
}
