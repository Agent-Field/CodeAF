package tui3

// programcalls.go draws a PROGRAM'S RAW CALLS: the dialogue between the program
// and the model that answered it, one call at a time, which is what a program's
// page used to open on. It opens on the program's actions now
// (taskconversation.go), and this is one key away ([programCallsKey]) — for a
// person debugging what the model was actually sent and what it said.
//
//	senior-dev          rewrite the auth middleware to use the new session store
//	deepseek-v4-flash   I'll read the middleware first.
//	                    ▤ read internal/auth/middleware.go
//	senior-dev          read: package auth
//	◐ deepseek-v4-flash · 12s
//
// THE PAGE READS NOTHING, here as on the actions: every line is drawn from the
// page the surface already holds, with the frame's own clock for the call in
// flight.
//
// WHAT IS DRAWN IS THE PERSON'S, NEVER THE MACHINERY'S. A program's system
// prompt and the model's own words handed back to it are part of every call and
// say nothing new, so neither is ever a row; a program that summarized its own
// history is said in one line, not replayed.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// programCallsKey turns a program's page between its actions and its raw
// calls, and back — on the stored page in the tasks place and in the program's
// room alike, named ONCE so both key rows and the manual say the one chord.
//
// WHY THIS ONE. The room has a message box, so the key must be a chord that
// types no character; the stored page takes the reading keys and `ctrl+o`; and
// every chord the conversation already spends is spent (keys.md's table). ctrl+y
// means nothing in either surface — its only other binding is the `/files`
// list's copy, a different page with a different keyboard — and it is a plain
// control byte every terminal delivers.
const programCallsKey = "ctrl+y"

// The key row's words for it: what the next press shows.
const (
	programCallsWord   = programCallsKey + " calls"
	programActionsWord = programCallsKey + " actions"
)

// programCallsHint is the key row's clause for a program's page: the view the
// next press of [programCallsKey] turns to.
func programCallsHint(calls bool) string {
	if calls {
		return programActionsWord
	}
	return programCallsWord
}

const (
	// convSaidMost is how many of the program's messages one of its turns draws.
	// A turn that answers eight tool calls sends eight results, and the eight
	// calls are already drawn one row each on the model's side just above it.
	convSaidMost = 3
	// convCodeaf is who answers a call codeaf refused. No model saw it, so the
	// line under the program's is codeaf's own.
	convCodeaf = "codeaf"
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

// programCalls is a program's page as the raw calls it made: the brief the
// program was handed, then every call, each as the program's side and the side
// that answered it. It is one key away from the actions the page opens on
// ([programCallsKey]), for a person who wants to see what the model was sent
// and what it said.
//
// THE NAMES STAND IN A COLUMN OF THEIR OWN while the frame has the room, so the
// eye reads down the speakers and across to what each said; under
// [convTextLeast] cells of words each name stands on its own line instead. The
// column is as wide as the widest name on the page, so it does not move as the
// conversation grows by a call from the same model.
func (a *app) programCalls(page session.PlanTaskPage, width int, briefFull bool) []string {
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
