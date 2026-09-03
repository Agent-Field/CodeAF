package tui3

// ── A PAGE WITH NO COMPOSER ENDS WHERE ITS CONTENT ENDS ─────────────────────
//
// The task's record card and a job's page are the two surfaces here that draw a
// head, a body and a foot and have NOTHING under them: no composer, no place
// strip, no tab bar. Both padded the body out to the bottom of the terminal and
// pinned the rule and the key row there — so a six-line card in a fifty-row
// frame drew seventeen blank rows and put `esc back` on row thirty, and the
// reader's eye travelled the whole frame to find the foot of a page that had
// ended at line eleven (docs/design/polish/audit-tasks.md row 5).
//
// A PLACE IS THE OTHER CASE AND IS NOT COVERED HERE. The tasks LIST has a
// composer under it at a fixed row, which may not move (composerlayer.go), so
// the space between a short list and its rule there is honest emptiness and not
// a defect.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAPageWithNoComposerPutsItsFootUnderTheLastRowItDrew(t *testing.T) {
	for _, width := range []int{160, 120, 80, 60} {
		card := newTestApp(&fakeAgent{model: "m"})
		card.width, card.height = width, 50
		card.raisePlace(pageTasks)
		card.taskSheet = tasksPlace{detailOn: true, detail: session.TaskIndexEntry{
			ID: "1", Title: "Port the picker onto the new list",
			Label:  "Port the picker onto the new list",
			Status: string(session.TaskDone), DurationMS: 360000,
		}}
		w, h := card.size()
		lines, _, _, _ := card.taskCardFrame(w, h)
		footRidesUp(t, "the record card", width, lines, h, taskCardFoot)

		job := jobPageApp(t, jobNotice(3, session.JobFailed, "make check",
			jobLogFile(t, "go vet ./...", "make: *** [Makefile:41: vet] Error 1")))
		job.width, job.height = width, 50
		w, h = job.size()
		lines, _, _, _ = job.jobPageFrame(w, h)
		footRidesUp(t, "the job page", width, lines, h, jobPageFoot)
	}
}

// footRidesUp fails unless the frame stops at its own last row: shorter than the
// terminal, with the closing rule directly under the last row of the body rather
// than a run of blanks away from it.
func footRidesUp(t *testing.T, page string, width int, lines []string, height, foot int) {
	t.Helper()
	drawn := drawnRows(lines)
	if len(lines) >= height {
		t.Fatalf("%s at %d columns drew %d rows into a %d-row frame — a page with nothing under it pads to the bottom for nobody:\n%s",
			page, width, len(lines), height, strings.Join(drawn, "\n"))
	}
	if len(lines) < 5 {
		t.Fatalf("%s at %d columns drew %d rows and there is no foot to find:\n%s",
			page, width, len(lines), strings.Join(drawn, "\n"))
	}
	// The last row is the keys, the one above it the closing rule, and the one
	// above THAT is the last thing the page had to say. A blank there is the
	// padding this row is about.
	if last := strings.TrimSpace(drawn[len(drawn)-1]); last == "" {
		t.Fatalf("%s at %d columns ends on a blank row rather than on its keys:\n%s",
			page, width, strings.Join(drawn, "\n"))
	}
	at := len(drawn) - foot
	rule := strings.TrimSpace(drawn[at])
	if rule == "" || strings.Trim(rule, "─-") != "" {
		t.Fatalf("%s at %d columns does not close on a rule under its last row; row %d is %q:\n%s",
			page, width, at, drawn[at], strings.Join(drawn, "\n"))
	}
	if body := strings.TrimSpace(drawn[at-1]); body == "" {
		t.Fatalf("%s at %d columns still pads: the row above its closing rule is blank:\n%s",
			page, width, strings.Join(drawn, "\n"))
	}
}

// AND A PAGE THAT SCROLLS KEEPS ITS PINNED FOOT, because there the bottom of the
// frame IS where the content ends. This is the half the fix must not have
// taken: a log longer than the room fills it, and the rule and the keys sit on
// the last two rows of the terminal exactly as they always did.
func TestAPageWhoseBodyScrollsStillFillsTheFrame(t *testing.T) {
	long := make([]string, 200)
	for i := range long {
		long[i] = "09:28:44 [vite] GET /jobs/" + itoa(i) + " 200 in 15ms"
	}
	job := jobPageApp(t, jobNotice(3, session.JobRunning, "npm run dev", jobLogFile(t, long...)))
	for _, height := range []int{24, 40, 50} {
		job.width, job.height = 120, height
		w, h := job.size()
		if view := job.jobView(); view != nil {
			view.top = 0
		}
		lines, _, _, _ := job.jobPageFrame(w, h)
		drawn := drawnRows(lines)
		if len(lines) != h {
			t.Fatalf("a job page with a log of %d lines drew %d rows into a %d-row frame — a body that scrolls fills the frame:\n%s",
				len(long), len(lines), h, strings.Join(drawn, "\n"))
		}
		// AND THE WINDOW IS THE ONE THE SCROLL ASKED FOR. The body starts at the
		// top of the log because that is where the page is scrolled to; a frame
		// that drew every line and let the trim keep the tail would be the same
		// height and a different page.
		if first := strings.TrimSpace(drawn[3]); !strings.HasPrefix(first, jobPageHandle(3)) {
			t.Fatalf("the first row under the rule is %q, not the top of the body:\n%s", first, strings.Join(drawn, "\n"))
		}
		if foot := strings.TrimSpace(drawn[len(drawn)-1]); !strings.Contains(foot, "scroll") {
			t.Fatalf("a scrolling job page's last row is %q, not its key sheet", foot)
		}
	}
}
