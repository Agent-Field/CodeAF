package tui3

// THE PROJECT CARD'S FIRST BAND: THE CONVERSATIONS THIS PROJECT HOLDS.
//
// The cursor on a project is a person asking a different question from the one
// they ask on a conversation. On a conversation the question is "what is this
// doing"; on a project it is "what is going on in here" — and the only honest
// answer to that is the list of conversations, in the order home already puts
// them in.
//
// SO THE ROWS ARE THE LEFT COLUMN'S OWN ROWS, drawn again at the card's width.
// The glyph is [homeGlyph], the name is [homeName], the tail is [homeNote] —
// the same three functions the row across the gutter is made of, so a
// conversation cannot be called one thing on the left and another on the right.
// What this band does NOT do is invent an order: [session.Project.Sessions]
// arrives in triage order from the world's own reader (session's sortSessions:
// needs-you, running, incomplete, then recency), which is exactly the order
// [homeView.buildWorld] draws them in with nothing typed. A second ladder here
// would be the same judgement made twice, and the two would drift.
//
// AND IT FOLDS AT [homeShown], which is the count the left column folds at. A
// project with forty conversations is not a card with forty rows on it — the
// card is a glance, the fold is the door, and the number on the door is the
// same number the project's own tail line uses.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func init() {
	registerHomeBand(homeBand{
		name:  "projectsessions",
		order: bandOrderWork,
		kinds: []bandKind{bandKindProject},
		draw:  drawProjectSessionsBand,
	})
}

// projectSessionsWord is the plural noun the fold line says. It is the word a
// person uses for the thing — never "sessions", which is what the folder on
// disk is called and not what the chat inside it is.
const projectSessionsWord = "conversations"

func drawProjectSessionsBand(a *app, ctx bandContext) []string {
	project, ok := bandProjectOf(ctx.subject)
	if !ok || len(project.Sessions) == 0 {
		// THE EMPTINESS LAW: a project bucket with nothing in it draws no band
		// at all rather than a heading over nothing.
		return nil
	}
	rows := make([]string, 0, len(project.Sessions))
	for _, row := range project.Sessions {
		label := homeGlyph(row, ctx.pal.ascii) + " " + homeName(row)
		rows = append(rows, projectCardRow(label, homeNote(row, a.homeHeld(row), ctx.now), ctx.width, ctx.pal))
	}
	return a.bandFold(ctx, "projectsessions", rows, homeShown, projectSessionsWord)
}

// bandProjectOf is the world's own reading of the project under the cursor.
//
// IT MATCHES ON WHATEVER THE SUBJECT WAS GIVEN. The left column carries a
// project's BUCKET directory on its rows ([homeLine.dir]) and the card's place
// line is about its real PATH, and the two are different strings for one
// directory (session's world.go: the bucket name is an encoding of the path and
// decoding it would be guessing). So both are tried, and the display name last
// — a project nothing recorded a path for has only its name, and answering
// nothing for it would blank the card of the very projects that need it most.
func bandProjectOf(subject bandSubject) (session.Project, bool) {
	if subject.kind != bandKindProject {
		return session.Project{}, false
	}
	dir := strings.TrimSpace(subject.dir)
	if dir != "" {
		for _, project := range subject.world.Projects {
			if project.Dir == dir || project.Path == dir {
				return project, true
			}
		}
	}
	if name := strings.TrimSpace(subject.project); name != "" {
		for _, project := range subject.world.Projects {
			if project.Name == name {
				return project, true
			}
		}
	}
	return session.Project{}, false
}

// projectCardRow is one row of a project's card: a name on the left, a dim
// fact on the right, and the name giving way first when the two will not fit.
//
// It is [homeTaskLine]'s arithmetic said about the other kinds of row, and it
// is deliberately NOT [overlayRow]: that draws the two-cell cursor lead every
// row of the LIST column carries, and this column has no cursor of its own
// (homebands.go's [app.toggleAllBandFolds] states the same fact about the keys).
func projectCardRow(label, note string, width int, pal palette) string {
	if width < 1 {
		return ""
	}
	// THE NAME KEEPS A FLOOR AND THE TAIL IS WHAT GIVES WAY FIRST, which is the
	// law [StandingItemRow] states for the left column and for the same reason.
	// A conversation's tail is two or three words, but a standing item's can be
	// a whole sentence a run stopped on — and a row that gave the tail whatever
	// it asked for drew all rollup and no name at all. The floor is
	// [standWordsFloor]'s own figure because it is the same question: how many
	// cells does the thing's own name keep, whatever the fact beside it wants.
	if note != "" {
		switch room := width - standWordsFloor - 1; {
		case room < projectRowFloor:
			// Too narrow to share at all. The NAME takes the line, because a name
			// cut in half is still recognisable and a rollup cut to an ellipsis
			// is not a fact.
			note = ""
		case ansi.StringWidth(note) > room:
			note = fit(note, room)
		}
	}
	room := width
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	if room < projectRowFloor {
		return pal.muted(fit(label, width))
	}
	label = fit(label, room)
	line := pal.muted(label)
	if note != "" {
		gap := width - ansi.StringWidth(label) - ansi.StringWidth(note)
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + pal.dim(note)
	}
	return line
}

// projectRowFloor is the narrowest a row will still split into a name and a
// tail. It is [homeTaskLine]'s own figure, for its own reason: under it the
// name has nothing left and the row is all trailing fact.
const projectRowFloor = 8
