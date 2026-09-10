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
	// reply is the last thing the run said, in whichever turn said something,
	// and final is what that turn said after its last tool call.
	reply, final string
	// report is the last report the run wrote between both lines; lines says
	// whether its last opened report was closed ([delimitedReport]).
	report string
	lines  reportLines
	// needs is the line the run stopped on when only a person could allow a
	// call.
	needs string
	// cut is the error a turn ended on.
	cut error
	// saved: a call saved something. capped: the step or spending limit
	// stopped the run. ownReportWrite: a write of its own report path was
	// refused, as every unattended write is. truncated: the last turn's answer
	// stopped at the output limit and its continuations ran out, as that
	// turn's own ending said ([Event.Truncated]).
	saved, capped, ownReportWrite, truncated bool
}

// readReport records the report lines in one reader's worth of text. Text that
// holds none leaves the last one standing: a run that wrote its report and
// then said something more has still written its report.
func (e *firingEnd) readReport(said string) {
	if body, lines := delimitedReport(said); lines != noReportLines {
		e.report, e.lines = body, lines
	}
}

// beginCorrection clears what decides the report before the run's one
// correction turn (standing_rules.go), which is judged on its own words.
func (e *firingEnd) beginCorrection() {
	e.final, e.report, e.lines, e.cut, e.ownReportWrite, e.truncated = "", "", noReportLines, nil, false, false
}

// reportWithheld is the answer. The zero value withholds nothing; every other
// value is the one reason a report was not published.
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

	// The three below are decided after the turns, by the gates that own them,
	// and are the same answer carried on: the rules check held the report
	// (standing_rules.go), the person stopped the item while it ran, or the
	// report could not be written.
	withheldByRules
	withheldStopped
	withheldUnwritten
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
}

// code is the answer as it is recorded; "" for a run nothing withheld.
func (w reportWithheld) code() string { return withheldCodes[w] }

// withheld answers whether this firing's report may be published, and why not
// when it may not. owesReport says the item keeps a report; deadline is the
// run's context error.
//
// THE ORDER IS THE ORDER OF THE EVIDENCE. A question for the person outranks
// everything, because it is work waiting on them rather than work that failed;
// an error outranks the rest, because nothing after it was finished. Past
// those, a firing with no report to keep has nothing to withhold. For one that
// has, AN ANSWER CUT AT THE OUTPUT LIMIT IS NOT AN ANSWER, whatever lines it
// holds, and A LIMIT WITHHOLDS EVEN A CLOSED REPORT: the run was stopped before
// it said it was done, and a report written before later tool work may be about
// a state that work had not yet reached. A report's lines must be whole and
// hold something — AN EMPTY REPORT IS NO REPORT, and publishing one would erase
// the last good page. A FINISHED REPORT BETWEEN THE LINES STANDS even when the
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
	case !owesReport:
		return notWithheld
	case e.truncated:
		return withheldOutputLimit
	case e.capped:
		return withheldAtALimit
	case e.lines == unclosedReport:
		return withheldUnclosed
	case e.lines == unopenedReport:
		return withheldUnopened
	case e.lines == closedReport && e.report == "":
		return withheldEmpty
	case e.lines == closedReport:
		return notWithheld
	case e.ownReportWrite:
		return withheldSelfWrite
	case e.reply == "" && !e.saved:
		return notWithheld
	case e.final == "":
		return withheldNoReport
	}
	return notWithheld
}

// why is the line a person reads for a withheld report, and the outcome's text.
func (e firingEnd) why(w reportWithheld, deadline error) string {
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
		return "the run's answer was cut off at the model's output limit; the previous report is unchanged"
	case withheldAtALimit:
		return "the run reached its step or spending limit before it finished; the previous report is unchanged"
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

// outcome is what the firing came to, read off the one answer.
func (e firingEnd) outcome(w reportWithheld, deadline error) standing.Outcome {
	outcome := standing.Outcome{
		Kind: standingCameTo(e.saved, e.reply, e.needs),
		Text: clip(e.reply, standingOutcomeClip),
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
		outcome.Text = e.why(w, deadline)
	}
	return outcome
}

// recordWithheld writes the answer onto an outcome for a firing that keeps a
// report. A firing with no report has nothing to withhold, so its record never
// carries a code (the emptiness law, for a field a front end reads).
func recordWithheld(outcome *standing.Outcome, w reportWithheld, owesReport bool) {
	outcome.Withheld = ""
	if owesReport {
		outcome.Withheld = w.code()
	}
}

// body is the report to publish when nothing withheld it: the closed report,
// or — when the run wrote no report lines at all — its final words, whole, so
// a model that ignores the request still publishes, with whatever it said.
func (e firingEnd) body() string {
	if e.lines == closedReport {
		return e.report
	}
	return strings.TrimSpace(e.final)
}

// delimitedReport answers the body of the LAST report in text and what its
// lines came to.
//
// THE REPORT IS DELIMITED, NOT GUESSED. The live journey's reports (2026-09-10)
// opened with the sentence the model said on its way to writing them, in the
// same turn as the report; cutting at the first heading would be a guess about
// what a report looks like. The opening must be a line of its own — so a
// sentence that merely mentions the tag opens nothing — and the report runs to
// the first closing tag after it, wherever on its line that falls. A REPORT
// THAT WAS NEVER CLOSED IS NO REPORT: its end is wherever the run stopped
// writing, which is half a page.
func delimitedReport(text string) (string, reportLines) {
	lines := strings.Split(text, "\n")
	open := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == standingReportOpen {
			open = i
		}
	}
	if open < 0 {
		// A CLOSING TAG WITH NO OPENING LINE IS NOT "NO LINES". The run meant
		// to delimit a report and did not open it, so there is no telling where
		// the report began; taking the whole answer would publish the narration
		// in front of it and the tag itself.
		if strings.Contains(text, standingReportClose) {
			return "", unopenedReport
		}
		return "", noReportLines
	}
	body := strings.Join(lines[open+1:], "\n")
	end := strings.Index(body, standingReportClose)
	if end < 0 {
		return "", unclosedReport
	}
	return strings.TrimSpace(body[:end]), closedReport
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
