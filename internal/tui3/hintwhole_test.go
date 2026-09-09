package tui3

// ── THE JOB PAGE AND THE RECORD CARD DROP WHOLE HINTS ───────────────────────
//
// Both feet were painted through a plain [fit], which is a character ruler with
// no idea what a clause is, so a narrow terminal ended them `· m puts it in
// yo…` — a sheet that names a key and then eats it. The structural law in
// narrow_test.go now covers every foot on the surface with an empty ledger;
// these are the two behaviours behind it, said as a person meets them.

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE WAY OUT IS NAMED ONCE PER FRAME, AND ALWAYS ONCE.
//
// Two laws met on these two feet and they were read as a conflict. One: the way
// out is the LAST clause of a key row, because [hintFit] keeps its final clause
// to the last cell there is and spends the ones in front of it first — a foot
// that led with `esc back` was a foot that gave the way out away first, and a
// sixty-cell card ended up offering `m puts it in your message` and no way off
// the page. Two: `esc back` was drawn TWICE on every frame of these pages, once
// in the head's right corner and once at the end of the foot, which on a
// six-line card spent two of its sixteen words repeating one instruction
// (docs/design/polish/audit-tasks.md row 15).
//
// They do not actually conflict, and this is the resolution: THE FOOT CARRIES
// THE WAY OUT ONLY WHERE THE HEAD DOES NOT. The head keeps its corner at almost
// every width, so almost every foot is the `Held` sheet; where the title has
// eaten the corner — or the frame is too narrow to have one — the foot takes
// the way out back, at the END, where the first law wants it. So the narrow
// case is not weakened: it is the case that still spends `esc back` last.
func TestTheJobPageAndTheRecordCardNameTheWayOutOnceOnEveryFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		// keys is the sheet drawn where the head has no corner, held the sheet
		// drawn where it has one, and out the clause that says how to leave.
		keys, held, out string
	}{
		{"a running job", jobPageKeysRun, jobPageKeysRunHeld, jobPageBackWord},
		{"a settled job", jobPageKeysOver, jobPageKeysOverHeld, jobPageBackWord},
		{"a task's record card", taskCardKeys, taskCardKeysHeld, taskCardBackWord},
		// And the same card over work another window is running, which has no
		// mention to offer ([taskAwayCardKeys] says why) and obeys both laws with
		// one clause fewer.
		{"a task's card over another window's work", taskAwayCardKeys, taskAwayCardKeysHeld, taskCardBackWord},
	} {
		// LAW ONE, on the sheet that carries it: the way out is last, because it
		// is the clause the fitter keeps.
		if !strings.HasSuffix(tc.keys, tc.out) {
			t.Fatalf("%s's foot is\n\t%q\nand the clause that says how to leave has to be last, because that is the one the fitter keeps: %q",
				tc.name, tc.keys, tc.out)
		}
		// AND THE HELD SHEET IS THE SAME KEYS WITH THAT CLAUSE GONE — every clause
		// of one is a clause of the other, and none of them is re-spelled, because
		// a foot that said a key two ways would be two sheets to learn. What DOES
		// move is their order: the held sheet ends on the cheapest clause worth
		// keeping rather than on the way out, because [hintFit] protects whatever
		// is last and slices it if it will not fit (jobpage.go states it).
		if strings.Contains(tc.held, tc.out) {
			t.Fatalf("%s's held foot still names the way out:\n\t%q", tc.name, tc.held)
		}
		full := strings.Split(strings.TrimSuffix(tc.keys, railSep+tc.out), railSep)
		held := strings.Split(tc.held, railSep)
		sort.Strings(full)
		sort.Strings(held)
		if !slices.Equal(full, held) {
			t.Fatalf("%s's held foot is\n\t%q\nand its clauses are not the ones on\n\t%q", tc.name, tc.held, tc.keys)
		}
		for _, width := range []int{160, 120, 80, 60, 40, 24} {
			for _, sheet := range []string{tc.keys, tc.held} {
				line := hintFit(sheet, width-2)
				if ansi.StringWidth(line) > width-2 {
					t.Fatalf("%s's foot at %d columns is %d cells:\n\t%q", tc.name, width, ansi.StringWidth(line), line)
				}
				if strings.Contains(line, glyphMore) {
					t.Fatalf("%s's foot at %d columns is cut mid-clause:\n\t%q\nhints drop whole clauses", tc.name, width, line)
				}
				// EVERY CLAUSE THAT SURVIVED IS ONE THAT WAS THERE. A foot that
				// re-spelled a key at a narrow width would be two sheets to learn.
				for _, part := range strings.Split(line, railSep) {
					if !strings.Contains(sheet, part) {
						t.Fatalf("%s's foot at %d columns says %q, which is not a clause of\n\t%q", tc.name, width, part, sheet)
					}
				}
			}
			// THE NARROW CASE, UNWEAKENED: on the sheet the foot draws when the
			// head has no corner, the way out survives every width a person can
			// read at, ahead of every key in front of it.
			if line := hintFit(tc.keys, width-2); width > 24 && !strings.Contains(line, tc.out) {
				t.Fatalf("%s's foot at %d columns is\n\t%q\nand it gave away the way out, %q, before the keys in front of it",
					tc.name, width, line, tc.out)
			}
		}
	}
}

// AND THAT IS TRUE OF THE FRAME A PERSON ACTUALLY LOOKS AT, not only of the
// constants: on every frame of both pages, `esc back` is drawn exactly once —
// in the head where the head has room for it, and on the foot where it has not.
//
// The wide case is the head's; the narrow case here is a TITLE long enough to
// take the whole line, which is how the head loses its corner at any width.
func TestBothPagesDrawTheWayOutInTheHeadOrOnTheFootAndNeverBoth(t *testing.T) {
	const long = "Cut every list on the task surface over to the shared row fitter so a name is never cut to nine cells"
	for _, title := range []string{"port the picker", long} {
		for _, width := range []int{160, 120, 80, 60} {
			card := newTestApp(&fakeAgent{model: "m"})
			card.width, card.height = width, 40
			card.raisePlace(pageTasks)
			card.taskSheet = tasksPlace{detailOn: true, detail: session.TaskIndexEntry{
				ID: "1", Title: title, Label: title, Status: string(session.TaskDone),
			}}
			w, h := card.size()
			lines, _, _, _ := card.taskCardFrame(w, h)
			countTheWayOut(t, "the record card", title, width, lines, taskCardBackWord)

			notice := jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", jobLogFile(t))
			notice.Name = title
			job := jobPageApp(t, notice)
			job.width, job.height = width, 40
			w, h = job.size()
			lines, _, _, _ = job.jobPageFrame(w, h)
			countTheWayOut(t, "the job page", title, width, lines, jobPageBackWord)
		}
	}
}

// drawnRows is a frame with its ink taken off, for a failure message.
func drawnRows(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = plain(line)
	}
	return out
}

// countTheWayOut fails unless exactly one row of the frame names the way out.
func countTheWayOut(t *testing.T, page, title string, width int, lines []string, out string) {
	t.Helper()
	var said []int
	for at, line := range lines {
		if strings.Contains(ansi.Strip(line), out) {
			said = append(said, at)
		}
	}
	if len(said) == 1 {
		return
	}
	t.Fatalf("%s at %d columns, titled %q, names %q on %d rows (%v) and it has to be exactly one — the head where the head has a corner, the foot where it has not:\n%s",
		page, width, title, out, len(said), said, strings.Join(drawnRows(lines), "\n"))
}

// AND THE RANK IS THE ONE THE PAGE ARGUES FOR: a job that is still running keeps
// `x stop it` longest of the four, because stopping something is the one thing on
// that page a person cannot reach from anywhere else.
func TestARunningJobsFootKeepsStopLongestOfItsFourKeys(t *testing.T) {
	line := hintFit(jobPageKeysRun, 44)
	if !strings.Contains(line, "x stop it") {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it dropped %q before the keys behind it", line, "x stop it")
	}
	if strings.Contains(line, "m puts it in your message") {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it is wider than the clauses it should have dropped", line)
	}
	if !strings.HasSuffix(line, jobPageBackWord) {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it should still end in %q", line, jobPageBackWord)
	}
}
