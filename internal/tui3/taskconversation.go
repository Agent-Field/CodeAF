package tui3

// taskconversation.go draws a PROGRAM'S task page as the conversation it is.
//
// A task a run handed to a program codeaf carries (senior-dev first) used to
// open on the same page as every other task: a telemetry line, the brief, and a
// list of steps, with a box for a note the program would never read. What the
// program was actually doing was invisible — its work happens inside the calls
// it makes to a model, and every one of those goes through the model API codeaf
// serves the run. So codeaf has the whole exchange, and this page draws it: the
// program on one side, like a very particular person asking codeaf things, and
// the model that answered on the other, one call at a time, with the call in
// flight as the last line while it is out.
//
//	senior-dev          rewrite the auth middleware to use the new session store
//	deepseek-v4-flash   I'll read the middleware first.
//	                    ▤ read internal/auth/middleware.go
//	senior-dev          read: package auth
//	◐ deepseek-v4-flash · 12s
//
// THE PAGE READS NOTHING. Every line here is drawn from the page the surface
// already holds ([session.PlanTaskPage.Program], read off the loop on the page's
// own beat), with the frame's own clock for the two figures that tick — the
// run's age and the call in flight's. The texts arrive cut to their first line
// with the run's copy taken out of their paths (internal/session's
// plandb_program.go), so a row here is a choice of which line to show and never
// a reading of the record.
//
// WHAT IS DRAWN IS THE PERSON'S, NEVER THE MACHINERY'S. A program's system
// prompt and the model's own words handed back to it are part of every call and
// say nothing new, so neither is ever a row; a program that summarized its own
// history is said in one line, not replayed.

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const (
	// convLabelMost is the most cells a speaker's name is given in the column
	// the names stand in. A model id past it is cut in the middle, keeping the
	// end that tells one model of a family from another ([rowTrim]).
	convLabelMost = 22
	// convTextLeast is the least room the words beside the names keep. Under it
	// the column costs more than it buys, and each name stands on a line of its
	// own with its words under it.
	convTextLeast = 28
	// convGap is the air between a name and its words: the row fitter's own
	// gutter, because this is the same shape of row — a name and what it says.
	convGap = rowGutter
	// convIndent is how far a speaker's words hang under its name when the names
	// stand on lines of their own: the two-cell lead every piece of this
	// surface's machinery keeps.
	convIndent = 2
	// convSaidMost is how many of the program's messages one of its turns draws.
	// A turn that answers eight tool calls sends eight results, and the eight
	// calls are already drawn one row each on the model's side just above it.
	convSaidMost = 3
	// convCodeaf is who answers a call codeaf refused. No model saw it, so the
	// line under the program's is codeaf's own.
	convCodeaf = "codeaf"
	// convProgramFallback names the program's side in the one case its name is
	// unknown: a conversation log with no program record beside it.
	convProgramFallback = "program"
)

// The words this page says in its own voice, each quoted in the manual as it is
// spelled here (worker-harness.md).
const (
	// convRestartedWord is the program's side of a call made after it rewrote its
	// own history as a summary: what it sent is its whole history again, and that
	// is one sentence rather than a replay.
	convRestartedWord = "summarized its history so far"
	// convFailedWord leads the line a call the model's side failed draws.
	convFailedWord = "the call failed"
	// convEarlierWord follows the count of calls the page does not carry.
	convEarlierWord = "earlier calls"
)

// convSide is one speaker's turn at talking: the name in the column and the
// lines it said, each already painted and not yet fitted.
type convSide struct {
	name  string
	lines []convLine
}

// convLine is one line a speaker said. lead is a painted mark drawn in front of
// the words and outside their fitting, so a narrow row gives up the tail of the
// words and never half a mark; text is the words, painted by ink.
type convLine struct {
	lead string
	text string
	ink  func(string) string
}

// taskPlanIsProgram reports whether the open stored page is a program's: its
// read carries the program's conversation, or its row names the program, which
// is how a read that came back for a side-list press is known to belong in the
// program's room instead ([app.finishRailPlan]).
func (a *app) taskPlanIsProgram() bool {
	page := a.taskSheet.plan
	return page.Program != nil || strings.TrimSpace(page.Row.Program) != ""
}

// convProgramOf is the page's program, never nil on a program's page: a page
// whose read has not come back yet is a program with nothing said, named off
// its row.
func convProgramOf(page session.PlanTaskPage) *session.PlanProgram {
	if page.Program != nil {
		return page.Program
	}
	return &session.PlanProgram{Name: strings.TrimSpace(page.Row.Program)}
}

// taskPlanPinned is the one line a program's page pins under its title, which
// no scroll moves: where the program is, what it has spent, how many calls it
// has made, and how long it has been going.
//
//	implement · $1.24 · 38 calls · 14m 3s
//
// THE LEAD IS THE STAGE, and the task's own state word when there is no stage —
// `running` in the seconds before the program names one, and `done`,
// `incomplete` or `stopped` once it has ended — so the line always says where
// the work stands. EVERY OTHER FIGURE IS DRAWN ONLY WHEN IT IS SOMETHING: no
// `$0.00`, no `0 calls`, no clock under a second. The spend is the store's
// spend rows for this task — the run's own money, which the status line's
// conversation total is not — and it names the ceiling beside it only when the
// page knows the ceiling. The line is ranked and fitted by [rowTail], so a
// narrow frame gives up the clock before the calls and the calls before the
// money.
func (a *app) taskPlanPinned(page session.PlanTaskPage, width int) string {
	return a.programPinned(page, width, a.taskPlanAge(page.Row))
}

// programPinned is [app.taskPlanPinned] with the clock handed in: the program's
// room ([app.programFactsWord]) draws the same line with the clock of the
// node it is standing on, which is the clock the rail and the landed card
// read, so the one run reads one figure wherever it is drawn.
func (a *app) programPinned(page session.PlanTaskPage, width int, clock string) string {
	if (page.Program == nil && strings.TrimSpace(page.Row.Program) == "") || width < 1 {
		return ""
	}
	program := convProgramOf(page)
	lead := strings.TrimSpace(page.Row.Stage)
	if lead == "" {
		lead = planStateWord(page.Row)
	}
	fields := []rowField{rowSay(lead)}
	if usd := planSpendWord(page.Row.USD); usd != "" {
		if program.CeilingUSD > 0 {
			fields = append(fields, rowSay(usd+" of "+dollars(program.CeilingUSD), usd))
		} else {
			fields = append(fields, rowSay(usd))
		}
	}
	if n := program.Calls; n > 0 {
		fields = append(fields, rowSay(itoa(n)+" "+plural("call", n)))
	}
	fields = append(fields, rowSay(clock))
	return rowTail(fields, width)
}

// taskPlanAge is how long a task has been going: from when it was made to when
// it ended, or to now while it runs. A task that has ended without a moment
// recorded for its ending draws no age rather than one that keeps climbing.
//
// A PROGRAM'S RUN READS THE CLOCK THE RAIL READS whenever this conversation
// holds one for it ([app.nodeClock]). The store's two stamps bracket other
// events than the run's notices do — the store is seeded before the run's copy
// is made, and it is ended by the supervisor rather than when the program's
// process is gone — so a page reading them and a rail and a landed card reading
// the notices drew three different figures for one run. The store's stamps are
// what is left for a run this conversation has no row for — and for the span
// between the program's exit and the row settling, when the page's pair has
// already stopped where the row will ([programExitClock]).
func (a *app) taskPlanAge(row session.PlanTaskRow) string {
	if node := a.programRowNode(row); node != nil {
		if word, ok := programExitClock(row, node); ok {
			return word
		}
		if word, ok := a.nodeClock(node); ok {
			return word
		}
	}
	if row.Started.IsZero() {
		return ""
	}
	end := row.Ended
	if end.IsZero() {
		if planEnded(row) {
			return ""
		}
		end = a.now()
	}
	if end.Before(row.Started) {
		return ""
	}
	span := end.Sub(row.Started)
	// A FINISHED SPAN IS ROUNDED TO THE SECOND, as the card and the room round
	// it ([taskNode.ranFor]) and as the note the chat is handed does, so a
	// 61.5-second run reads `1m 2s` wherever it is read; a running clock counts
	// whole seconds up.
	if !row.Ended.IsZero() {
		span = span.Round(time.Second)
	}
	return countUpWord(span)
}

// taskProgramBody is what a person reads on a program's page, under the pinned
// line: the conversation, and under it the notes the run left — its outcome and
// where its work went, which arrive when it ends and so belong at the bottom
// edge the page opens on, not above an hour of calls.
//
// EVERYTHING AN ORDINARY PAGE SPENDS ON ITS OWN MACHINERY IS ABSENT. The
// telemetry line is the pinned one; the brief opens the conversation; and a
// program's run records no steps worth a list of their own when the calls that
// did the work are on the page. A run whose conversation was never written —
// one from before the model API kept one — still has the steps its program
// reported, and draws them, so no page shows less than it did.
func (a *app) taskProgramBody(width int) []string {
	page, pal := a.taskSheet.plan, a.pal
	var out []string
	if n := len(a.taskSheet.planBack); n > 0 {
		out = append(out, pal.dim("esc/← "+a.taskSheet.planBack[n-1].Row.Title))
	}
	return append(out, a.programBody(page, width, a.taskSheet.planBriefFull)...)
}

// programBody is what both of a program's pages draw under their head — the
// tasks place's stored page ([app.taskProgramBody]) and the program's room in
// the conversation's own tab (programroom.go): the conversation, the steps a
// run with no conversation reported, and the notes. briefFull is the page's
// own fold, because each page folds its brief with its own key.
func (a *app) programBody(page session.PlanTaskPage, width int, briefFull bool) []string {
	pal := a.pal
	out := a.taskConversation(page, width, briefFull)
	if len(convProgramOf(page).Turns) == 0 && len(page.Steps) > 0 {
		out = append(out, "", pal.dim("steps"))
		for _, step := range page.Steps {
			if step.NotRun {
				continue
			}
			if command := planDisplayCommand(step.Command, step.Parts); command != "" {
				out = append(out, pal.ink(itoa(step.Step)+"  "+command))
			}
		}
	}
	if len(page.Notes) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, pal.dim("notes"))
		out = append(out, a.taskPlanNoteRows(page.Notes, width)...)
	}
	return out
}

// taskConversation is a program's page where an ordinary page draws its steps:
// the brief the program was handed, then every call it made, each as the
// program's side and the side that answered it.
//
// THE NAMES STAND IN A COLUMN OF THEIR OWN while the frame has the room, so the
// eye reads down the speakers and across to what each said; under
// [convTextLeast] cells of words each name stands on its own line instead. The
// column is as wide as the widest name on the page, so it does not move as the
// conversation grows by a call from the same model.
func (a *app) taskConversation(page session.PlanTaskPage, width int, briefFull bool) []string {
	if width < 1 {
		return nil
	}
	program := convProgramOf(page)
	pal := a.pal
	speaker := convProgramName(page)
	running := planStateWord(page.Row) == "running"
	column, text := convColumns(convNames(program, speaker), width)

	var out []string
	// THE BRIEF OPENS THE CONVERSATION. It is what the program was handed, in
	// the person's own words, and it stands for the program's side of the first
	// call — whose own words are the program's prompt around the same brief.
	// Folded to the brief's own three lines, with the key that unfolds it, the
	// way every other page folds a brief.
	opening := convSide{name: speaker}
	for _, line := range taskConversationBrief(page, text, briefFull) {
		opening.lines = append(opening.lines, convLine{text: line, ink: pal.ink})
	}
	if len(opening.lines) > 0 {
		out = append(out, convDraw(opening, column, width, pal)...)
	}
	// THE CALLS THE PAGE LEAVES OUT ARE COUNTED AT THE PAGE'S OWN EDGE, never in
	// a speaker's column, where the count read as something the program said. It
	// is spelled the way every fold line on this surface is ([bandFoldWord]).
	if program.Earlier > 0 {
		out = append(out, pal.dim(fit(glyphMore+itoa(program.Earlier)+" "+convEarlierWord, width)))
	}
	briefHead := convBriefHead(page.Description)
	for i, turn := range program.Turns {
		first := i == 0 && program.Earlier == 0
		if said := a.convProgramSide(turn, speaker, briefHead, first); len(said.lines) > 0 {
			out = append(out, convDraw(said, column, width, pal)...)
		}
		switch {
		case convHead(turn.Refused) != "":
			out = append(out, convDraw(convSide{name: convCodeaf, lines: []convLine{{
				text: taskPlanRefusedWord + railSep + convHead(turn.Refused), ink: pal.dim,
			}}}, column, width, pal)...)
		case convHead(turn.Failed) != "":
			out = append(out, convDraw(convSide{name: convModelWord(turn), lines: []convLine{{
				text: convFailedWord + railSep + convHead(turn.Failed), ink: pal.dim,
			}}}, column, width, pal)...)
		case turn.InFlight():
			// THE CALL IN FLIGHT IS THE LIVE EDGE, and it is drawn only while the
			// task can still be waiting on it. A call whose ending never reached the
			// log before the run ended is not in flight on a page about work that
			// is over: it draws no line at all rather than a clock that never stops.
			if running {
				if line := a.convInFlight(turn, width); line != "" {
					out = append(out, line)
				}
			}
		default:
			if answer := a.convModelSide(turn); len(answer.lines) > 0 {
				out = append(out, convDraw(answer, column, width, pal)...)
			}
		}
	}
	return out
}

// taskConversationBrief is the brief as the conversation opens with it: the
// description through the reader every page draws a brief with
// ([planBriefRows]), at the width the words get beside the names, folded to
// [briefFoldLines] with the line that says how many more and which key opens
// them.
func taskConversationBrief(page session.PlanTaskPage, text int, briefFull bool) []string {
	lines := planBriefRows(page.Description, text)
	if briefFull || len(lines) <= briefFoldLines {
		return lines
	}
	return append(append([]string(nil), lines[:briefFoldLines]...),
		bandFoldWord(len(lines)-briefFoldLines, briefFoldWhat, true)+railSep+briefFoldKey)
}

// taskConversationFolds reports whether a program's brief is long enough to
// fold at the frame's own width, which is what `ctrl+o` asks before it opens
// or closes it ([app.taskPlanKey]). It measures the brief at the width the
// conversation draws it at, so the key and the fold line cannot disagree.
func (a *app) taskConversationFolds() bool {
	width, _ := a.size()
	return convBriefFolds(a.taskSheet.plan, width-2)
}

// convBriefFolds is whether a program's brief folds when its conversation is
// drawn at this width — the one measure both of a program's pages ask before
// their `ctrl+o` opens or closes it.
func convBriefFolds(page session.PlanTaskPage, width int) bool {
	_, text := convColumns(convNames(convProgramOf(page), convProgramName(page)), width)
	return len(planBriefRows(page.Description, text)) > briefFoldLines
}

// convProgramSide is what the program said on one call, in the lines a person
// reads for it: nothing new on the first call, whose words the brief above
// already stands for; one sentence on a call made after it summarized its own
// history; and otherwise its newest messages, each as its first line — a tool's
// result as `<tool>: <line>` and its own words as they were.
//
// NEITHER A PROMPT NOR AN ECHO IS A ROW. A `system` message is the program
// instructing its model, and an `assistant` one is the model's last answer
// handed back to it, which the model's own side has already drawn; and a
// message that is the brief again says nothing the opening has not.
func (a *app) convProgramSide(turn delegate.Turn, speaker, briefHead string, first bool) convSide {
	pal := a.pal
	side := convSide{name: speaker}
	if turn.Restarted {
		side.lines = append(side.lines, convLine{text: convRestartedWord, ink: pal.dim})
		return side
	}
	if first {
		return side
	}
	var said []string
	for _, message := range turn.Sent {
		line := convHead(message.Text)
		switch strings.TrimSpace(message.Role) {
		case "system", "assistant":
			continue
		case "tool":
			if tool := strings.TrimSpace(message.Tool); tool != "" {
				line = tool + ": " + line
			}
		default:
			if convRepeatsBrief(line, briefHead) {
				continue
			}
		}
		if strings.TrimSpace(line) != "" {
			said = append(said, line)
		}
	}
	shown := said
	if len(shown) > convSaidMost {
		shown = shown[:convSaidMost]
	}
	for _, line := range shown {
		side.lines = append(side.lines, convLine{text: line, ink: pal.dim})
	}
	if more := len(said) - len(shown); more > 0 {
		side.lines = append(side.lines, convLine{text: "+" + itoa(more) + " more", ink: pal.dim})
	}
	return side
}

// convModelSide is what the model answered on one call: the first line of its
// words, and every tool it asked the program to run, one dim row each behind
// that tool's action mark — the same family marks the conversation's own steps
// wear ([app.actionMarkFor]), so a person who has learned `✎` for an edit there
// reads it here.
func (a *app) convModelSide(turn delegate.Turn) convSide {
	pal := a.pal
	side := convSide{name: convModelWord(turn)}
	if reply := convHead(turn.Reply); reply != "" {
		side.lines = append(side.lines, convLine{text: reply, ink: pal.ink})
	}
	for _, call := range turn.Calls {
		name := convHead(call.Name)
		if name == "" {
			continue
		}
		words := name
		if about := convHead(convCallAbout(call.Args)); about != "" {
			words += " " + about
		}
		mark := a.actionMarkFor(session.ActionCategoryForTool(name))
		side.lines = append(side.lines, convLine{lead: pal.dim(mark) + " ", text: words, ink: pal.dim})
	}
	return side
}

// convInFlight is the call in flight: the running mark, the model it went to,
// and how long it has been out — one line, the last on the page, gone the moment
// its ending reaches the log. The mark comes off the vocabulary's own door, so
// the line gets this terminal's repertoire; the clock is the frame's and says
// nothing for the call's first second.
func (a *app) convInFlight(turn delegate.Turn, width int) string {
	mark := a.icon(tokens.GStepRunning)
	room := width - ansi.StringWidth(mark) - 1
	if room < 1 {
		return ""
	}
	var words []string
	if model := convModelWordOf(turn.Model); model != "" {
		words = append(words, model)
	}
	if !turn.Started.IsZero() {
		if clock := countUpWord(a.now().Sub(turn.Started)); clock != "" {
			words = append(words, clock)
		}
	}
	return a.pal.ink(mark) + " " + a.pal.dim(fit(strings.Join(words, railSep), room))
}

// convDraw lays one side out: its name in the column on its first line and its
// words beside it, or — where the frame is too narrow for a column — its name
// on a line of its own and its words hung under it. EVERY ROW IS FITTED TO THE
// WIDTH: the name is cut in the middle when it must be ([rowTrim]), a mark in
// front of the words is kept whole, and the words give up their tail.
func convDraw(side convSide, column, width int, pal palette) []string {
	var out []string
	if column == 0 {
		if name := strings.TrimSpace(side.name); name != "" {
			label, _ := rowTrim(name, width, false)
			out = append(out, pal.muted(label))
		}
		indent := strings.Repeat(" ", convIndent)
		for _, line := range side.lines {
			out = append(out, indent+convWords(line, width-convIndent))
		}
		return out
	}
	gap := strings.Repeat(" ", convGap)
	blank := strings.Repeat(" ", column)
	for i, line := range side.lines {
		cell := blank
		if i == 0 && strings.TrimSpace(side.name) != "" {
			label, _ := rowTrim(side.name, column, false)
			cell = padTo(pal.muted(label), column)
		}
		out = append(out, cell+gap+convWords(line, width-column-convGap))
	}
	return out
}

// convWords is one line's words at their width, behind its mark when it has
// one. A width too small to hold the mark draws the words alone.
func convWords(line convLine, width int) string {
	if width < 1 {
		return ""
	}
	ink := line.ink
	if ink == nil {
		ink = func(s string) string { return s }
	}
	lead := line.lead
	if lead != "" {
		if cells := ansi.StringWidth(ansi.Strip(lead)); cells < width {
			return lead + ink(fit(line.text, width-cells))
		}
	}
	return ink(fit(line.text, width))
}

// convColumns decides the page's two widths from the names on it: the column
// the names stand in, and the room their words get beside it. A column of zero
// is the narrow layout, where every name stands on its own line and the words
// hang [convIndent] cells under it.
func convColumns(names []string, width int) (int, int) {
	widest := 0
	for _, name := range names {
		if cells := ansi.StringWidth(strings.TrimSpace(name)); cells > widest {
			widest = cells
		}
	}
	column := widest
	if column > convLabelMost {
		column = convLabelMost
	}
	if third := width / 3; column > third {
		column = third
	}
	if column < 1 || width-column-convGap < convTextLeast {
		return 0, width - convIndent
	}
	return column, width - column - convGap
}

// convNames is every name the page will draw in its column: the program's,
// codeaf's when a call was refused, and the model of every call on the page.
func convNames(program *session.PlanProgram, speaker string) []string {
	names := []string{speaker}
	for _, turn := range program.Turns {
		if strings.TrimSpace(turn.Refused) != "" {
			names = append(names, convCodeaf)
			continue
		}
		if !turn.InFlight() {
			names = append(names, convModelWord(turn))
		}
	}
	return names
}

// convProgramName is the name the program's side wears: the program record's,
// then its row's, and a plain noun only when neither said.
func convProgramName(page session.PlanTaskPage) string {
	if page.Program != nil {
		if name := strings.TrimSpace(page.Program.Name); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(page.Row.Program); name != "" {
		return name
	}
	return convProgramFallback
}

// convModelWord is the model that answered a call: the one that served it
// when codeaf's router answered with another than the program asked for, and
// otherwise the one asked for.
func convModelWord(turn delegate.Turn) string {
	if served := strings.TrimSpace(turn.Served); served != "" {
		return convModelWordOf(served)
	}
	return convModelWordOf(turn.Model)
}

// convModelWordOf is a model id as a speaker's name: the part after the last
// vendor, which is the part that names the model rather than who sells it —
// the rail's own reading of a model ([railModelWord]).
func convModelWordOf(id string) string {
	id = convHead(id)
	if at := strings.LastIndexByte(id, '/'); at >= 0 && at+1 < len(id) {
		id = id[at+1:]
	}
	return id
}

// convHead is the first line of a text that says anything, CLEANED TO BE ONE
// ROW. The page's texts arrive already cut to their heads (internal/session's
// plandb_program.go), and this is still the one door every text on the page is
// drawn through, for two reasons: a page built any other way — a test's, an
// older engine's — must not draw a second line into one row, and what a
// program sends is a tool's raw output, which carries tabs a row cannot measure
// and escape sequences a terminal would obey. So the line is stripped of every
// escape sequence, a tab becomes a space and any other control character goes.
func convHead(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(convClean(line)); line != "" {
			return line
		}
	}
	return ""
}

// convClean is one line of a program's words with nothing a terminal would act
// on left in it.
func convClean(line string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
			return -1
		}
		return r
	}, ansi.Strip(line))
}

// convBriefHead is the brief's first line as a program's own message would
// carry it, so a message that is the brief again can be told from one that
// says something new.
func convBriefHead(description string) string { return convHead(description) }

// convRepeatsBrief reports whether a message's first line is the brief's again.
// The page carries a message's head cut at a couple of hundred bytes with the
// cut marked, so a long brief repeated is the brief's own line up to that mark.
func convRepeatsBrief(line, briefHead string) bool {
	if line == "" || briefHead == "" {
		return false
	}
	if line == briefHead {
		return true
	}
	cut := strings.TrimSuffix(line, glyphMore)
	return cut != line && cut != "" && strings.HasPrefix(briefHead, cut)
}

// convAboutKeys are the arguments that say what a call was about, most telling
// first: the command a shell ran, the pattern a search looked for (a search
// names where it looked too, and the pattern is the part a person reads it
// for), the address a fetch went to, then the file a call opened or wrote. They
// are the names the programs' tools use for them, in both spellings those tools
// use.
var convAboutKeys = []string{
	"command", "cmd", "pattern", "query", "url", "filePath", "file_path", "path", "description", "prompt",
}

// convCallAbout is what a call was about, in the fewest words that say it. A
// program's tool call carries its arguments as one line of JSON, cut at a
// couple of hundred bytes when it is long — an edit carries the text it
// replaces — so it is READ FORGIVINGLY rather than parsed: the most telling
// argument that has a string value, then the first string value there is. A
// line that is not an object is drawn as it was written, and an object that
// holds no string at all says nothing a row can carry.
func convCallAbout(args string) string {
	args = strings.TrimSpace(args)
	if !strings.HasPrefix(args, "{") {
		return args
	}
	for _, key := range convAboutKeys {
		if value, ok := convArgNamed(args, key); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for at := 0; at < len(args); {
		colon := strings.IndexByte(args[at:], ':')
		if colon < 0 {
			break
		}
		at += colon + 1
		if rest := strings.TrimLeft(args[at:], " \t"); strings.HasPrefix(rest, `"`) {
			if value := strings.TrimSpace(convJSONString(rest[1:])); value != "" {
				return value
			}
		}
	}
	return ""
}

// convArgNamed is the string value of one argument, when the line names it
// with a string value; a value cut off at the line's end is read to where it
// was cut.
func convArgNamed(args, key string) (string, bool) {
	name := `"` + key + `"`
	for from := 0; ; {
		at := strings.Index(args[from:], name)
		if at < 0 {
			return "", false
		}
		from += at + len(name)
		rest := strings.TrimLeft(args[from:], " \t")
		if !strings.HasPrefix(rest, ":") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t")
		if !strings.HasPrefix(rest, `"`) {
			return "", false
		}
		return convJSONString(rest[1:]), true
	}
}

// convJSONString reads a JSON string's body up to its closing quote, or to the
// end of a line that was cut inside it, with its escapes read as the characters
// they stand for and a line break or a tab as a space.
func convJSONString(body string) string {
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '"':
			return out.String()
		case c != '\\':
			out.WriteByte(c)
			continue
		}
		if i+1 >= len(body) {
			break
		}
		i++
		switch body[i] {
		case 'n', 't', 'r':
			out.WriteByte(' ')
		case 'u':
			if i+4 < len(body) {
				if code, err := strconv.ParseUint(body[i+1:i+5], 16, 32); err == nil && utf8.ValidRune(rune(code)) {
					out.WriteRune(rune(code))
					i += 4
					continue
				}
			}
			out.WriteByte('u')
		default:
			// `\"`, `\\` and `\/` stand for the character after the backslash.
			out.WriteByte(body[i])
		}
	}
	return out.String()
}
