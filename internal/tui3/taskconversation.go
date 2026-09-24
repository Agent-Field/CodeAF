package tui3

// taskconversation.go draws a PROGRAM'S task page as the actions it took.
//
// A task a run handed to a program codeaf carries (senior-dev first) is a
// process with steps of its own, and the page shows the program doing it: each
// thing it did, under the step of its process that action served, with how it
// came out at the right edge — not a dialogue between the program and a model,
// which made a pipeline of spec, exploration, a pinned check, a checklist, an
// implementation, a hand-in and its own check of the tree read like one chat.
//
//	BRIEF      rewrite the auth middleware to use the new session store
//	SETUP      set up its workspace                               git
//	SPEC       wrote your brief down as its spec
//	EXPLORE    read internal/auth/middleware.go
//	           ran go test ./internal/auth/...             fails · exit 1
//	IMPLEMENT  edited internal/auth/middleware.go
//	           compacted its memory
//	           ◐ thinking · 12s
//
// THE ACTIONS ARE THE PROGRAM'S, IN ITS OWN WORDS. The run keeps every stage,
// step and ending the program reported, stamped as codeaf received it, and the
// program's own vocabulary reads them (internal/delegate's Present; senior-dev's
// is internal/seniordev's actions.go) before the page ever holds them. What only
// the program's calls to a model know is merged in by time: a history rewritten
// as a summary is `compacted its memory` (once, when the program said so too), a
// change of the model answering is `switched to <model>` with the program's
// reason when it gave one, and a call refused or failed is one plain line. A
// model is named nowhere else on the page; the cost and the clock stay on the
// pinned line. The raw calls are one key away (programcalls.go,
// [programCallsKey]).
//
// THE PAGE READS NOTHING. Every line here is drawn from the page the surface
// already holds ([session.PlanTaskPage.Program], read off the loop on the page's
// own beat), with the frame's own clock for the two figures that tick — the
// run's age and the call in flight's. The texts arrive cut to their first line
// with the run's copy taken out of their paths (internal/session's
// plandb_program.go), so a row here is a choice of which line to show and never
// a reading of the record.

import (
	"sort"
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
	// convProgramFallback names the program's side in the one case its name is
	// unknown: a conversation log with no program record beside it.
	convProgramFallback = "program"
)

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
// line: the actions the program took, and under them the notes the run left —
// its outcome and where its work went, which arrive when it ends and so belong
// at the bottom edge the page opens on, not above an hour of work.
//
// EVERYTHING AN ORDINARY PAGE SPENDS ON ITS OWN MACHINERY IS ABSENT. The
// telemetry line is the pinned one; the brief opens the actions; and a
// program's run needs no list of steps of its own when the actions that did
// the work are on the page.
func (a *app) taskProgramBody(width int) []string {
	page, pal := a.taskSheet.plan, a.pal
	var out []string
	if n := len(a.taskSheet.planBack); n > 0 {
		out = append(out, pal.dim("esc/← "+a.taskSheet.planBack[n-1].Row.Title))
	}
	return append(out, a.programBody(page, width, a.taskSheet.planBriefFull, a.taskSheet.planCalls)...)
}

// programBody is what both of a program's pages draw under their head — the
// tasks place's stored page ([app.taskProgramBody]) and the program's room in
// the conversation's own tab (programroom.go): the program's actions, or its
// raw calls when the page's own [programCallsKey] asked for them, and the
// notes. briefFull and calls are the page's own, because each page folds its
// brief and turns to its calls with its own keys.
//
// A RUN FROM BEFORE ITS CALLS WERE LOGGED still has the steps its program
// reported: the actions draw them in their own shape, and the calls, which
// have none to draw, list them under their own heading as they always did.
func (a *app) programBody(page session.PlanTaskPage, width int, briefFull, calls bool) []string {
	pal := a.pal
	var out []string
	if calls {
		out = a.programCalls(page, width, briefFull)
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
	} else {
		out = a.taskConversation(page, width, briefFull)
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

// The words this page says in its own voice, each quoted in the manual as it is
// spelled here (senior-dev.md, worker-harness.md).
const (
	// actBriefWord leads the brief, in the column the step words stand in: the
	// brief is what the program was handed, before any step of its own.
	actBriefWord = "brief"
	// actCompactedWord is a program's history rewritten as a summary, said once.
	actCompactedWord = "compacted its memory"
	// actSwitchedWord leads the line a change of the model answering draws.
	actSwitchedWord = "switched to"
	// actRefusedWord and actFailedWord lead the line a call codeaf refused, or
	// the model's side failed, draws.
	actRefusedWord = "codeaf refused a call"
	actFailedWord  = "a call to its model failed"
	// actThinkingWord is what is in flight while a call to the model is out.
	actThinkingWord = "thinking"
	// actEarlierWord follows the count of actions the page does not carry.
	actEarlierWord = "earlier actions"
)

// actNear is how far apart in time a line from the calls and the program's own
// line about the same thing — a compaction, a switch of model — may be and
// still be the one event. The program reports a compaction the moment it is
// decided, between the summary call and the call after it; a switch, when the
// call it was made for has come back.
const actNear = 10 * time.Second

// actLine is one line of the actions, before it is laid out: when it happened,
// the step's word it belongs under ("" for whatever step is under way), its
// words and how it came out, and whether it is the program steering its own
// model or a line from the calls, which are drawn quieter.
type actLine struct {
	at      time.Time
	step    string
	text    string
	outcome string
	steer   bool
	quiet   bool
}

// taskConversation is a program's page where an ordinary page draws its steps:
// the brief the program was handed, then every action it took, merged by time
// with what only its calls know, each under the step of its process it served.
//
// THE STEP'S WORD STANDS IN A COLUMN OF ITS OWN while the frame has the room,
// printed on the first action of each run of actions in one step and blank for
// the rest, so the eye reads down the steps and across to what was done in
// each; under [convTextLeast] cells of words each step's word stands on its own
// line instead, and its actions hang under it. The column is as wide as the
// widest word on the page, so it does not move as the run goes on.
func (a *app) taskConversation(page session.PlanTaskPage, width int, briefFull bool) []string {
	if width < 1 {
		return nil
	}
	program := convProgramOf(page)
	pal := a.pal
	lines := actLines(page)
	column, text := actColumns(lines, width)

	var out []string
	// THE BRIEF OPENS THE PAGE, under its own word: it is what the program was
	// handed, in the person's own words, folded to the brief's own three lines
	// with the key that unfolds it, the way every other page folds a brief.
	for i, line := range taskConversationBrief(page, text, briefFull) {
		word := ""
		if i == 0 {
			word = actBriefWord
		}
		out = append(out, actRow(pal, word, pal.ink(fit(line, text)), column, width)...)
	}
	// THE ACTIONS THE PAGE LEAVES OUT ARE COUNTED AT THE PAGE'S OWN EDGE, spelled
	// the way every fold line on this surface is ([bandFoldWord]).
	if program.EarlierActions > 0 {
		out = append(out, pal.dim(fit(glyphMore+itoa(program.EarlierActions)+" "+actEarlierWord, width)))
	}
	current := ""
	for _, line := range lines {
		word := ""
		if line.step != "" && line.step != current {
			word, current = line.step, line.step
		}
		out = append(out, actRow(pal, word, a.actBody(line, text), column, width)...)
	}
	// THE CALL IN FLIGHT IS THE LIVE EDGE, drawn only while the task can still be
	// waiting on it: a call whose ending never reached the log before the run
	// ended is not in flight on a page about work that is over.
	if planStateWord(page.Row) == "running" {
		if n := len(program.Turns); n > 0 && program.Turns[n-1].InFlight() {
			if line := a.actInFlight(program.Turns[n-1], text); line != "" {
				out = append(out, actRow(pal, "", line, column, width)...)
			}
		}
	}
	return out
}

// actRow lays one row out: the step's word in the column and the body beside
// it, or — where the frame is too narrow for a column — the word on a line of
// its own and the body hung [convIndent] cells under it. The word is the page's
// structure, not its signal: bold and upper-case in the muted ink, never the
// accent, which a screen spends on the one live thing ([DESIGN-LANGUAGE.md]'s
// accent budget).
func actRow(pal palette, word, body string, column, width int) []string {
	label := ""
	if word = strings.TrimSpace(word); word != "" {
		room := column
		if column == 0 {
			room = width
		}
		trimmed, _ := rowTrim(strings.ToUpper(word), room, false)
		label = pal.bold(pal.muted(trimmed))
	}
	if column == 0 {
		var out []string
		if label != "" {
			out = append(out, label)
		}
		return append(out, strings.Repeat(" ", convIndent)+body)
	}
	cell := strings.Repeat(" ", column)
	if label != "" {
		cell = padTo(label, column)
	}
	return []string{cell + strings.Repeat(" ", convGap) + body}
}

// actBody is one action's words at the room they get, painted: the program's
// own work in ink, its steering of its model in the note's ink, a line from the
// calls dim, and how it came out dim at the right edge. When the room will not
// hold both at the edge, the outcome follows the words after a separator and
// the row gives up its tail.
func (a *app) actBody(line actLine, width int) string {
	pal := a.pal
	ink := pal.ink
	switch {
	case line.quiet:
		ink = pal.dim
	case line.steer:
		ink = pal.narr
	}
	text, outcome := convHead(line.text), convHead(line.outcome)
	if outcome == "" {
		return ink(fit(text, width))
	}
	cells := ansi.StringWidth(outcome)
	if room := width - cells - convGap; room >= convTextLeast/2 {
		words, measured := fitWidth(text, room)
		return ink(words) + strings.Repeat(" ", width-measured-cells) + pal.dim(outcome)
	}
	return ink(fit(text+railSep+outcome, width))
}

// actInFlight is the call in flight: the running mark and how long the program
// has been waiting on its model — one line, the last on the page, gone the
// moment the call's ending reaches the log. The mark comes off the vocabulary's
// own door, so the line gets this terminal's repertoire; the clock is the
// frame's and says nothing for the call's first second.
func (a *app) actInFlight(turn delegate.Turn, width int) string {
	mark := a.icon(tokens.GStepRunning)
	room := width - ansi.StringWidth(mark) - 1
	if room < 1 {
		return ""
	}
	words := []string{actThinkingWord}
	if !turn.Started.IsZero() {
		if clock := countUpWord(a.now().Sub(turn.Started)); clock != "" {
			words = append(words, clock)
		}
	}
	return a.pal.ink(mark) + " " + a.pal.dim(fit(strings.Join(words, railSep), room))
}

// actColumns decides the page's two widths from the step words on it: the
// column the words stand in and the room the actions get beside it. A column
// of zero is the narrow layout, where every word stands on its own line and
// the actions hang [convIndent] cells under it.
func actColumns(lines []actLine, width int) (int, int) {
	widest := ansi.StringWidth(actBriefWord)
	for _, line := range lines {
		if cells := ansi.StringWidth(strings.TrimSpace(line.step)); cells > widest {
			widest = cells
		}
	}
	column := min(min(widest, convLabelMost), width/3)
	if column < 1 || width-column-convGap < convTextLeast {
		return 0, width - convIndent
	}
	return column, width - column - convGap
}

// actLines is everything the page draws under the brief, in the order it
// happened: the program's actions as its own vocabulary read them, and what
// only its calls know — a compaction the program did not report, a change of
// the model answering, a call refused or failed.
//
// A RUN FROM BEFORE THE ACTION LOG has only its calls, and its page is drawn
// from them in the same shape: each tool its model asked for as an action, and
// no step's word, because nothing recorded which step it served. A run whose
// calls were never logged either draws the steps its program reported.
func actLines(page session.PlanTaskPage) []actLine {
	program := convProgramOf(page)
	var lines []actLine
	for _, shown := range program.Actions {
		if strings.TrimSpace(shown.Model) != "" {
			// A SWITCH IS READ WITH THE CALLS BELOW, which say the same move from
			// the model's side; the program's line gives it its reason.
			continue
		}
		lines = append(lines, actLine{at: shown.At, step: shown.Step, text: shown.Text, outcome: shown.Outcome, steer: shown.Steer})
	}
	lines = append(lines, actFromCalls(program)...)
	if len(program.Actions) == 0 && len(program.Turns) == 0 {
		for _, step := range page.Steps {
			if command := planDisplayCommand(step.Command, step.Parts); command != "" && !step.NotRun {
				lines = append(lines, actLine{text: command})
			}
		}
	}
	// THE ACTIONS THE PAGE CUT ARE CUT FROM THE CALLS TOO: a line from a call
	// older than the first action the page carries would stand above the count
	// of the ones it left out.
	if program.EarlierActions > 0 && len(program.Actions) > 0 {
		first := program.Actions[0].At
		kept := lines[:0]
		for _, line := range lines {
			if !line.at.Before(first) {
				kept = append(kept, line)
			}
		}
		lines = kept
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].at.Before(lines[j].at) })
	return lines
}

// actFromCalls is what the page draws from the program's calls: the compactions
// the program did not report itself, every change of the model answering its
// work — with the program's reason when it gave one, and the program's own line
// for a move the calls do not show — and every call refused or failed; and for
// a run with no action log at all, every tool its model asked for.
func actFromCalls(program *session.PlanProgram) []actLine {
	turns := program.Turns
	var lines []actLine
	memory := func(from, to time.Time) bool {
		for _, shown := range program.Actions {
			if shown.Memory && !shown.At.Before(from.Add(-actNear)) && !shown.At.After(to.Add(actNear)) {
				return true
			}
		}
		return false
	}
	switches := map[int]bool{}
	switchFor := func(model string, from, to time.Time) (string, bool) {
		for i, shown := range program.Actions {
			if switches[i] || strings.TrimSpace(shown.Model) == "" || !actSameModel(convModelWordOf(shown.Model), model) {
				continue
			}
			if !shown.At.Before(from.Add(-actNear)) && !shown.At.After(to.Add(actNear)) {
				switches[i] = true
				return shown.Reason, true
			}
		}
		return "", false
	}
	previous, previousAt := "", time.Time{}
	compacted := -1
	for i := 0; i < len(turns); i++ {
		turn := turns[i]
		switch {
		case convHead(turn.Refused) != "":
			lines = append(lines, actLine{at: turn.Started, text: actRefusedWord + railSep + convHead(turn.Refused), quiet: true})
			continue
		case convHead(turn.Failed) != "":
			lines = append(lines, actLine{at: turn.Started, text: actFailedWord + railSep + convHead(turn.Failed), quiet: true})
			continue
		}
		if turn.Restarted && i > compacted {
			// ONE COMPACTION IS A RUN OF RESTARTED CALLS: the summary itself and
			// the call after it both rewrite the history, and a person reads one
			// line for them.
			last := i
			for last+1 < len(turns) && turns[last+1].Restarted {
				last++
			}
			compacted = last
			end := turns[last].Ended
			if end.IsZero() {
				end = turns[last].Started
			}
			if !memory(turn.Started, end) {
				lines = append(lines, actLine{at: turn.Started, text: actCompactedWord})
			}
		}
		if turn.InFlight() {
			continue
		}
		// THE SUMMARY CALL IS NOT THE WORK. A restarted call that asked for no
		// tool, followed by another restarted call, is the history being
		// summarized — on a cheaper model, often — and its model is not a change
		// of the model doing the work.
		if turn.Restarted && len(turn.Calls) == 0 && i+1 < len(turns) && turns[i+1].Restarted {
			continue
		}
		model := convModelWord(turn)
		if previous != "" && model != "" && !actSameModel(model, previous) {
			reason, _ := switchFor(model, previousAt, turn.Ended)
			text := actSwitchedWord + " " + model
			if reason != "" {
				text += railSep + reason
			}
			lines = append(lines, actLine{at: turn.Started, text: text})
		}
		if model != "" {
			previous, previousAt = model, turn.Started
		}
		if len(program.Actions) == 0 {
			for _, call := range turn.Calls {
				if text := actCallText(call); text != "" {
					lines = append(lines, actLine{at: turn.Ended, text: text})
				}
			}
		}
	}
	// A MOVE THE CALLS DO NOT SHOW is still the program's to say: the router
	// moved the work and the model API answered on the model it moved to.
	for i, shown := range program.Actions {
		if strings.TrimSpace(shown.Model) == "" || switches[i] {
			continue
		}
		text := actSwitchedWord + " " + convModelWordOf(shown.Model)
		if reason := strings.TrimSpace(shown.Reason); reason != "" {
			text += railSep + reason
		}
		lines = append(lines, actLine{at: shown.At, step: shown.Step, text: text})
	}
	return lines
}

// actSameModel reports whether two short model words name one model: the same
// word, or one a dated build or variant of the other (`deepseek-v4-flash` and
// `deepseek-v4-flash-0731`), which is how the model a program asked for and the
// one the service answered with are often spelled.
func actSameModel(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"-") || strings.HasPrefix(b, a+"-")
}

// actCallText is one tool a model asked for, as an action a person reads, for a
// run whose program kept no action log: the verb and what it was about.
func actCallText(call delegate.ToolUse) string {
	name := convHead(call.Name)
	about := convHead(convCallAbout(call.Args))
	if name == "" {
		return ""
	}
	verb := map[string]string{
		"read": "read", "edit": "edited", "write": "wrote", "apply_patch": "patched",
		"bash": "ran", "grep": "searched", "glob": "listed", "webfetch": "fetched",
		"websearch": "searched the web for",
	}[name]
	switch {
	case name == "submit":
		return "handed in its work"
	case verb == "" || about == "":
		return strings.TrimSpace(name + " " + about)
	}
	return verb + " " + about
}

// taskConversationBrief is the brief as the page opens with it: the description
// through the reader every page draws a brief with ([planBriefRows]), at the
// width the words get beside the step words, folded to [briefFoldLines] with
// the line that says how many more and which key opens them.
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
// page draws it at, so the key and the fold line cannot disagree.
func (a *app) taskConversationFolds() bool {
	width, _ := a.size()
	return convBriefFolds(a.taskSheet.plan, width-2, a.taskSheet.planCalls)
}

// convBriefFolds is whether a program's brief folds when its page is drawn at
// this width — as its actions, or as its calls — the one measure both of a
// program's pages ask before their `ctrl+o` opens or closes it.
func convBriefFolds(page session.PlanTaskPage, width int, calls bool) bool {
	_, text := actColumns(actLines(page), width)
	if calls {
		_, text = convColumns(convNames(convProgramOf(page), convProgramName(page)), width)
	}
	return len(planBriefRows(page.Description, text)) > briefFoldLines
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
