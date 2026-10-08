package tui3

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE SHAPING QUESTION ────────────────────────────────────────────────────
//
// After the manager shapes a run whose `ask me at` is plan, the runner stops on
// `run these stages? <what the manager set>` of kind `plan`. One long line would
// hide the answer's whole content, so the peek and the item page's waiting
// stage draw it as the stages it asks about:
//
//	? run these stages?
//	  1 read · the diff and its claims
//	  2 review · thorough on security, code and architecture
//	  manager set review: thorough … · why: touches the call row
//	  y run · n keep the recipe · a in words
//
// The digit is the stage's key on the item page, so a stage switched off keeps
// its number and is not drawn. A pane too short for every stage cuts the asks
// first, then ends the stages on `… N more`. Every other `plan` question is
// drawn as before.

// factoryIsShaping says whether the item waits on the shaping question.
func factoryIsShaping(it factory.Item) bool {
	return it.QKind == "plan" && strings.HasPrefix(strings.TrimSpace(it.Question), wordRunTheseStages)
}

// factoryShapingHint is the shaping question's keys.
func factoryShapingHint() string {
	return strings.Join([]string{
		factoryHintClause(keyYes, wordRun),
		factoryHintClause(keyNo, wordKeepTheRecipe),
		factoryHintClause(keyInWords, wordInWords),
	}, rowSep)
}

// factoryShapingBlock is the shaping question as at most room lines of at most
// measure cells: the question row, one line per stage that is on, the Adapted
// line dim, and the keys.
func (a *app) factoryShapingBlock(it factory.Item, measure, room int) []string {
	pal := a.pal
	head := a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), wordRunTheseStages, pal.ink, measure)
	var tail []string
	if adapted := strings.TrimSpace(factory.AdaptedLine(it)); adapted != "" {
		tail = append(tail, factorySpaces(factoryLeadW)+pal.dim(fit(adapted, measure-factoryLeadW)))
	}
	if a.factory.Has("answer") {
		tail = append(tail, factorySpaces(factoryLeadW)+pal.dim(fit(factoryShapingHint(), measure-factoryLeadW)))
	}
	type row struct{ digit, name, ask string }
	var rows []row
	for i, st := range it.Stages {
		if !st.On {
			continue
		}
		rows = append(rows, row{fmt.Sprint(i + 1), strings.TrimSpace(st.Name), strings.Join(strings.Fields(st.Ask), " ")})
	}
	lead := factorySpaces(factoryLeadW)
	space := room - len(head) - len(tail)
	shown, more, cutAsks := rows, 0, false
	if len(rows) > space {
		// Too few rows for every stage: the asks go first, and when the names
		// alone do not fit either, the last stage line says how many more.
		cutAsks = true
		keep := max(space-1, 0)
		shown, more = rows[:keep], len(rows)-keep
	}
	out := append([]string{}, head...)
	for _, r := range shown {
		line := r.digit + " " + r.name
		if r.ask != "" && !cutAsks {
			line += rowSep + r.ask
		}
		out = append(out, lead+pal.ink(fit(line, measure-factoryLeadW)))
	}
	if more > 0 {
		out = append(out, lead+pal.dim(fmt.Sprintf("… %d more", more)))
	}
	return append(out, tail...)
}

// factoryShapingMinRows is the fewest rows the peek hands the shaping block:
// the question, a stage, the keys.
const factoryShapingMinRows = 4
