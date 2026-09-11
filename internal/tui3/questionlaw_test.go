package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE TWO LAWS OF THE QUESTION VIEWS ──────────────────────────────────────
//
// Both are owner rulings of 2026-09-11, and both are the kind of law that is
// kept by a test or not at all: one is about a colour that is easy to reach for
// and the other is about a ladder that is easy to grow an `if` on.

// COLOUR IS STROKE AND NEVER FILL: NO ROW OF A QUESTION IS PAINTED IN THE
// QUESTION HUE (colour pick C).
//
// The amber belongs to the three marks — `?`, the pointer `▸`, the recommended
// `◆` — and to the keys' own payload hue. Every word on a question is ordinary
// ink, every aside dim, the frame's edge dim, and the row the pointer stands on
// takes the GROUND ladder's `selected` rather than a colour of its own. A row
// painted amber is a row shouting the one thing the marks already say, and the
// block spent a year doing it: `a.pal.ask(head)`, `askBold` on every answer's
// key, the whole offer row in the question hue.
func TestNoQuestionRowIsPaintedInTheQuestionHue(t *testing.T) {
	// The escape that TURNS the question hue on, read off the palette rather
	// than typed here, so a hue that moves cannot leave this law testing an
	// old number.
	painted := newPalette(tokens.TrueColor, false).warn("x")
	amber, _, ok := strings.Cut(painted, "x")
	if !ok || amber == "" {
		t.Fatalf("the palette paints nothing for the question hue: %q", painted)
	}
	for _, shape := range []struct {
		name string
		q    session.Question
	}{
		{"a permission", consentAsk()},
		{"a choice with weight", session.Question{
			ID: 81, Kind: session.QuestionAsk, Ask: session.AskChoice,
			Asker: session.Asker{Kind: session.AskerModel}, Head: "Which storage?",
			Reason: "three ways work here", Stakes: session.StakesReversible,
			Options: []session.AnswerOption{
				{Key: "1", Label: "SQLite", Consequence: "one file beside the conversation"},
				{Key: "2", Label: "JSONL", Consequence: "append-only"},
			},
			Pick: &session.Pick{Key: "1", Reason: "it survives a crash mid-write"},
		}},
		{"a question with no weight at all", session.Question{
			ID: 82, Kind: session.QuestionAsk, Ask: session.AskChoice,
			Asker: session.Asker{Kind: session.AskerModel}, Head: "publish it?",
			Reason: "nobody has read it", Stakes: session.StakesReversible,
			Options: []session.AnswerOption{{Key: "1", Label: "publish"}, {Key: "2", Label: "hold"}},
		}},
	} {
		lab := newQuestionLab(t)
		lab.a.width = 110
		lab.raise(shape.q)
		for i, row := range lab.rows() {
			// The marks are allowed the hue and nothing else is, so they come
			// out before the row is read.
			bare := row
			for _, mark := range []tokens.GlyphID{tokens.GNeedsHuman, tokens.GPointer, tokens.GRecommended, tokens.GSettled} {
				glyph := lab.a.icon(mark)
				bare = strings.ReplaceAll(bare, lab.a.pal.warnBold(glyph), "")
				bare = strings.ReplaceAll(bare, lab.a.pal.warn(glyph), "")
			}
			if strings.Contains(bare, amber) {
				t.Fatalf("%s paints row %d in the question hue:\n%q", shape.name, i, bare)
			}
		}
	}
}

// AND THE CHOOSER IS ONE LADDER OF PROPERTIES (the chooser, questionchooser.go).
//
// Which drawing a question gets is decided in ONE place, by what the question
// CARRIES rather than by which lane raised it — a kind is where a question came
// from, and it says nothing about how much room the decision needs. The ladder
// is data so that it can be read: every rung states why it stands above the next.
func TestTheChooserIsOneLadderOfProperties(t *testing.T) {
	if len(questionLadder) < 5 {
		t.Fatalf("the ladder has %d rungs; it is the whole decision", len(questionLadder))
	}
	for i, rung := range questionLadder {
		if strings.TrimSpace(rung.why) == "" {
			t.Fatalf("rung %d states no reason for standing where it does", i)
		}
		if rung.when == nil {
			t.Fatalf("rung %d asks nothing", i)
		}
	}
	// THE LAST RUNG IS TOTAL: a ladder whose bottom rung can say no is a ladder
	// with an undrawn question under it.
	if last := questionLadder[len(questionLadder)-1]; !last.when(nil, questionShown{}, 0) {
		t.Fatal("the ladder's last rung can refuse, which leaves a question with no drawing")
	}
	// AND IT NEVER ASKS THE KIND. Two questions carrying the same things get the
	// same drawing whichever lane raised them.
	lab := newQuestionLab(t)
	lab.a.width = 100
	weighed := session.Question{
		ID: 83, Ask: session.AskChoice, Asker: session.Asker{Kind: session.AskerModel},
		Head: "Which storage?", Reason: "three ways work", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "SQLite", Consequence: "one file"},
			{Key: "2", Label: "JSONL", Consequence: "append-only"},
		},
	}
	kinds := []session.QuestionKind{session.QuestionAsk, session.QuestionTask, session.QuestionStanding}
	want := questionView(0)
	for at, kind := range kinds {
		q := weighed
		q.Kind = kind
		got := lab.a.questionViewOf(questionShown{question: q, shown: lab.a.now()}, lab.a.width)
		if at == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("%q takes view %d and %q takes view %d on the same evidence",
				kinds[0], want, kind, got)
		}
	}
	// AND THE EVIDENCE DOES DECIDE: the same question with nothing to weigh is a
	// row, and with a command to read is the panel.
	bare := weighed
	bare.Options = []session.AnswerOption{{Key: "1", Label: "SQLite"}, {Key: "2", Label: "JSONL"}}
	if got := lab.a.questionViewOf(questionShown{question: bare, shown: lab.a.now()}, lab.a.width); got != viewRow {
		t.Fatalf("a question with nothing to weigh takes view %d, want the row", got)
	}
	if want == viewRow {
		t.Fatal("a question whose answers carry consequences took the row")
	}
	// AND A NARROW FRAME OUTRANKS ALL OF IT.
	if got := lab.a.questionViewOf(questionShown{question: weighed, shown: lab.a.now()}, 40); got != viewPhone {
		t.Fatalf("a forty-column frame takes view %d, want the phone sheet", got)
	}
}

// AND EVERY VIEW SETS EVERY ROW TO THE FRAME IT WAS GIVEN. A row wider than the
// terminal is a row that wraps, and a block that wraps moves the box under
// somebody's hands.
func TestEveryQuestionViewFitsTheFrameItWasGiven(t *testing.T) {
	for _, width := range []int{56, 96, 120} {
		lab := newQuestionLab(t)
		lab.a.width = width
		lab.raise(session.Question{
			ID: 84, Kind: session.QuestionAsk, Ask: session.AskChoice,
			Asker: session.Asker{Kind: session.AskerModel},
			Head:  "Which storage should the session index sit on, and for how long?",
			Reason: "The index needs somewhere to live between launches, and three ways " +
				"work here with nothing to choose between them on cost.",
			Stakes: session.StakesReversible,
			Options: []session.AnswerOption{
				{Key: "1", Label: "SQLite", Consequence: "one file beside the conversation", Body: "already a dependency"},
				{Key: "2", Label: "JSONL", Consequence: "append-only, no new dependency"},
				{Key: "3", Label: "BoltDB", Consequence: "fastest reads · adds a dependency"},
			},
			Pick: &session.Pick{Key: "1", Reason: "it survives a crash mid-write", Confidence: session.ConfidenceFairly},
		})
		for i, row := range lab.rows() {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("at %d columns row %d is %d cells: %q", width, i, got, ansi.Strip(row))
			}
		}
	}
}
