package tui3

import (
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// `x` — THE ANSWERS LAID AGAINST EACH OTHER, AND ONLY WHERE THEY DIFFER.
//
// docs/design/questions/DESIGN.md: "x compare on the asker's dimensions
// (fallback: +/− lines)", and "compare stacks under 80 cols".
//
// WHY DIFFERENCES ONLY. Three answers with eight axes is a twenty-four cell
// table of which perhaps five cells carry the decision, and a person reading the
// other nineteen is a person reading for the third time that all three answers
// run on this machine. An axis every answer agrees on is not a comparison, it is
// context — and the row it would occupy is the row the axis that DOES decide the
// question has to be found in. So an axis whose readings are all the same is
// dropped, and the foot says so, because a table that silently hid rows would be
// a table nobody could trust.
//
// THE FALLBACK IS THE CONSEQUENCE LINES. Most askers will not fill in
// [session.AnswerOption.Dimensions] — it is the most structured field on the
// object and the last one a model reaches for — but nearly every one of them
// writes the `+ …` and `− …` lines under an answer, because those are what a
// person asks for out loud. Those lines ARE axes with the axis name left off: a `+`
// is something this answer gains and a `−` is something it costs, so the
// fallback lays them out as two rows and the table is worth opening on a
// question nobody wrote a dimension for.
//
// ── THE ROWS, AT 92 COLUMNS ──
//
//	‹ back
//	? which store should the ledger sit on?
//
//	              1 postgres          2 sqlite            3 a file per day
//	  runs        a server            in the file         nothing
//	  backup      one dump            copy the file       copy the folder
//	  reporting   direct              through a copy      no
//
//	  only what differs is here · x back to the answers
//
// And under 80 columns the same table stacks, one answer at a time, because a
// column that has been cut to nine characters is a column that lies:
//
//	  1 postgres
//	    runs        a server
//	    backup      one dump
//	  2 sqlite
//	    runs        in the file

const (
	// questionAxisColumn is how wide the axis names' column is. It is a fixed
	// number rather than the longest name because the table has to keep its
	// columns when a row is long, and an axis called "what it costs to run this
	// in production" would otherwise take the table with it.
	questionAxisColumn = 12
	// questionCellFloor is the narrowest a value column may be before the table
	// stacks. Below it a reading is an ellipsis with two letters in front of it.
	questionCellFloor = 10
	// questionGainWord and questionCostWord are the two axes the fallback
	// derives, and they are in the person's words rather than in `+`/`−`, which
	// are the marks the CELLS wear.
	questionGainWord = "gains"
	questionCostWord = "costs"
)

// questionCompareRows is the whole table, or the sentence that stands where it
// would be when there is nothing to compare.
func (a *app) questionCompareRows(width int) []string {
	room := a.qroom
	if room == nil {
		return nil
	}
	axes, cells := questionAxes(room.head.question)
	if len(axes) == 0 {
		word := questionCompareNone
		if questionHasDimensions(room.head.question) {
			word = questionCompareSame
		}
		return []string{questionIndent + a.pal.dim(fit(word, max(1, width-2)))}
	}
	inner := width - len(questionIndent)
	columns := len(room.head.question.Options)
	cell := 0
	if columns > 0 {
		cell = (inner - questionAxisColumn) / columns
	}
	if width < questionCompareFloor || cell < questionCellFloor {
		return a.questionCompareStack(axes, cells, width)
	}
	return a.questionCompareTable(axes, cells, cell, width)
}

// questionCompareTable is the side-by-side shape.
func (a *app) questionCompareTable(axes []string, cells map[string]map[string]string, cell, width int) []string {
	room := a.qroom
	out := make([]string, 0, len(axes)+3)
	head := strings.Repeat(" ", questionAxisColumn)
	for _, opt := range room.head.question.Options {
		head += questionCell(opt.Key+" "+strings.TrimSpace(opt.Label), cell)
	}
	out = append(out, questionIndent+a.pal.dim(fit(head, max(1, width-2))))
	for _, axis := range axes {
		// The axis name is dim and the readings are ink: the name is the index and
		// the readings are what a person came here to compare.
		line := a.pal.dim(questionCell(axis, questionAxisColumn))
		for _, opt := range room.head.question.Options {
			line += a.pal.ink(questionCell(cells[axis][opt.Key], cell))
		}
		out = append(out, questionIndent+fit(line, max(1, width-2)))
	}
	out = append(out, "")
	out = append(out, questionIndent+a.pal.dim(fit(questionCompareOnly, max(1, width-2))))
	return out
}

// questionCompareStack is the same readings one answer at a time, which is what
// a narrow window and the reader tier both get.
func (a *app) questionCompareStack(axes []string, cells map[string]map[string]string, width int) []string {
	room := a.qroom
	out := make([]string, 0, len(axes)*len(room.head.question.Options)+2)
	for _, opt := range room.head.question.Options {
		out = append(out, questionIndent+a.pal.ink(fit(opt.Key+" "+strings.TrimSpace(opt.Label), max(1, width-2))))
		for _, axis := range axes {
			reading := cells[axis][opt.Key]
			if strings.TrimSpace(reading) == "" {
				continue
			}
			line := questionCell(axis, questionAxisColumn) + reading
			out = append(out, questionBodyIndent+a.pal.dim(fit(line, max(1, width-len(questionBodyIndent)))))
		}
	}
	out = append(out, "")
	out = append(out, questionIndent+a.pal.dim(fit(questionCompareOnly, max(1, width-2))))
	return out
}

// questionCell pads or cuts one cell to its column. It measures the PAINTED
// width, because a cell is padded before it is painted and a column measured any
// other way is a column that moves when a value happens to be the pick.
func questionCell(text string, width int) string {
	if width <= 2 {
		return ""
	}
	// TWO CELLS OF GUTTER AND NOT ONE. A reading that fills its column to within
	// a single space of the next one reads as one long phrase — `2 sqlite beside
	// the project 3 a file per day` was a header row that had stopped being two
	// headings.
	text = fit(strings.TrimSpace(text), width-2)
	if pad := width - ansi.StringWidth(text); pad > 0 {
		return text + strings.Repeat(" ", pad)
	}
	return text
}

// questionAxes is what the table has rows for and what is in each cell.
//
// IT RETURNS THE DIFFERENCES AND NOTHING ELSE, which is the header's law: an
// axis every answer reads the same on is dropped here rather than drawn grey.
// The order is the asker's where the asker gave one — the first answer's map is
// unordered, so the axes are sorted, which at least makes the table the same
// table every time it is opened.
func questionAxes(q session.Question) ([]string, map[string]map[string]string) {
	cells := map[string]map[string]string{}
	put := func(axis, key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if cells[axis] == nil {
			cells[axis] = map[string]string{}
		}
		cells[axis][key] = strings.TrimSpace(value)
	}
	for _, opt := range q.Options {
		for axis, value := range opt.Dimensions {
			put(strings.TrimSpace(axis), opt.Key, value)
		}
	}
	if len(cells) == 0 {
		// THE FALLBACK: the `+`/`−` lines an asker actually writes, read as two
		// axes. It runs only where no dimension was given at all, because an asker
		// that gave both meant the dimensions.
		for _, opt := range q.Options {
			gains, costs := questionConsequenceLines(opt)
			put(questionGainWord, opt.Key, strings.Join(gains, ", "))
			put(questionCostWord, opt.Key, strings.Join(costs, ", "))
		}
	}
	axes := make([]string, 0, len(cells))
	for axis, readings := range cells {
		if questionAllSame(readings, len(q.Options)) {
			continue
		}
		axes = append(axes, axis)
	}
	sort.Strings(axes)
	return axes, cells
}

// questionAllSame reports whether every answer read the same on one axis. An
// axis only SOME answers carry is a difference — the ones that did not carry it
// are the difference — so a partial row is kept.
func questionAllSame(readings map[string]string, options int) bool {
	if len(readings) < options {
		return false
	}
	first := ""
	for _, value := range readings {
		if first == "" {
			first = value
			continue
		}
		if value != first {
			return false
		}
	}
	return true
}

// questionConsequenceLines splits an answer's consequence into what it gains and
// what it costs, by the marks the asker wrote them with.
//
// THE MARKS ARE THE DIFF'S OWN and are read as either the ASCII the asker typed
// or the vocabulary's minus sign, because a model writing a consequence list
// types whichever its tokenizer reaches for and neither is wrong.
func questionConsequenceLines(opt session.AnswerOption) (gains, costs []string) {
	body := opt.Consequence
	if strings.TrimSpace(opt.Body) != "" {
		body += "\n" + opt.Body
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "+"):
			gains = append(gains, strings.TrimSpace(strings.TrimPrefix(line, "+")))
		case strings.HasPrefix(line, "-"):
			costs = append(costs, strings.TrimSpace(strings.TrimPrefix(line, "-")))
		case strings.HasPrefix(line, "−"):
			costs = append(costs, strings.TrimSpace(strings.TrimPrefix(line, "−")))
		}
	}
	return gains, costs
}

// questionConsequenceAxes counts the derivable axes on one answer, and is what
// [questionHasDimensions] asks before it offers `x`.
func questionConsequenceAxes(opt session.AnswerOption) int {
	gains, costs := questionConsequenceLines(opt)
	return len(gains) + len(costs)
}
