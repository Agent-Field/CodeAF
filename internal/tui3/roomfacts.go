package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Named facts feed both the roomy groups and the compact ranked fallback.
// A different layout must not invent a second reading of task activity.
type roomFactFields struct {
	state, clock, spend, calls, live, model, effort rowField
}

func (a *app) roomFactsOf(node *taskNode) roomFactFields {
	work := roomWorkOf(a.roomEntries())
	model := strings.TrimSpace(node.model)
	return roomFactFields{
		state:  rowSay(a.roomStateWord(node)),
		clock:  rowSay(a.roomClock(node)),
		spend:  rowSay(a.roomSpend(node)),
		calls:  roomCallField(work.calls),
		live:   rowSay(a.roomLiveWord(node, work)),
		model:  rowSay(model, modelBase(model)),
		effort: rowSay(a.taskEffortClause(node)),
	}
}

func (f roomFactFields) ranked() []rowField {
	return []rowField{f.state, f.clock, f.spend, f.calls, f.live, f.model, f.effort}
}

// roomGroupedFacts gives the outcome, activity and setup distinct reading
// groups without spending another row. If the complete groups cannot fit,
// the existing ranked fitter decides which facts survive on the compact row.
func (a *app) roomGroupedFacts(node *taskNode, width int, stop string) (string, bool) {
	f := a.roomFactsOf(node)
	state := strings.TrimSpace(a.roomMark(node) + " " + rowAll([]rowField{f.state}))
	activity := rowAll([]rowField{f.clock, f.calls, f.live})
	setup := a.roomSetupInk(rowAll([]rowField{f.model, f.effort, f.spend}), node)
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
	return tasksPaintTail(text, []tasksFact{
		{field: rowSay(a.roomSpend(node)), ink: a.pal.ink},
		{field: rowSay(model, modelBase(model)), ink: a.pal.narr},
	}, a.pal)
}
