package tui3

// PICKER SPEND HISTORY — what YOU paid, on the same list that picks what to run.
//
// Every door onto the model picker (/model, settings Providers, task model,
// home draft, composer) shares one [picker]. Catalog prices say what a million
// tokens WOULD cost; the ledger says what a model DID cost on this machine.
// Both belong on the row: a person choosing between two models is often
// choosing between a fortnight's habit and a catalog number.
//
// TWO ADDITIONS, ONE SNAPSHOT:
//
//  1. A dim chip on rows you have used: `this fortnight $12 · 2.1M` — absent
//     when unused (emptiness law), frozen into [picker.held] like via.
//  2. A `used lately` section above the catalog when the filter box is empty,
//     ordered by dollars spent; `/model used` (and the `used` filter term)
//     keeps only those rows.
//
// THE SNAPSHOT IS TAKEN WHEN THE LIST OPENS. A running turn still updates the
// ledger; neither may rewrite a row somebody is reading — the same law as
// [picker.current] and [picker.pin].

import (
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	pickerUsedSectionWord = "used lately"
	pickerAllSectionWord  = "all models"
	pickerSpendDays       = 14
)

// armSpendHistory freezes this machine's last fortnight of model spend onto the
// open picker. It is the single door every opener goes through after start, so
// /model and a settings row cannot disagree about who you have been paying.
func (a *app) armSpendHistory(p *picker) {
	if p == nil || !p.open {
		return
	}
	p.spent = nil
	lines, ok := a.usageSince(session.LastDays(a.now(), pickerSpendDays).From)
	if !ok || len(lines) == 0 {
		return
	}
	priced := make([]session.UsageLine, 0, len(lines))
	win := session.LastDays(a.now(), pickerSpendDays)
	for _, line := range lines {
		if line.USD > 0 && win.Holds(session.UsageLineDay(line)) {
			priced = append(priced, line)
		}
	}
	if len(priced) == 0 {
		return
	}
	p.spent = make(map[string]session.ModelSpend, len(priced))
	for _, row := range session.UsageByModel(priced) {
		if row.Model == "" || !(row.USD > 0) {
			continue
		}
		p.spent[spendModelKey(row.Model)] = row
		// Also key the raw id so a catalog row with a leading ~ still joins.
		p.spent[strings.ToLower(strings.TrimSpace(row.Model))] = row
	}
}

// spendOf is what this list believes one model cost in the fortnight, or a zero
// row when it has never been on the bill.
func (p *picker) spendOf(id string) (session.ModelSpend, bool) {
	if p == nil || len(p.spent) == 0 {
		return session.ModelSpend{}, false
	}
	if row, ok := p.spent[spendModelKey(id)]; ok && row.USD > 0 {
		return row, true
	}
	if row, ok := p.spent[strings.ToLower(strings.TrimSpace(id))]; ok && row.USD > 0 {
		return row, true
	}
	return session.ModelSpend{}, false
}

// spendChipField is the ranked fact drawn on a used model's row.
func spendChipField(row session.ModelSpend) rowField {
	if !(row.USD > 0) {
		return rowField{}
	}
	money := spendMoneyWord(row.USD)
	if money == "" {
		return rowField{}
	}
	full := "this fortnight " + money
	mid := money
	if row.Tokens > 0 {
		toks := tokenWord(row.Tokens)
		full += " · " + toks
		mid += " · " + toks
	}
	return rowSay(full, mid, money)
}

// promoteUsed reorders hits so models on the bill lead the empty-box list,
// dearest first. A typed filter keeps ordinary ranking — promoting mid-search
// would yank the cursor away from the row a person was walking toward.
func (p *picker) promoteUsed() {
	if p == nil || len(p.spent) == 0 || len(p.hits) < 2 {
		return
	}
	tokens, terms := splitQuery(p.filter.String())
	if len(tokens) > 0 || hasOrderingTerm(terms) {
		return
	}
	// `used` already keeps only spenders; still sort them dearest-first.
	sort.SliceStable(p.hits, func(a, b int) bool {
		left, lok := p.spendOf(p.all[p.hits[a]].ID)
		right, rok := p.spendOf(p.all[p.hits[b]].ID)
		switch {
		case lok && !rok:
			return true
		case !lok && rok:
			return false
		case lok && rok && left.USD != right.USD:
			return left.USD > right.USD
		}
		return false
	})
}

func hasOrderingTerm(terms []laneTerm) bool {
	for _, term := range terms {
		if term.kind == termFast || term.kind == termCheap {
			return true
		}
	}
	return false
}

func hasUsedTerm(terms []laneTerm) bool {
	for _, term := range terms {
		if term.kind == termUsed {
			return true
		}
	}
	return false
}

// usedSectionBefore is the dim heading above a stretch of used (or unused)
// models when the empty-box promotion is in effect. Multi-service lists keep
// their own group headings and skip this — two heading systems on one list
// would fight.
func (p *picker) usedSectionBefore(at int) string {
	if p == nil || len(p.spent) == 0 {
		return ""
	}
	tokens, terms := splitQuery(p.filter.String())
	if len(tokens) > 0 || hasOrderingTerm(terms) || hasUsedTerm(terms) {
		return ""
	}
	if at < 0 || at >= len(p.list) || p.list[at].lane != laneNone {
		return ""
	}
	model := p.all[p.hits[p.list[at].hit]]
	if model.Group != "" || model.Unavailable {
		return ""
	}
	_, used := p.spendOf(model.ID)
	if at == 0 || at == p.top {
		if used {
			return pickerUsedSectionWord
		}
		// Scrolled into the unused half: name the catalog so the section is not
		// mistaken for more of "used lately".
		if !used {
			return pickerAllSectionWord
		}
	}
	if at == 0 {
		return ""
	}
	prev := p.list[at-1]
	if prev.lane != laneNone {
		return ""
	}
	before := p.all[p.hits[prev.hit]]
	_, beforeUsed := p.spendOf(before.ID)
	if used && !beforeUsed {
		return pickerUsedSectionWord
	}
	if !used && beforeUsed {
		return pickerAllSectionWord
	}
	return ""
}

// ── order chips on the framed sheet ─────────────────────────────────────────
//
// `cheap`, `fast` and `used` already work as typed filter words (lanes.go). A
// stranger will not invent them. The chip strip under the box is the same
// discovery move as the spend lens bar: name the doors, take a press, keep the
// keyboard path.

// pickOrderWords is the chip strip under `/model`'s filter, left to right.
var pickOrderWords = []string{"cheap", "fast", "used"}

// pickOrderBar draws the order chips and returns each chip's span in the INNER
// content (column 0 is the first cell inside the sheet's side glyphs).
func pickOrderBar(inner int, filter string, pal palette) (string, []hudSpan) {
	active := pickOrderActive(filter)
	line := strings.Repeat(" ", tabLead)
	spans := make([]hudSpan, len(pickOrderWords))
	at := tabLead
	for i, word := range pickOrderWords {
		if i > 0 {
			line += strings.Repeat(" ", tabGap)
			at += tabGap
		}
		chip := tabPad + word + tabPad
		cols := tabChipCols(word)
		spans[i] = hudSpan{from: at, to: at + cols}
		if active[word] {
			line += pal.selected(pal.bold(pal.ink(chip)), cols)
		} else {
			line += pal.dim(chip)
		}
		at += cols
	}
	return fit(line, inner), spans
}

// pickOrderActive is which of the three chip words are already in the filter.
func pickOrderActive(filter string) map[string]bool {
	on := make(map[string]bool, len(pickOrderWords))
	for _, token := range strings.Fields(strings.ToLower(filter)) {
		for _, word := range pickOrderWords {
			if token == word {
				on[word] = true
			}
		}
	}
	return on
}

// toggleFilterWord turns one order chip on or off in the filter box. cheap and
// fast are exclusive (both are order terms; the last one wins, and two lit chips
// would lie about which order is live). used keeps or drops alongside either.
func (p *picker) toggleFilterWord(word string) {
	if p == nil || word == "" {
		return
	}
	word = strings.ToLower(strings.TrimSpace(word))
	fields := strings.Fields(p.filter.String())
	out := make([]string, 0, len(fields)+1)
	had := false
	for _, token := range fields {
		low := strings.ToLower(token)
		if low == word {
			had = true
			continue
		}
		// Selecting cheap drops fast, and the other way — one order at a time.
		if (word == "cheap" && low == "fast") || (word == "fast" && low == "cheap") {
			continue
		}
		out = append(out, token)
	}
	if !had {
		out = append(out, word)
	}
	p.filter.setText(strings.Join(out, " "))
	p.rank()
}
