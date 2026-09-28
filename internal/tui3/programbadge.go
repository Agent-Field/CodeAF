package tui3

// THE PROGRAM'S BADGE: how a person tells work handed to a program codeaf
// carries — senior-dev — from a task this conversation's own worker does.
//
// The two looked the same everywhere a task is named. A senior-dev run and an
// ordinary `/task` drew the same first line on the side list — a state glyph, a
// title, a handle — and the card a person approved said `wants to start a
// task:` for both, so the one thing about the work that decides what it will do
// and how long it will take was not on the screen at all. The owner asked for a
// prominent badge like `[senior-dev]` on the task's row, none on an ordinary
// task's, and a badge of its own for every program that comes after it.
//
// SO THE BADGE IS THE PROGRAM'S NAME IN BRACKETS, AND NOTHING ELSE MAKES IT. A
// program's [session.TaskNotice.Program] is the name its own command row says
// out loud, and the delegate package keeps that name to one plain ASCII shape
// (internal/delegate's nameShape), so a name can be bracketed as it stands. A
// program added next year gets its badge by existing: there is no table here to
// forget to add it to. Its short spelling is the initials of the name's
// hyphen-separated parts — `[sd]`, `[dw]` for a doc-writer — which is what a
// twenty-four-cell column can afford.
//
// THE BRACKETS ARE ALWAYS DRAWN. The badge is painted in the accent, bold,
// because the owner asked for it to be prominent and the accent is this
// surface's one loud voice (docs/DESIGN-LANGUAGE.md's accent budget: it marks
// the live or chosen thing, and a program's work is the one row on a column that
// is a different kind of thing) — but ink is not always there to be read. A
// terminal with no colour draws none; a row somebody is standing in is laid on
// the selected ground, which swallows a chip's; and a screen reader reads words.
// The brackets are what is left in all three, so they are part of the badge and
// never decoration around it ([session.ProgramBadge] spells them once).
//
// IT IS NEVER A LEAD AND NEVER A TARGET. The badge stands after the title, in
// the row's trailing slot — never in front of it, where the column's law keeps
// one state glyph and nothing else (railclick_test.go's
// TestTheColumnLeadsWithStateAndSpendsNoCellOnIdentity) — and it is not a press
// target of its own: a press anywhere on a task's row opens that task.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// programBadge is a program's badge in its spellings, longest first: the whole
// name in brackets and its initials in brackets. A name with nothing to say
// is the unknown field ([rowSay] with no spellings), which every caller draws
// as nothing — the emptiness law, and the whole of what an ordinary task gets.
func programBadge(name string) rowField {
	name = strings.TrimSpace(name)
	full := session.ProgramBadge(name)
	if full == "" {
		return rowField{}
	}
	short := session.ProgramBadge(programInitials(name))
	if short == full {
		short = ""
	}
	return rowSay(full, short)
}

// programInitials is the first character of each hyphen-separated part of a
// program's name: `senior-dev` is `sd`. The name's shape is the delegate
// package's guarantee — lowercase ASCII words joined by single hyphens — so a
// byte is a character here.
func programInitials(name string) string {
	var out strings.Builder
	for _, part := range strings.Split(name, "-") {
		if part != "" {
			out.WriteByte(part[0])
		}
	}
	return out.String()
}

// programSpelling is the longest spelling of a badge that still leaves a title
// the floor of cells beside it — or the whole title, when that is shorter than
// the floor — in room cells, with one cell of air between them. It answers ""
// when no spelling does, which only a row indented deeper than any program's
// row ever sits can meet: the title is the row's identity, and a badge that cost
// it below the floor would be a mark that erased the thing it marks.
func programSpelling(badge rowField, title string, room, floor int) string {
	need := min(ansi.StringWidth(title), floor)
	for _, spelling := range [...]string{badge.full, badge.short} {
		if spelling != "" && room-ansi.StringWidth(spelling)-1 >= need {
			return spelling
		}
	}
	return ""
}

// programCells is what a chosen spelling costs a row: its own cells and the one
// cell of air in front of it, and nothing for no badge.
func programCells(spelling string) int {
	if spelling == "" {
		return 0
	}
	return ansi.StringWidth(spelling) + 1
}

// programInk paints a badge spelling. It is the ONE painter every surface
// draws a program's badge with, so the badge reads the same on the side list,
// the strip, the card, a page's head and the tasks place.
func (p palette) programInk(spelling string) string {
	return p.bold(p.accent(spelling))
}

// programAfter is a badge as it follows a title: one cell of air and the badge,
// painted, or nothing for no badge.
func (p palette) programAfter(spelling string) string {
	if spelling == "" {
		return ""
	}
	return " " + p.programInk(spelling)
}

// programTitled is a title fitted into room cells with its program's badge
// after it, painted — the title in the row's own ink and the badge in its own.
// The badge keeps its long spelling while the title keeps [railTitleFloor]
// cells beside it, falls to its short one after that, and the title is cut into
// whatever is left; an ordinary task's title is simply fitted as it always was.
func (p palette) programTitled(title, program string, room int, ink func(string) string) string {
	spelling := programSpelling(programBadge(program), title, room, railTitleFloor)
	return ink(fit(title, room-programCells(spelling))) + p.programAfter(spelling)
}

// programLabel is [palette.programTitled] for a row that paints its label in
// ONE call it does not own (home's [bandSides] is the one caller): the title
// and the badge as one string fitted into room cells, and an ink that paints
// the title with the row's own and the badge with [palette.programInk]. The
// label is fitted here, before the row sees it, so the row never has to cut it
// and the badge on its end is never what a cut takes.
func (p palette) programLabel(title, program string, room int, ink func(string) string) (string, func(string) string) {
	spelling := programSpelling(programBadge(program), title, room, railTitleFloor)
	if spelling == "" {
		return title, ink
	}
	label := fit(title, room-programCells(spelling)) + " " + spelling
	return label, func(s string) string {
		if head, ok := strings.CutSuffix(s, " "+spelling); ok {
			return ink(head) + p.programAfter(spelling)
		}
		return ink(s)
	}
}

// pageProgram is the program a stored page's task was handed to: the name the
// program's own record gives it, and the name its row carries while that record
// has not reached the disk — "" for every page no program was handed.
func pageProgram(page session.PlanTaskPage) string {
	if page.Program != nil {
		if name := strings.TrimSpace(page.Program.Name); name != "" {
			return name
		}
	}
	return strings.TrimSpace(page.Row.Program)
}

// nodeProgram is the program a node's work was handed to, and "" for every
// ordinary task. It is the node's own fact, taken from the engine's notices
// ([taskNode.program]) — and, for a node whose notices named none because the
// engine that sent them predates the field, the name the run's own plan row
// carries ([app.railProgramRow]), which is a row the surface already holds and
// never a read made while drawing.
//
// ONLY THIS WINDOW'S OWN NODE IS LOOKED UP IN THIS WINDOW'S PLAN ROWS. Those
// rows are the conversation in front's, found by the bare number, and task ids
// restart with every conversation — so a guest page's node (taskowner.go's
// [taskGuestNode]), standing for another conversation's task 7, would take the
// badge of this conversation's own task 7 and wear `[senior-dev]` over work no
// program had. Such a node answers with its own fact and nothing else.
func (a *app) nodeProgram(node *taskNode) string {
	if node == nil {
		return ""
	}
	if node.program != "" {
		return node.program
	}
	if a.tasks[node.id] != node {
		return ""
	}
	if row, ok := a.railProgramRow(node); ok {
		return strings.TrimSpace(row.Program)
	}
	return ""
}
