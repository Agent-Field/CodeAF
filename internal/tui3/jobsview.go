package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE JOBS SECTION AS A READING.
//
// THE LAW THIS FILE EXISTS FOR: a column section is a function of its facts.
// What is running, what has ended, whether the person opened it, the instant
// this frame is drawn at, the width and the rows it may spend — that is the
// whole input. There is no window behind it, no disk, and no second read of
// the clock. That is the same law placelaws_test.go pins for the place
// readings, and it is what makes the section testable with no model and no
// terminal.
//
// The app-side half (jobsection.go) holds the toggle, routes the keys and
// clicks, and calls this. It does not format a string a person sees.

// The section's own words. They are the vocabulary the rest of the product
// already uses for these facts — a job is running or it ran, and what did not
// fit is earlier — and they are quoted in the tests exactly as they are spelled
// here.
const (
	jobsRunningWord = "running"
	jobsRanWord     = "ran"
	jobsEarlierWord = "earlier"
	// jobsExitedWord is a failed job's ending: the process left a non-zero code,
	// and that code is the news.
	jobsExitedWord = "exited"
	// jobsStoppedWord is a job somebody ended rather than one that finished.
	// It is its own word because "it failed" and "you stopped it" are different
	// news about a process that is equally not running.
	jobsStoppedWord = "stopped"
	// jobsDoneWord is a clean finish. It is a word of its own — not the
	// duration alone — because a frozen clock and a ticking one are the same
	// shape at a glance, and a column of eleven sleeps needs a state a person
	// can read without watching the digits move. How long it ran lives on the
	// page ([jobPageEnding]).
	jobsDoneWord = "done"
	// jobRowIndent is the two cells every row of this section sits behind, and
	// the overflow count sits behind them too so it lines up under the rows it
	// is counting rather than under the head that opened them.
	jobRowIndent = "  "
)

// jobSectionRows is the jobs section as the column draws it.
//
// ZERO JOBS IS ZERO ROWS. A label over nothing is an announcement of absence,
// which the emptiness law forbids — no `0 jobs`, no empty row, no chrome.
//
// COLLAPSED IT IS EXACTLY ONE LINE OF NEWS under the blank that separates it
// from what is above: the fold, the word, the live count, a clock only when
// there is exactly one duration to name, and history as a count. History is
// never rows while the section is shut.
//
// EXPANDED, RUNNING WORK IS NEVER DROPPED. Every live job draws, oldest first,
// and finished jobs then fill whatever room is left, newest first. What does
// not fit is counted on a `▸ N earlier` line rather than silently dropped.
// That is [marginJobsFit]'s law, and it is [marginStandFit]'s law applied to
// this section rather than a second budget algebra.
func jobSectionRows(live, settled []session.JobNotice, open bool, now time.Time, width, room int, pal palette) []railLine {
	if len(live)+len(settled) == 0 {
		return nil
	}
	if !open {
		if room < marginJobsCost {
			return nil
		}
		return []railLine{
			{entry: -1},
			{text: jobSectionHead(live, settled, false, now, width, pal), entry: -1, jobs: true},
		}
	}
	// OPEN: the chrome and every running job draw even when the budget is
	// short, because a section that hid live work would be the column saying
	// nothing about what is happening. History spends what is left.
	out := make([]railLine, 0, marginJobsCost+len(live)+len(settled)+1)
	out = append(out, railLine{entry: -1})
	out = append(out, railLine{
		text:  jobSectionHead(live, settled, true, now, width, pal),
		entry: -1,
		jobs:  true,
	})
	for _, job := range live {
		out = append(out, railLine{
			text:  jobRowLine(job, now, width, pal),
			entry: -1,
			job:   job.ID,
		})
	}
	shown := marginJobsFit(len(settled), room, len(live))
	if shown > len(settled) {
		shown = len(settled)
	}
	for _, job := range settled[:shown] {
		out = append(out, railLine{
			text:  jobRowLine(job, now, width, pal),
			entry: -1,
			job:   job.ID,
		})
	}
	if hidden := len(settled) - shown; hidden > 0 && room-marginJobsCost-len(live)-shown > 0 {
		out = append(out, railLine{
			text:  jobEarlierLine(hidden, width, pal),
			entry: -1,
		})
	}
	return out
}

// jobSectionHead is the label, with the live count, an optional clock, and the
// history count riding on it.
//
//	▸ jobs · 2 running
//	▸ jobs · 1 running · 4m12s
//	▸ jobs · 2 running · 6 ran
//	▸ jobs · 6 ran
//
// IT IS THE STANDING LABEL'S OWN SHAPE ([app.marginStandHead]): the figure in
// the data ink and the words around it dim. The clock appears only when
// exactly one job is running, because then there is one duration to name;
// past that the count is the news and a clock beside it would belong to no
// particular row.
func jobSectionHead(live, settled []session.JobNotice, open bool, now time.Time, width int, pal palette) string {
	mark := jobFoldMark(open, pal) + " "
	type bit struct {
		s     string
		paint func(string) string
	}
	bits := []bit{{mark + marginJobsWord, pal.dim}}
	if n := len(live); n > 0 {
		bits = append(bits,
			bit{railSep, pal.dim},
			bit{itoa(n), pal.data},
			bit{" " + jobsRunningWord, pal.dim},
		)
		if n == 1 {
			if clock := jobClock(live[0], now); clock != "" {
				bits = append(bits,
					bit{railSep, pal.dim},
					bit{clock, pal.data},
				)
			}
		}
	}
	if n := len(settled); n > 0 {
		bits = append(bits,
			bit{railSep, pal.dim},
			bit{itoa(n), pal.data},
			bit{" " + jobsRanWord, pal.dim},
		)
	}
	var plain strings.Builder
	for _, b := range bits {
		plain.WriteString(b.s)
	}
	if ansi.StringWidth(plain.String()) > width {
		return pal.dim(fit(mark+marginJobsWord, width))
	}
	var out strings.Builder
	for _, b := range bits {
		out.WriteString(b.paint(b.s))
	}
	return out.String()
}

// jobRowLine is one job as one line: the name on the left and a dim figure on
// the right, which is the roster's own row grammar ([app.railEntryRows]) rather
// than a second one. The figure always leads with the job's own number — the
// handle `jobs kill` and `job:3` take — so eleven similar commands stay
// distinguishable, and then the clock or the ending.
func jobRowLine(job session.JobNotice, now time.Time, width int, pal palette) string {
	meta := jobRightWord(job, now)
	indent := jobRowIndent
	room := width - ansi.StringWidth(indent)
	if meta != "" && room-ansi.StringWidth(meta)-1 < railTitleFloor {
		// KEEP THE HANDLE even when the status will not fit: a row that lost
		// its number is a row a person cannot aim `jobs kill` at by sight.
		meta = itoa(job.ID)
	}
	if meta != "" {
		room -= ansi.StringWidth(meta) + 1
	}
	if room < 1 {
		return pal.dim(indent)
	}
	title := fit(job.Label(), room)
	paint := pal.muted
	if !job.Over() {
		paint = pal.ink
	}
	line := indent + paint(title)
	if meta == "" {
		return line
	}
	if pad := room - ansi.StringWidth(title) + 1; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + pal.dim(meta)
}

// jobEarlierLine is the remainder the column could not fit, counted rather than
// dropped. It is not a fold and it opens nothing: there is no second fold level
// on this section.
//
// SO IT WEARS NO FOLD MARK. It carried a `▸` under a section whose head is also
// a `▸`, which invited a keypress that does nothing — the one state this surface
// is not allowed to be in — and the doc comment above said in so many words that
// it opens nothing while the line drew the mark that promises it does
// (docs/design/polish/audit-tasks.md row 16). The count sits at the ROWS' own
// indent, under the rows it counts, so the head's `▸` means exactly one thing on
// this section.
func jobEarlierLine(n, width int, pal palette) string {
	return pal.dim(fit(jobRowIndent+itoa(n)+" "+jobsEarlierWord, width))
}

// jobRightWord is the figure on the right of a job row. It always carries the
// job's number, then the clock while the process runs or the word for how it
// ended — so the column answers "which job" and "is it still going" in one
// glance without a second column of chrome.
//
//	3 · 49s
//	3 · done
//	3 · exited 1
//	3 · stopped
func jobRightWord(job session.JobNotice, now time.Time) string {
	id := itoa(job.ID)
	status := jobStateWord(job)
	if status == "" {
		status = jobClock(job, now)
	}
	if status == "" {
		return id
	}
	return id + railSep + status
}

// jobStateWord is how a job ENDED, in the words above, or "" for one that has
// not — a running job has a clock and no ending yet.
//
// IT IS THE ONE ANSWER THE COLUMN AND THE PAGE BOTH READ. The page used to
// render `strings.TrimSpace(string(job.State))`, which is the engine's own enum
// with nothing between it and the screen, so the same failed job read `3 ·
// exited 1` in the column and `job 3 · failed · exit 1` on the page one keypress
// away — two words for one fact, and a new [session.JobState] would have leaked
// its spelling onto a person's screen the day it was added
// (docs/design/polish/audit-tasks.md row 11).
func jobStateWord(job session.JobNotice) string {
	switch job.State {
	case session.JobFailed:
		return jobsExitedWord + " " + itoa(job.ExitCode)
	case session.JobStopped:
		return jobsStoppedWord
	case session.JobDone:
		return jobsDoneWord
	}
	return ""
}

// jobClock is how long this job has been running, or ran, in the column's
// compact duration form. Under a second there is no waiting to report, and
// "0s" on a row that has just begun is a figure that has to be read to learn
// nothing.
//
// IT COUNTS FROM Started FOR A LIVE JOB, which is why that field is on the
// notice: a surface with only Elapsed would have to re-ask the engine on every
// frame, and with Started it counts up on the paint clock and asks nothing.
func jobClock(job session.JobNotice, now time.Time) string {
	age := job.Elapsed
	if !job.Over() && !job.Started.IsZero() {
		age = now.Sub(job.Started)
	}
	if age < time.Second {
		return ""
	}
	return tokens.Duration(age)
}

// jobFoldMark is ▸ or ▾, and the ASCII stand-in on a terminal that cannot be
// trusted with those, or on the linear tier that spells a shape out loud.
func jobFoldMark(open bool, pal palette) string {
	if pal.ascii || pal.linear {
		if open {
			return glyphOpenASCII
		}
		return glyphShutASCII
	}
	if open {
		return glyphOpen
	}
	return glyphShut
}
