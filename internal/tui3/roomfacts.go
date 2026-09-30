package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Named facts feed both the roomy groups and the compact ranked fallback.
// A different layout must not invent a second reading of task activity.
type roomFactFields struct {
	state, clock, spend, calls, live, model, tokens, effort rowField
}

func (a *app) roomFactsOf(node *taskNode) roomFactFields {
	work := roomWorkOf(a.roomEntries())
	model := strings.TrimSpace(node.model)
	calls := roomCallField(work.calls)
	// A RUN'S TASK COUNTS ITS STEPS AS THE STORE COUNTS THEM (planroom.go): a
	// step the engine answered itself is in the record and in the count, and
	// has no row on the page, so the page's own rows cannot be the count.
	if plan := a.roomPlan(); plan != nil {
		calls = rowSay(planStepWords(plan.page.Row.Steps))
	}
	return roomFactFields{
		state:  rowSay(a.roomStateWord(node)),
		clock:  rowSay(a.roomClock(node)),
		spend:  rowSay(a.roomSpend(node)),
		calls:  calls,
		live:   rowSay(a.roomLiveWord(node, work)),
		model:  rowSay(model, modelBase(model)),
		tokens: rowSay(a.roomTokensWord(node)),
		effort: rowSay(a.taskEffortClause(node)),
	}
}

// roomTokensWord is the tokens a RUN'S TASK has read and written, off the
// store's ledger (planroom.go), and nothing where the ledger names none. A
// node's page does not say it here: a node's tokens are the live column's.
func (a *app) roomTokensWord(node *taskNode) string {
	if a.roomPlan() == nil || node == nil || node.tokens <= 0 {
		return ""
	}
	return tokenWord(node.tokens) + " tok"
}

func (f roomFactFields) ranked() []rowField {
	return []rowField{f.state, f.clock, f.spend, f.calls, f.live, f.model, f.tokens, f.effort}
}

// roomGroupedFacts gives the outcome, activity and setup distinct reading
// groups without spending another row. If the complete groups cannot fit,
// the existing ranked fitter decides which facts survive on the compact row.
func (a *app) roomGroupedFacts(node *taskNode, width int, stop string) (string, bool) {
	f := a.roomFactsOf(node)
	if a.roomOrganized() {
		left := a.taskStateInk(node)(a.roomMark(node) + " " + rowAll([]rowField{f.state}))
		if f.live.known() {
			left += a.pal.muted(rowSep + rowAll([]rowField{f.live}))
		}
		right := a.pal.muted(rowAll([]rowField{f.clock, f.spend, f.tokens}))
		if stop != "" {
			right += "   " + a.pal.ink(stop)
		}
		room := max(width-headLabelAt-2-ansi.StringWidth(right), 0)
		left = fit(left, max(room-2, 0))
		return strings.Repeat(" ", headLabelAt) + left + strings.Repeat(" ", max(room-ansi.StringWidth(left), 0)) + right + "  ", true
	}
	state := strings.TrimSpace(a.roomMark(node) + " " + rowAll([]rowField{f.state}))
	activity := rowAll([]rowField{f.clock, f.calls, f.live})
	setup := a.roomSetupInk(rowAll([]rowField{f.model, f.tokens, f.effort, f.spend}), node)
	left := state
	if activity != "" {
		if left != "" {
			left += "   "
		}
		left += activity
	}
	right := setup
	if stop != "" {
		if right != "" {
			right += "   "
		}
		right += a.pal.dim(stop)
	}
	lead := ansi.StringWidth(state)
	paint := func(s string) string {
		return a.taskStateInk(node)(ansi.Cut(s, 0, lead)) + a.pal.muted(ansi.Cut(s, lead, ansi.StringWidth(s)))
	}
	return a.legendLine(left, right, width, paint)
}

// Add this last air row only after the tab row has gained its own padding.
// Staggering the thresholds keeps a taller terminal from losing reading rows.
func (a *app) roomHeaderPad() int {
	if a.roomOrganized() {
		return 1
	}
	width, height := a.size()
	if width >= 4*roomHeadFloor && height >= 2*airyFloor+8 {
		return 1
	}
	return 0
}

// Price is an accountable figure; the model identifies the setup. Both remain
// regular weight, above the quiet separators and below the task's main title.
// Reuse the task fact painter so compact and grouped headers share the roles.
func (a *app) roomSetupInk(text string, node *taskNode) string {
	if node == nil {
		return a.pal.dim(text)
	}
	model := strings.TrimSpace(node.model)
	return roomPaintTail(text, []roomFact{
		{field: rowSay(a.roomSpend(node)), ink: a.pal.ink},
		{field: rowSay(model, modelBase(model)), ink: a.pal.narr},
	}, a.pal.dim, a.pal)
}

// roomFact is one fact of a room's head line: what it can say, in the spellings
// the fitter chooses between, and the ink it is painted in when it survives.
//
// IT LIVES HERE NOW AND IT USED TO BE THE TASKS PLACE'S. That page drew a ranked
// tail of six facts and this was its shape; the page is a TABLE now
// (taskstable.go), with fixed columns and nothing to paint fact by fact, and a
// `tasks`-named type that only the room used would be a name pointing at a page
// that no longer has one.
type roomFact struct {
	field rowField
	// ink is nil for everything the surface draws dim, which is nearly all of it.
	// A fact with an ink of its own has it because the ink is part of the fact.
	ink func(string) string
}

// roomPaintTail paints a fitted tail fact by fact.
//
// IT PAINTS THE SEPARATORS ITSELF because a hue nested inside a hue ends at the
// inner one's reset (room.go's [app.roomHeadWord] states the same rule), so a
// tail carrying a money figure may not be painted whole. Anything the fitter
// spelled that no fact answers to — the halves of a sentence that had a ` · ` of
// its own inside it — is the dim every other fact wears.
func roomPaintTail(tail string, facts []roomFact, rest func(string) string, pal palette) string {
	if tail == "" {
		return ""
	}
	ink := func(said string) string {
		for _, fact := range facts {
			if fact.ink == nil || said == "" {
				continue
			}
			if said == fact.field.full || said == fact.field.short || said == fact.field.tiny {
				return fact.ink(said)
			}
		}
		return rest(said)
	}
	said := strings.Split(tail, rowSep)
	for i := range said {
		said[i] = ink(said[i])
	}
	return strings.Join(said, pal.dim(rowSep))
}
