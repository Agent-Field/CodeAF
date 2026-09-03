package tui3

// ── ONE WORD FOR ONE JOB, AND NO MARK THAT OPENS NOTHING ────────────────────
//
// The jobs section and the job page are one keypress apart, and for a while
// they said different things about the same process: the column read
// [jobStateWord] and the page rendered `string(job.State)`, the engine's own
// enum, with nothing between it and the screen
// (docs/design/polish/audit-tasks.md rows 11 and 16).

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE COLUMN AND THE PAGE SAY ONE WORD ABOUT HOW A JOB ENDED. `3 · exited 1` in
// the section and `job 3 · exited 1 · ran 32s` on the page — one function, so a
// new [session.JobState] cannot leak its spelling onto a person's screen by
// being added.
func TestTheJobsColumnAndItsPageSayOneWordAboutHowAJobEnded(t *testing.T) {
	now := taskFixtureNow.Add(time.Minute)
	for _, tc := range []struct {
		state session.JobState
		exit  int
		want  string
	}{
		{session.JobFailed, 1, jobsExitedWord + " 1"},
		{session.JobFailed, 0, jobsExitedWord + " 0"},
		{session.JobStopped, 0, jobsStoppedWord},
		{session.JobDone, 0, jobsDoneWord},
	} {
		job := jobNotice(3, tc.state, "make check", "")
		job.ExitCode = tc.exit
		row := plain(jobRowLine(job, now, 60, jobViewPal()))
		if !strings.Contains(row, tc.want) {
			t.Fatalf("the column's row for a %s job is\n\t%q\nand it should carry %q", tc.state, row, tc.want)
		}
		page := jobPageEnding(job, "32s")
		if !strings.Contains(page, tc.want) {
			t.Fatalf("the page for a %s job says\n\t%q\nand the column beside it says %q — one job, one word",
				tc.state, page, tc.want)
		}
		// AND NEVER THE ENUM. `failed` is what the engine calls the state and it
		// is not a word this surface may print: the column has never said it and
		// the page may not either.
		if enum := strings.TrimSpace(string(tc.state)); enum != tc.want && strings.Contains(page, enum) {
			t.Fatalf("the page for a %s job says\n\t%q\nwhich carries the engine's own enum %q", tc.state, page, enum)
		}
		// AND THE EXIT CODE IS SAID ONCE. `exited 1` already carries it; the page
		// used to append `exit 1` behind it.
		if strings.Contains(page, "exit "+itoa(tc.exit)) && tc.want != "exit "+itoa(tc.exit) {
			t.Fatalf("the page for a %s job says\n\t%q\nand states the exit code twice", tc.state, page)
		}
	}
}

// THE OVERFLOW COUNT WEARS NO MARK IT CANNOT OPEN. `▸ 4 earlier` sat under a
// section whose head is also a `▸`, which invites a keypress that does nothing —
// and [jobEarlierLine]'s own doc comment said in so many words that it opens
// nothing while the line drew the mark that promises it does.
func TestTheJobsOverflowCountWearsNoMarkItCannotOpen(t *testing.T) {
	ascii := jobViewPal()
	ascii.ascii = true
	for _, pal := range []palette{jobViewPal(), ascii} {
		line := plain(jobEarlierLine(4, 40, pal))
		if !strings.Contains(line, "4 "+jobsEarlierWord) {
			t.Fatalf("the overflow line is %q and it should count what it hid: %q", line, "4 "+jobsEarlierWord)
		}
		for _, mark := range []string{glyphShut, glyphOpen, glyphShutASCII, glyphOpenASCII} {
			if strings.Contains(line, mark) {
				t.Fatalf("the overflow line is %q and it wears the fold mark %q, which opens nothing", line, mark)
			}
		}
		// AND IT SITS UNDER THE ROWS IT COUNTS, at their own indent, so the
		// section's head keeps the only mark on it.
		if !strings.HasPrefix(line, jobRowIndent+"4") {
			t.Fatalf("the overflow line is %q and it does not sit at the rows' indent %q", line, jobRowIndent)
		}
	}
}
