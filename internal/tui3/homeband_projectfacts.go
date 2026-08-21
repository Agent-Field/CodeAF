package tui3

// THE PROJECT CARD'S LAST BAND: THE DIM ARITHMETIC.
//
// One line, under everything, saying how big this project is and when anybody
// was last in it:
//
//	12 conversations · 34 tasks · spent $4.10 · last active 2h
//
// IT IS [homeFacts] SAID ABOUT A PROJECT INSTEAD OF A CONVERSATION, and it
// keeps that function's law to the letter: EVERY CLAUSE IS OMITTED WHEN IT IS
// NOT ONE. A project nobody has run a task in says nothing about tasks; one
// that has spent nothing says nothing about spending; and a line with nothing
// to say is not drawn at all, so a brand-new project's card is a name, a place,
// and no footer — never `0 tasks · $0.00`.
//
// The sums are taken over the project's own sessions ([session.TaskRollup] is
// already rolled up per conversation by the world's reader), so this band reads
// nothing from disk and cannot block.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func init() {
	registerHomeBand(homeBand{
		name:  "projectfacts",
		order: bandOrderSpend,
		kinds: []bandKind{bandKindProject},
		draw:  drawProjectFactsBand,
	})
}

func drawProjectFactsBand(a *app, ctx bandContext) []string {
	project, ok := bandProjectOf(ctx.subject)
	if !ok {
		return nil
	}
	facts := projectFacts(project, ctx.now)
	if facts == "" {
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, strings.Split(facts, " · ")...)
}

// projectFacts is that line, or "" when the project has nothing to say.
func projectFacts(project session.Project, now time.Time) string {
	var parts []string
	if count := len(project.Sessions); count > 0 {
		parts = append(parts, itoa(count)+plural(" conversation", count))
	}
	tasks, spend := 0, 0.0
	var touched time.Time
	for _, row := range project.Sessions {
		tasks += row.Tasks.Total()
		spend += row.Tasks.Spend
		// The later of "somebody spoke" and "work landed", exactly as
		// [homeFacts] takes it for one conversation: both are this project being
		// active, and the footer is asked when, not how.
		if row.At.After(touched) {
			touched = row.At
		}
		if row.Tasks.Newest.After(touched) {
			touched = row.Tasks.Newest
		}
	}
	if tasks > 0 {
		parts = append(parts, itoa(tasks)+plural(" task", tasks))
	}
	if spend > 0 {
		parts = append(parts, "spent "+dollars(spend))
	}
	if age := sinceAt(touched, now); age != "" {
		parts = append(parts, "last active "+age)
	}
	return strings.Join(parts, " · ")
}
