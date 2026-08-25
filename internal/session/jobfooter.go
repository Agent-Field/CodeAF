package session

// THE MODEL ALWAYS SEES WHERE ITS STREAMS ARE.
//
// A shell prints `[1]+ Running  make &` at the bottom of your prompt for a
// reason: work you started and cannot see is work you will either forget or
// start again. The model had neither. It made a call, got back an id, and from
// that moment the only way to learn anything about the job was to SPEND A CALL
// ASKING — which is what was measured, and what it cost: `sleep 30 && tail`, nine
// times, against a log that was empty for reasons the model could not see, ending
// in it killing work that was about to finish.
//
// So every tool result the model reads carries, at its foot, one line per
// outstanding job:
//
//	[job 1] running 3m12s · last: case 41/120 scored
//
// Three facts and no verbs. It is alive, it has been alive this long, and this is
// the last thing it said. A model reading that does not need to poll, cannot
// mistake a quiet job for a dead one, and is never surprised by a completion.
//
// ── WHERE IT IS APPENDED, AND WHY ONLY THERE ──
//
// At [Agent.executeTool]'s chokepoint, immediately after the error→fix sidecar
// (fixrecall.go), and nowhere else. That function is the ONE place every executed
// tool call passes through — the batch, and the early warm start that begins a
// read while the response is still streaming — so one call site covers every
// result the model will ever read, and a tool added next month gets the footer
// without anybody remembering to give it one. It is also, deliberately, the same
// seam the fix line uses: ONE PLACE THAT APPENDS TO RESULTS, so the order of the
// two additions is fixed and stripping them again is a question with one answer.
//
// ── AND IT IS NOT INFORMATION ABOUT THE WORK ──
//
// The footer changes on every result, because elapsed time changes. That makes it
// poison for anything measuring whether a node is getting anywhere: nine
// identical `(no output)` results with nine different footers are nine distinct
// strings and would read as nine discoveries. So [stripJobFooter] exists beside
// the renderer, and task_run.go's progress counter strips before it compares —
// stated here rather than there because the two have to be written against each
// other or they drift.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// jobFooterLead is how every footer line starts. It is the marker
// [stripJobFooter] recognises, so it is written once and matched against itself.
const jobFooterLead = "[job "

// jobFooterRunning is the word between the id and the elapsed time. It, too, is
// half of the strip's test: a line is a footer line only if it has both.
const jobFooterRunning = "] running "

// jobFooterLastLimit caps the quoted last line. It is [jobExitNoteLimit]'s
// figure for [jobExitNoteLimit]'s reason — this is a headline beside a status,
// not the output — and it is read from there rather than typed again.
const jobFooterLastLimit = jobExitNoteLimit

// runningFooter renders one line per job that is still going, or nothing at all.
//
// A TASK NODE IS NOT ON IT, for the reason [jobRegistry.announceRow] leaves one
// off the roster: a node already reports itself, in its own room, with its own
// row and its own landing note, and a second account of it here would be the same
// work counted twice in front of the same model.
func (r *jobRegistry) runningFooter() string {
	if r == nil {
		return ""
	}
	var lines []string
	for _, one := range r.all() {
		info := one.info()
		if info.state != jobRunning || info.kind == jobKindTask {
			continue
		}
		line := jobFooterLead + strconv.Itoa(info.id) + jobFooterRunning + formatJobAge(info.elapsed)
		if last := one.sink.lastNonEmptyLine(); last != "" {
			line += " · last: " + clip(firstLine(last), jobFooterLastLimit)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// formatJobAge is how long a job has been running, said the way a person says
// it. [formatElapsed] is the other one in this file's neighbourhood and is
// deliberately not reused: it renders `192.0s`, which is right for a tool row
// that finished in under a second and unreadable for a build in its fourth
// minute.
func formatJobAge(elapsed time.Duration) string {
	seconds := int(elapsed.Round(time.Second).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	hours, minutes, remainder := seconds/3600, (seconds%3600)/60, seconds%60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%02ds", minutes, remainder)
	default:
		return fmt.Sprintf("%ds", remainder)
	}
}

// withJobState is the append itself: the result the model was going to read,
// plus the footer, or the result untouched when nothing is outstanding.
//
// A session with no background work pays nothing and reads exactly what it read
// before — which is most sessions, and is why this is a suffix rather than a
// header the model has to learn to skip past.
func (a *Agent) withJobState(result toolResult) toolResult {
	if a == nil || a.jobs == nil {
		return result
	}
	footer := a.jobs.runningFooter()
	if footer == "" {
		return result
	}
	if strings.TrimSpace(result.text) == "" {
		result.text = footer
		return result
	}
	result.text = strings.TrimRight(result.text, "\n") + "\n\n" + footer
	return result
}

// stripJobFooter gives back the result body the model was actually answered
// with, without the job state that was appended to it.
//
// IT IS THE COUNTERPART OF [Agent.withJobState] AND MUST STAY ONE. Anything
// deciding whether a result is NEW has to compare bodies: the footer carries an
// elapsed time, so it differs on every single call, and a comparison that
// included it would report novelty for a result that had not changed a byte
// (task_run.go's [taughtSomething] — the defect this was written for was a model
// polling a job nine times, getting `(no output)` nine times, and having every
// one of them counted as progress).
//
// The test is deliberately narrow: only a run of lines at the very END, each of
// which begins `[job N] running `, is taken off. A result whose own content
// happens to mention a job is left exactly as it is.
func stripJobFooter(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	cut := len(lines)
	for cut > 0 && isJobFooterLine(lines[cut-1]) {
		cut--
	}
	if cut == len(lines) {
		return text
	}
	// The blank line the append put between the body and the footer goes with
	// it: it belongs to the footer, not to what the tool said.
	for cut > 0 && strings.TrimSpace(lines[cut-1]) == "" {
		cut--
	}
	return strings.Join(lines[:cut], "\n")
}

// isJobFooterLine reports whether one line is [jobRegistry.runningFooter]'s.
func isJobFooterLine(line string) bool {
	if !strings.HasPrefix(line, jobFooterLead) {
		return false
	}
	rest := line[len(jobFooterLead):]
	marker := strings.Index(rest, jobFooterRunning)
	if marker <= 0 {
		return false
	}
	// Everything between the lead and the marker has to be the id and nothing
	// else, so a sentence that merely opens with the same two words is not
	// mistaken for a footer.
	_, err := strconv.Atoi(rest[:marker])
	return err == nil
}
