package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// THE MARGIN: ONE COLUMN, THREE SECTIONS, AND A DOOR AT THE FOOT OF THE TWO THAT HAVE ONE.
//
// The column on the right has been this conversation's WORK and nothing else
// (task.go's rail). The other thing a conversation can leave behind it — the
// automations it set up, which go on running while nobody is looking — lived on
// a page a person had to know the name of. Two things come out of a
// conversation and only one of them was on the frame.
//
// So the column carries both. Real rows earn dim labels in the words the
// product already uses:
//
//	tasks
//	⠙ Fix nil-map             #7
//	+ /task
//
//	automations
//	◷ weekly update
//	◷ ci on main
//	+ /automations
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE DOORS ARE GEOGRAPHY AND THE LABELS DESCRIBE CONTENT. The `+` rows
//     teach both places before anything exists; an empty label would spend a row
//     to announce absence, which the emptiness law forbids.
//
//   - THE `+` ROW TYPES, IT DOES NOT ARM. Pressing it puts the slash word and a
//     space in the draft and hands the keyboard back — visible, deletable text,
//     no mode, no form. The mechanism a person can see is the mechanism they can
//     learn: the next time they want one they type the word themselves.
//
//   - TWO ROWS OF PERMANENT INK AND NO MORE. The `+` rows are the whole of what
//     this file adds to a column that is otherwise the session's own news.
//     Anything else worth saying about an automation is on the place, one press
//     away.
//
//   - THE GLYPH CARRIES THE STATE, through the one door every mark comes
//     through (docs/design/icons/DESIGN.md): running, paused, or what its last
//     run came to. The title is calm.

// The margin's own words. Both labels are words this product already uses for
// these two things, and both are quoted in internal/manual/chat's pages exactly
// as they are spelled here.
const (
	// marginTasksWord and marginStandWord are the two section labels: dim,
	// lowercase, and the same two nouns the rest of the product calls these
	// things — the roster is tasks, and what runs on the clock is automations.
	marginTasksWord = "tasks"
	marginStandWord = placeAutomationsWord
	// marginJobsWord is the jobs section's label: the same noun the rest of
	// the product calls background work, dim and lowercase like the two
	// above. It has no `+` door — a job is started by a tool, not typed.
	marginJobsWord = "jobs"
	// marginTaskType and marginStandType are what a `+` row TYPES, trailing space
	// and all. They are the commands themselves rather than a spelling of them,
	// because the row is teaching the command: what lands in the box is what a
	// person would have typed.
	marginTaskType  = "/task "
	marginStandType = "/" + placeAutomationsWord + " "
	// marginDoorMark is what a door row leads with. A `+` is the one mark on this
	// surface that already means "one more of these" (the strip's own +N counts
	// what it could not draw), and it is what keeps the row from reading as a
	// third task.
	marginDoorMark = "+ "
	// marginStandMoreWord is what the automations label calls the ones the
	// column had no room for ([app.marginStandHead]). It is the band's own word for
	// the same fact (`+2 more`, sidecol.go), because a column that said `hidden`
	// in one place and `more` in another would be two vocabularies for "there
	// is another page of this".
	marginStandMoreWord = "more"
)

// marginStandCost is what this block spends before it has drawn a single
// automation: the `+ /task` door, which is drawn whatever else the column can
// afford, then the blank line that separates the two sections, the
// `automations` label, and the `+ /automations` door. It is counted here rather than measured because the rows
// are built in [app.marginRows] and a block that guessed its own height would be
// a column measured twice.
const marginStandCost = 4

// marginStandMax is the most automations the column draws at once, however
// tall the frame is. Three is a glance; past three the rows stop being read one
// at a time, and what a person wants then is the place `+ /automations` types
// the command for.
// The rest are counted on the label rather than dropped in silence
// ([app.marginStandHead]).
const marginStandMax = 3

// marginJobsCost is what the jobs section spends before its first job row:
// the blank that separates it from what is above, and the label. Collapsed
// that is the whole section; expanded, running work draws on top of it and
// history fills whatever is left ([marginJobsFit]).
const marginJobsCost = 2

// marginDoorWord is one door row as it is drawn.
func marginDoorWord(typed string) string { return marginDoorMark + strings.TrimSpace(typed) }

// ── what this conversation set up ───────────────────────────────────────────

// marginStanding is the automations this conversation set up, in the place's
// own order — the watcher's reading of the store, which every window already
// takes (automationwatch.go), so the column asks nothing of the disk.
func (a *app) marginStanding() []automation.Automation {
	if !a.autos.on() || strings.TrimSpace(a.file) == "" {
		return nil
	}
	var here []automation.Automation
	for _, item := range a.watch.list {
		if item.Status != automation.StatusFinished && sameTranscript(item.Origin.Transcript, a.file) {
			here = append(here, item)
		}
	}
	return here
}

// marginStandingShows reports whether the column carries the automations
// section at all — which is whether this window can read automations.
func (a *app) marginStandingShows() bool { return a.autos.on() }

// ── the rows ────────────────────────────────────────────────────────────────

// marginHead leaves one quiet row between the pinned hide control and tasks.
// An empty sidebar keeps its task action directly below the control.
func (a *app) marginHead(width int, hasTasks bool) []railLine {
	if !hasTasks {
		return nil
	}
	return []railLine{{entry: -1}}
}

// marginRows is everything the column draws UNDER this conversation's work: the
// tasks section's own door, then the standing section, then the jobs section.
//
// The blank line between sections is the separation this surface always uses —
// whitespace, never a rule, which is the design law a border would break — and
// it is counted like every other line, because a row the layout drew and did not
// count is a row the conversation pays for twice (task.go's [app.railView]).
//
// THIS BLOCK IS RESERVED AND NEVER SCROLLED (task.go's [app.railView] takes its
// height out of the column before the roster's window is measured), so it is
// asked how many rows it may have and it answers with a block that fits. What it
// gives up under pressure is stated by [marginStandFit] and [marginJobsFit]:
// standing orders go first, one at a time, and jobs keep every running one and
// count the history they could not fit. Live work is reserved before standing
// spends ([app.jobSectionMin]), which is the same trade the roster's own live
// head makes.
func (a *app) marginRows(width, room int) []railLine {
	if room < 1 {
		return nil
	}
	out := []railLine{{
		text:  a.marginDoorLine(marginTaskType, width),
		entry: -1,
		door:  marginTaskType,
	}}
	jobsMin := a.jobSectionMin()
	standRoom := room - jobsMin
	if a.marginStandingShows() {
		stand := a.marginStanding()
		shown := marginStandFit(len(stand), standRoom)
		skip := (len(stand) == 0 && standRoom < marginStandCost-1) ||
			(len(stand) > 0 && shown < 1)
		if !skip {
			out = append(out, railLine{entry: -1})
			if len(stand) > 0 {
				out = append(out, railLine{text: a.marginStandHead(width, len(stand)-shown), entry: -1})
			}
			for _, item := range stand[:shown] {
				out = append(out, railLine{
					text:  a.marginStandRow(item, width),
					entry: -1,
					stand: item.ID,
				})
			}
			out = append(out, railLine{
				text:  a.marginDoorLine(marginStandType, width),
				entry: -1,
				door:  marginStandType,
			})
		}
	}
	return append(out, a.jobSection(width, room-len(out))...)
}

// railWorkFloor is the rows the ROSTER keeps before anything else on this column
// is reserved a single one.
//
// A COLUMN THAT CANNOT SHOW THE WORK HAS NOTHING TO PUT UNDER THE WORK. Six is
// the pinned live head and a few rows beneath it — enough to read what is
// happening — and under a frame that short the two sections below give way
// entirely rather than each taking a slice of a column that has none to give.
// That is the same trade the roster's own live head makes ([app.railView] pins
// what is moving and lets the rest scroll): what is happening outranks the
// furniture around it.
const railWorkFloor = 6

// marginRoomFor is how many rows the sections under the roster may reserve,
// given the rows the column has left once the footer has taken its own and the
// lines the roster has to draw.
//
// THE FLOOR COMES OUT FIRST AND THE BLOCK LIVES ON WHAT IS ABOVE IT. A roster
// with nothing in it lends the whole column, which is why a fresh session still
// draws both doors and every order over it; a roster with two hundred lines in it
// lends whatever is over [railWorkFloor], and the block spends that in the order
// [marginStandFit] and [marginJobsFit] state.
func marginRoomFor(avail, work int) int { return max(0, avail-min(work, railWorkFloor)) }

// marginStandFit is how many orders the standing section draws in the rows it has
// been given, and it is the whole of what this column gives up when it is short.
//
// [marginStandCost] IS WHAT THE BLOCK SPENDS BEFORE ITS FIRST ORDER, so what is
// left over is what the orders get. A section that cannot show one order shows
// none at all (see [app.marginRows]).
//
// AND IT NEVER TAKES MORE THAN [marginStandMax], however tall the frame is. The
// column is a glance at what governs this conversation and not the list of it:
// past a handful the rows stop being read one by one, and the list a person wants
// then is the page the door beneath them types the command for. The rest are
// counted on the label rather than dropped in silence.
func marginStandFit(orders, room int) int {
	// An empty section can still show its door in fewer rows than a populated
	// section needs. Its item count must never become a negative slice bound.
	return max(0, min(orders, min(marginStandMax, room-marginStandCost)))
}

// marginJobsFit is how many finished jobs the jobs section draws in the rows
// it has been given, and it is [marginStandFit]'s law applied to a second
// section rather than a second budget algebra.
//
// [marginJobsCost] IS WHAT THE SECTION SPENDS BEFORE ITS FIRST JOB ROW, and
// every running job then draws, always — live work is not a thing this
// column gives up. What is left over is what history gets. When history
// cannot all fit, one of those leftover rows is the `▸ N earlier` count
// rather than a silently dropped job (jobsview.go).
func marginJobsFit(settled, room, live int) int {
	left := max(0, room-marginJobsCost-live)
	if settled <= left {
		return settled
	}
	if left < 1 {
		return 0
	}
	return left - 1
}

// marginStandHead is the standing section's label, with the count of the orders
// this column could not fit riding on it.
//
//	standing              every order it has is on screen
//	standing · 7 more     and the shape when they are not
//
// IT IS THE TASKS LABEL'S OWN SHAPE (`tasks · 3 working`, [app.marginHead]): one
// vocabulary for the two sections of one column, the figure in the data ink and
// the words around it dim. The emptiness law keeps the tail off the ordinary
// case — a section showing everything it has says nothing about what it is not
// hiding.
func (a *app) marginStandHead(width, hidden int) string {
	if hidden <= 0 {
		return a.pal.dim(fit(marginStandWord, width))
	}
	tail := " " + marginStandMoreWord
	plainTail := " · " + itoa(hidden) + tail
	if ansi.StringWidth(marginStandWord+plainTail) > width {
		return a.pal.dim(fit(marginStandWord, width))
	}
	room := max(0, width-ansi.StringWidth(plainTail))
	return a.pal.dim(fit(marginStandWord, room)+" · ") + a.pal.data(itoa(hidden)) + a.pal.dim(tail)
}

// marginDoorLine is one of the column's two doors — `+ /task` and `+ /standing`
// — as it is drawn.
//
// THE `+` IS THE CONTROL AND THE WORDS ARE THE LABEL, which is the same split
// the standing column's own door already makes ([app.railDoorLine]): the chord
// is a sentence for the hand that types, and the mark is what the hand that
// points presses. So the mark takes the accent for exactly as long as the
// pointer is on the row, and the words stay dim throughout.
//
// This is the second half of the emphasis law, which these two rows had been
// drawing only the first half of: the pointer raised the whole row's ground
// (task.go's [app.railRows]) and nothing in the row's own lead answered, so the
// louder cue was the quieter one and the mark said the same thing on every
// frame. It is the accent BUDGET that makes it safe — the mark is lit only while
// the pointer is on it, so the column never carries two.
func (a *app) marginDoorLine(typed string, width int) string {
	mark := a.pal.dim(marginDoorMark)
	if a.hoveringMarginDoor(typed) {
		mark = a.pal.accent(marginDoorMark)
	}
	word := strings.TrimSpace(typed)
	return mark + a.pal.dim(fit(word, max(0, width-ansi.StringWidth(marginDoorMark))))
}

// marginStandRow is one automation as one line: the mark its state wears and
// what it is called, in the muted voice — a task that is running is the news on
// this column and wears the ink for it (task.go's [app.railTitle]); an
// automation is a thing that is quietly on the clock.
func (a *app) marginStandRow(item automation.Automation, width int) string {
	glyph := a.icon(automationRowGlyph(item, a.watch.active))
	room := width - ansi.StringWidth(glyph) - 1
	if room < 1 {
		return glyph
	}
	return a.pal.dim(glyph) + " " + a.pal.muted(fit(strings.TrimSpace(item.Title), room))
}

// ── the pointer ─────────────────────────────────────────────────────────────

// marginPress answers a press on one of the margin's own lines, and reports
// whether it took it — with whatever that press owes the loop. The rows this
// column has always had are answered above it (room.go's [app.railPress]); what
// is left here is an automation, a job, the jobs label, and the two doors.
//
// IT CARRIES A COMMAND BECAUSE ONE OF THESE ROWS STARTS A READING. A job's row
// opens that job's page, and the page's log is read on a beat rather than once
// (jobpage.go) — so a press that answered only "taken" would open a live job's
// page on a single frozen reading under a clock still counting up. The other
// rows owe nothing and say so.
func (a *app) marginPress(line railLine) (tea.Cmd, bool) {
	switch {
	case line.jobs:
		a.railWhere = railSpot{jobs: true}
		a.toggleJobs()
		return nil, true
	case line.job != 0:
		a.railWhere = railSpot{job: line.job}
		return a.openJobPage(line.job), true
	case line.door != "":
		a.marginType(line.door)
		return nil, true
	case line.stand != "":
		// THE ROW OPENS THE PLACE ON ITSELF. Everything a person can do to an
		// automation is a key on that place (place_automations.go), and a column
		// two cells from a paragraph is not the place to grow a second set of
		// them.
		return a.openAutomationsAt(line.stand), true
	}
	return nil, false
}

// marginType is what a `+` row does: the slash word into the draft, the keyboard
// back to the box.
//
// IT GOES AT THE HEAD OF THE LINE AND KEEPS WHAT WAS THERE. A slash is only a
// command as the first thing on a line (input.go's [app.enterLine]), so a word
// dropped at the caret would be a command that never ran — and a person half-way
// through saying what the work is has said the useful half already: `fix the
// flaky test` with `/task ` put in front of it is exactly the line they were
// going to type.
//
// PRESSING IT TWICE IS PRESSING IT ONCE. The word is already there, and a draft
// reading `/task /task ` is the gesture arguing with itself.
func (a *app) marginType(typed string) {
	// THE ROSTER GIVES THE KEYBOARD BACK, because the whole point of the row is
	// that the person is now typing: a column still holding the keys would eat the
	// first letter of the sentence they just asked for (task.go's [app.railTake]).
	a.railTake(false)
	a.closeLists()
	draft := strings.TrimLeft(string(a.input.value), " ")
	if !strings.HasPrefix(draft, typed) {
		draft = typed + draft
	}
	a.input.setText(draft)
	a.touch()
}

// marginDoorAt and marginStandAt are the pointer's half of the two answers
// above: the set that LIGHTS has to be the set the press acts on, or a row
// brightens and then does nothing (hover.go's own law). Both resolve through the
// layout, because where a line was drawn is a fact only the layout has (task.go's
// [app.railDoorAt] makes the same bargain one line down).
func (a *app) marginDoorAt(x, y int) (string, bool) {
	line, ok := a.marginLineAt(x, y)
	if !ok || line.door == "" {
		return "", false
	}
	return line.door, true
}

func (a *app) marginStandAt(x, y int) (string, bool) {
	line, ok := a.marginLineAt(x, y)
	if !ok || line.stand == "" {
		return "", false
	}
	return line.stand, true
}

func (a *app) marginLineAt(x, y int) (railLine, bool) {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return railLine{}, false
	}
	return a.railLineAt(y)
}

// hoveringMarginDoor and hoveringMarginStand are what the paint asks: is THIS
// line the one under the pointer. They are asked by word and by id rather than
// by screen row for [hoverAt]'s own reason — the column is rebuilt every frame,
// and a hover stored as a row of it would follow the scroll instead of the thing.
func (a *app) hoveringMarginDoor(typed string) bool {
	return a.hot.kind == hoverMarginDoor && a.hot.key == typed
}

func (a *app) hoveringMarginStand(id string) bool {
	return a.hot.kind == hoverMarginStand && a.hot.key == id
}
