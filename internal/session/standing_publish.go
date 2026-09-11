package session

// standing_publish.go is the one decision a firing passes before aforge
// publishes its report: MAY THIS RUN PUBLISH, AND IF NOT, WHY NOT.
//
// ── WHO PUBLISHES, ON WHAT EVIDENCE ──
//
// The report is aforge's to publish and never the run's (internal/standing's
// Action.Report): the run only replies with it, and aforge writes it. So the
// question a firing's end has to answer is whether the run's own turns are
// evidence of a finished report — and the evidence is what those turns came
// to: an error or the pass's deadline, the step or spending limit, a call only
// a person could allow, a refused write of its own report, a report that was
// opened and never closed.
//
// ── WHY ONE VALUE ──
//
// Each of those used to be a flag of its own beside the run loop, read by a
// guard of its own, and the final review of 2026-09-10 found two that no guard
// read. A run stopped at its step limit ends its turn NORMALLY — the interrupt
// closes it like any other turn, with no error — so it published a report
// written before the stop. And a run whose write of its own report was refused
// published the apology it said afterwards. So the facts are gathered in one
// place ([firingEnd]), one method reads them ([firingEnd.withheld]), and the
// outcome, the line a person reads and the publication all read that one
// answer.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The lines a run's report is written between ([standingReportBlock]).
const (
	standingReportOpen  = "<report>"
	standingReportClose = "</report>"
)

// reportLines is what a run's text holds between the report's two lines.
type reportLines int

const (
	// noReportLines: the text opens no report.
	noReportLines reportLines = iota
	// closedReport: its last report was opened and closed.
	closedReport
	// unclosedReport: its last report was opened and never closed.
	unclosedReport
	// unopenedReport: it closed a report it never opened — a closing tag with
	// no opening line anywhere before it.
	unopenedReport
)

// firingEnd is what a firing's turns came to, gathered by the run's one event
// reader. Every fact the outcome and the publication are decided from is here.
type firingEnd struct {
	// said is the last thing the run said, from whichever turn said something,
	// and report is the last report it wrote between both lines — each WITH HOW
	// THE TURN THAT PRODUCED IT ENDED ([turnWords], [turnReport]).
	said   turnWords
	report turnReport
	// needs is the line the run stopped on when only a person could allow a
	// call.
	needs string
	// cut is the error a turn ended on.
	cut error
	// saved: a call saved something. capped: the step or spending limit
	// stopped the run. ownReportWrite: a write of its own report path was
	// refused, as every unattended write is.
	saved, capped, ownReportWrite bool
}

// turnWords is what one turn said: all of it (reply), and what it said after
// its last tool call (final). cutAtLimit says that turn's answer stopped at the
// provider's output limit with its continuations spent, as the turn's own
// ending said ([Event.Truncated]).
type turnWords struct {
	reply, final string
	cutAtLimit   bool
}

// turnReport is the last report lines one turn wrote ([delimitedReport]) and
// whether that turn's answer was cut at the output limit.
//
// A REPORT AND THE ENDING OF THE TURN THAT WROTE IT ARE ONE VALUE. They were two
// fields once, read at different times, and the second review of 2026-09-10
// found them come apart: a divided run wrote a closed report in a turn cut at
// the limit, its parts came home, and the resumed turn said only
// "Acknowledged." — the report stayed because the new words had no lines, the
// cut was forgotten because the new turn ended clean, and a disqualified report
// was published on the word of a turn that never touched it. Only a turn that
// writes NEW report lines replaces this, so only a new report can stand in for
// a cut one.
type turnReport struct {
	body       string
	lines      reportLines
	cutAtLimit bool
}

// closeTurn records one turn: everything it said, what it said after its last
// tool call, and whether its answer was cut at the output limit. Text that
// holds no report lines leaves the last report standing — a run that wrote its
// report and then said something more has still written its report — and a
// turn that said nothing leaves the last words standing, since silence is not a
// newer account. Each carries its own turn's ending with it.
func (e *firingEnd) closeTurn(said, sinceLastTool string, cutAtLimit bool) {
	if body, lines := delimitedReport(said); lines != noReportLines {
		e.report = turnReport{body: body, lines: lines, cutAtLimit: cutAtLimit}
	}
	switch words := strings.TrimSpace(said); {
	case words != "":
		e.said = turnWords{reply: words, final: strings.TrimSpace(sinceLastTool), cutAtLimit: cutAtLimit}
	case cutAtLimit:
		// A cut answer that said nothing is still the run's last answer, and it
		// did not finish.
		e.said.cutAtLimit = true
	}
}

// beginCorrection clears what decides the report before the run's one
// correction turn (standing_rules.go), which is judged on its own words.
func (e *firingEnd) beginCorrection() {
	e.said.final, e.said.cutAtLimit = "", false
	e.report, e.cut, e.ownReportWrite = turnReport{}, nil, false
}

// reportWithheld is the answer. The zero value withholds nothing; every other
// value is the one reason the run did not come back clean — for an order that
// keeps a report, the reason its report was not published.
type reportWithheld int

const (
	notWithheld reportWithheld = iota
	// withheldForAPerson: the run stopped on a call only a person can allow.
	withheldForAPerson
	// withheldCutOff: a turn ended on an error or on the pass's deadline.
	withheldCutOff
	// withheldOutputLimit: its answer stopped at the model's output limit and
	// the continuations ran out.
	withheldOutputLimit
	// withheldAtALimit: the step or spending limit stopped the run.
	withheldAtALimit
	// withheldUnclosed: its last report was opened and never closed.
	withheldUnclosed
	// withheldUnopened: it closed a report it never opened.
	withheldUnopened
	// withheldEmpty: its report between both lines had nothing in it.
	withheldEmpty
	// withheldSelfWrite: it tried to write its report file and replied with no
	// report between the lines.
	withheldSelfWrite
	// withheldNoReport: it ended on a tool call, with nothing said after it.
	withheldNoReport

	// The four below are decided after the turns, by the gates that own them,
	// and are the same answer carried on: the rules check held the report
	// (standing_rules.go), the person stopped the item while it ran, the report
	// could not be written, or the report file was not what aforge last wrote
	// there ([publishStandingReport]).
	withheldByRules
	withheldStopped
	withheldUnwritten
	withheldReportChanged
)

// withheldCodes is the one table of the codes a withheld run is recorded with
// (internal/standing's Occurrence.Withheld). THEY ARE IDENTIFIERS, NOT WORDS:
// a front end reads them instead of parsing the line a person reads, so a code
// is never renamed in passing — a test pins this exact set, and changing it is
// a deliberate act with a change entry.
var withheldCodes = map[reportWithheld]string{
	withheldForAPerson:  "waiting-on-person",
	withheldCutOff:      "cut-off",
	withheldOutputLimit: "output-limit",
	withheldAtALimit:    "at-a-limit",
	withheldUnclosed:    "unclosed-report",
	withheldUnopened:    "unopened-report",
	withheldEmpty:       "empty-report",
	withheldSelfWrite:   "self-write",
	withheldNoReport:    "no-report",
	withheldByRules:     "held-by-rules",
	withheldStopped:     "stopped",
	withheldUnwritten:   "not-written",
	// withheldReportChanged: the report file is not what aforge last published
	// there — somebody changed it, or it was there before aforge ever wrote it.
	withheldReportChanged: "report-changed",
}

// code is the answer as it is recorded; "" for a run nothing withheld.
func (w reportWithheld) code() string { return withheldCodes[w] }

// effectFence is what is rechecked immediately before each of aforge's own
// acts for a firing — the report's rename and the note's delivery — rather
// than once before both (the scale audit's law L2).
//
// THE WINDOW WAS REAL. The run's context was last read where the decision was
// taken and the item's status a few lines later; the report was written after
// both, by a writer that took neither, so a pass cancelled or an item stopped
// in between still published. The fence asks both questions again at the act,
// and asks the stop under the item's own lock ([standing.Store.UnlessStopped]),
// so a stop lands wholly before the act or wholly after it.
type effectFence struct {
	ctx context.Context
	// live says the decision was taken while the pass was still running, so a
	// cancellation seen at the act came between the two. A decision already cut
	// off has said so, and the note that says so is not held back by it.
	live bool
	// store and id are the item the stop is read from; a runner with no store
	// has nobody who could have said stop.
	store *standing.Store
	id    string
}

// do runs act unless the pass was cancelled since the decision or the item was
// stopped, answering which held it back. act answers a reason of its own when
// it declines ([publishStandingReport] on a changed file); a nil act only asks.
// A store whose lock cannot be taken is read as a store that cannot be read,
// which has never been a stop.
func (f effectFence) do(act func() (reportWithheld, error)) (reportWithheld, error) {
	var (
		held   reportWithheld
		failed error
	)
	gate := func() error {
		switch {
		case f.live && f.ctx.Err() != nil:
			held = withheldCutOff
		case act != nil:
			held, failed = act()
		}
		return nil
	}
	if f.store == nil || f.id == "" {
		_ = gate()
		return held, failed
	}
	stopped, err := f.store.UnlessStopped(f.id, gate)
	switch {
	case stopped:
		return withheldStopped, nil
	case err != nil:
		_ = gate()
	}
	return held, failed
}

// withheld answers whether this firing came back clean — for one that keeps a
// report, whether the report may be published — and why not when it did not.
// owesReport says the item keeps a report; deadline is the run's context error.
//
// THE ORDER IS THE ORDER OF THE EVIDENCE. A question for the person outranks
// everything, because it is work waiting on them rather than work that failed;
// an error outranks the rest, because nothing after it was finished. AN ANSWER
// CUT AT THE OUTPUT LIMIT IS NOT AN ANSWER, whatever lines it holds, and it is
// the answer's OWN turn that says so — the words' turn, or the report's turn —
// never a later turn that wrote neither. A LIMIT STOPS A RUN BEFORE IT SAID IT
// WAS DONE, which withholds even a closed report: one written before later tool
// work may be about a state that work had not yet reached. ALL OF THAT IS THE
// TRUTH ABOUT THE RUN, SO IT BINDS AN ORDER WITH NO REPORT TOO: owing no report
// is not itself a failure, and it is the only thing that exemption says (the
// second review, E2). A report's lines must be whole and hold something — AN
// EMPTY REPORT IS NO REPORT, and publishing one would erase the last good page. A FINISHED REPORT BETWEEN THE LINES STANDS even when the
// run also tried to write the file (the live review of 2026-09-10). WITHOUT
// ONE, a refused write of the report is the run trying to publish on its own
// authority, and what it said next is not a report. A run that said nothing and
// saved nothing came to nothing, as it always has.
func (e firingEnd) withheld(owesReport bool, deadline error) reportWithheld {
	switch {
	case e.needs != "":
		return withheldForAPerson
	case e.cut != nil || deadline != nil:
		return withheldCutOff
	case e.said.cutAtLimit || (owesReport && e.report.cutAtLimit):
		return withheldOutputLimit
	case e.capped:
		return withheldAtALimit
	case !owesReport:
		return notWithheld
	case e.report.lines == unclosedReport:
		return withheldUnclosed
	case e.report.lines == unopenedReport:
		return withheldUnopened
	case e.report.lines == closedReport && e.report.body == "":
		return withheldEmpty
	case e.report.lines == closedReport:
		return notWithheld
	case e.ownReportWrite:
		return withheldSelfWrite
	case e.said.reply == "" && !e.saved:
		return notWithheld
	case e.said.final == "":
		return withheldNoReport
	}
	return notWithheld
}

// why is the line a person reads for a withheld run, and the outcome's text.
// An order that keeps a report is also told its previous report stands; one
// that keeps none has no report to speak of.
func (e firingEnd) why(w reportWithheld, deadline error, owesReport bool) string {
	unchanged := ""
	if owesReport {
		unchanged = "; the previous report is unchanged"
	}
	switch w {
	case withheldForAPerson:
		return e.needs
	case withheldCutOff:
		cause := deadline
		if e.cut != nil {
			cause = e.cut
		}
		if cause == nil {
			cause = errors.New("the turn ended abnormally")
		}
		return "the run was cut off before it finished: " + oneLine(cause.Error())
	case withheldOutputLimit:
		return "the run's answer was cut off at the model's output limit" + unchanged
	case withheldAtALimit:
		return "the run reached its step or spending limit before it finished" + unchanged
	case withheldUnclosed:
		return "the run's report was never finished — it has no closing line; the previous report is unchanged"
	case withheldUnopened:
		return "the run's report has a closing line but no opening line; the previous report is unchanged"
	case withheldEmpty:
		return "the run's report was empty; the previous report is unchanged"
	case withheldSelfWrite:
		return "the run tried to write its report instead of replying with it; the previous report is unchanged"
	case withheldNoReport:
		return "the run ended without a report; the previous report is unchanged"
	}
	return ""
}

// outcome is what the firing came to, read off the one answer, with the
// answer's code beside it.
func (e firingEnd) outcome(w reportWithheld, deadline error, owesReport bool) standing.Outcome {
	outcome := standing.Outcome{
		Kind:     standingCameTo(e.saved, e.said.reply, e.needs),
		Text:     clip(e.said.reply, standingOutcomeClip),
		Withheld: w.code(),
	}
	switch w {
	case notWithheld:
	case withheldForAPerson:
		// NOTHING PRETENDS THIS LANDED. A run that stopped on something only a
		// person can allow is not a failure and is not a success; it is work
		// waiting for them, and home sorts on exactly that.
		outcome.NeedsPerson = e.needs
		if outcome.Text == "" {
			outcome.Text = e.needs
		}
	default:
		outcome.Kind = standing.OutcomeFailed
		outcome.Text = e.why(w, deadline, owesReport)
	}
	return outcome
}

// body is the report to publish when nothing withheld it: the closed report,
// or — when the run wrote no report lines at all — its final words, whole, so
// a model that ignores the request still publishes, with whatever it said.
func (e firingEnd) body() string {
	if e.report.lines == closedReport {
		return e.report.body
	}
	return strings.TrimSpace(e.said.final)
}

// delimitedReport answers the body of the LAST report in text and what its
// lines came to.
//
// THE REPORT IS DELIMITED, NOT GUESSED. The live journey's reports (2026-09-10)
// opened with the sentence the model said on its way to writing them, in the
// same turn as the report; cutting at the first heading would be a guess about
// what a report looks like. So a delimiter is a WHOLE LINE — `<report>` or
// `</report>` and nothing else on it once trimmed — and it is one only OUTSIDE
// A FENCED CODE BLOCK: a report may show the tags in an example, and a sentence
// may mention them, and neither opens, closes or cuts anything. The report runs
// from the last opening line to the first closing line after it. A REPORT THAT
// WAS NEVER CLOSED IS NO REPORT: its end is wherever the run stopped writing,
// which is half a page. Nor is one closed and never opened, whose start nobody
// can tell. (Every closing tag a real model wrote in the live runs, seventeen
// of them, was on a line of its own; one glued to other words is not a closing
// line, and its report reads as unclosed.)
func delimitedReport(text string) (string, reportLines) {
	lines := strings.Split(strings.TrimPrefix(text, "\uFEFF"), "\n")
	open, closing, strayClose := -1, -1, false
	var fence markdownFence
	for i, line := range lines {
		if fence.step(line) {
			continue
		}
		switch strings.TrimSpace(line) {
		case standingReportOpen:
			open, closing = i, -1
		case standingReportClose:
			switch {
			case open < 0:
				strayClose = true
			case closing < 0:
				closing = i
			}
		}
	}
	switch {
	case open < 0 && strayClose:
		return "", unopenedReport
	case open < 0:
		return "", noReportLines
	case closing < 0:
		return "", unclosedReport
	}
	return strings.TrimSpace(strings.Join(lines[open+1:closing], "\n")), closedReport
}

// markdownFence follows the fenced code blocks of a Markdown text one line at
// a time: a line of three or more backticks or tildes opens one, and a line of
// at least as many of the same character, with nothing after them, closes it.
type markdownFence struct {
	char  byte
	count int
}

// step reads one line and answers whether it belongs to a fence — the fence's
// own line, or a line inside one — and so can be no delimiter.
func (f *markdownFence) step(line string) bool {
	trimmed := strings.TrimSpace(line)
	run := 0
	if trimmed != "" && (trimmed[0] == '`' || trimmed[0] == '~') {
		for run < len(trimmed) && trimmed[run] == trimmed[0] {
			run++
		}
	}
	if f.count == 0 {
		if run >= 3 {
			f.char, f.count = trimmed[0], run
			return true
		}
		return false
	}
	if run >= f.count && trimmed[0] == f.char && strings.TrimSpace(trimmed[run:]) == "" {
		f.count = 0
	}
	return true
}

// writesTheReport answers whether a failed call was a write or an edit of the
// item's own report path — the exact file, however the call spelled it.
func writesTheReport(event Event, workspace, report string) bool {
	if report == "" || (event.Tool != "write" && event.Tool != "edit") {
		return false
	}
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(event.Args), &args) != nil || strings.TrimSpace(args.Path) == "" {
		return false
	}
	path := args.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	target := filepath.Join(workspace, report)
	if filepath.Clean(path) == filepath.Clean(target) {
		return true
	}
	// The same file through a symlinked workspace (/tmp on some systems).
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return false
	}
	resolved := path
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		resolved = filepath.Join(dir, filepath.Base(path))
	}
	return filepath.Clean(resolved) == filepath.Join(root, filepath.Clean(report))
}
