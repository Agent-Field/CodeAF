package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// summaryPlanFake is the store-backed plan seam with every call counted. The
// clock is test data: this suite never sleeps.
type summaryPlanFake struct {
	*planFake
	summary session.RunPlanSummary
	stale   bool
	planned int
	called  int
	gate    chan struct{}
}

func (f *summaryPlanFake) PlanRunSummary(string) (session.RunPlanSummary, bool) {
	f.planned++
	return f.summary, f.stale
}

func (f *summaryPlanFake) RefreshRunSummary(ctx context.Context, _ string, _ time.Time) (session.RunPlanSummary, bool) {
	f.called++
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return session.RunPlanSummary{}, false
		}
	}
	return f.summary, true
}

func summaryApp(t *testing.T, stale bool) (*app, *summaryPlanFake, *time.Time) {
	t.Helper()
	base, raw := planAppWith(t, c263PlanRows(), nil)
	at := taskFixtureNow
	fake := &summaryPlanFake{planFake: raw, stale: stale, summary: session.RunPlanSummary{What: "the run", Now: "checking the narrow rail without touching the frame", WrittenAt: at}}
	base.agent = fake
	base.clock = func() time.Time { return at }
	return base, fake, &at
}

func TestRunSummaryRefreshIsOffFrameStaleOnlySingleFlightAndMinuteThrottled(t *testing.T) {
	a, fake, at := summaryApp(t, true)
	fake.gate = make(chan struct{})
	a.taskSheet.mine.plan = c263PlanRows()

	first := a.refreshRunSummary()
	if first == nil {
		t.Fatal("a run whose shape moved returned no off-loop command")
	}
	// THE LOOP NEVER OPENS THE STORE FOR THIS. It runs after every message, so
	// deciding whether to ask reads the rows the task sheet already carries.
	if fake.planned != 0 || fake.called != 0 {
		t.Fatalf("making the command read the store %d times and called the model %d times", fake.planned, fake.called)
	}
	if second := a.refreshRunSummary(); second != nil {
		t.Fatal("a refresh already in flight admitted a second command")
	}
	close(fake.gate)
	drive(t, a, first())
	if fake.called != 1 {
		t.Fatalf("refresh calls = %d, want one", fake.called)
	}
	if a.runSummaryNow != fake.summary.Now {
		t.Fatalf("the now sentence did not reach the loop: %q", a.runSummaryNow)
	}

	// NOTHING MOVED: no command, however long it has been.
	*at = (*at).Add(10 * runSummaryRefreshEvery)
	if cmd := a.refreshRunSummary(); cmd != nil {
		t.Fatal("a run whose shape has not moved paid for another look")
	}

	// SOMETHING MOVED INSIDE THE MINUTE: held until the minute is up.
	b, other, clock := summaryApp(t, true)
	b.taskSheet.mine.plan = c263PlanRows()
	drive(t, b, b.refreshRunSummary()())
	moved := c263PlanRows()
	moved[2].Status = "done"
	b.taskSheet.mine.plan = moved
	*clock = (*clock).Add(runSummaryRefreshEvery - time.Nanosecond)
	if cmd := b.refreshRunSummary(); cmd != nil {
		t.Fatal("summary refreshed inside its named once-a-minute throttle")
	}
	*clock = (*clock).Add(time.Nanosecond)
	cmd := b.refreshRunSummary()
	if cmd == nil {
		t.Fatal("a moved run did not become eligible at the minute boundary")
	}
	drive(t, b, cmd())
	if other.called != 2 {
		t.Fatalf("refresh calls = %d, want two", other.called)
	}

	// A STORED SUMMARY THAT IS NOT STALE IS SHOWN AND COSTS NO CALL.
	fresh, kept, _ := summaryApp(t, false)
	fresh.taskSheet.mine.plan = c263PlanRows()
	drive(t, fresh, fresh.refreshRunSummary()())
	if kept.called != 0 || fresh.runSummaryNow != kept.summary.Now {
		t.Fatalf("a fresh stored summary: calls = %d, now = %q", kept.called, fresh.runSummaryNow)
	}
}

func TestPlanRowsDrawStoredNowUnderDotsAndRespectAbsenceAndWidth(t *testing.T) {
	const sentence = "reviewing the deterministic summary contract across a rail that has only two lines to spare and must cut the rest"
	rail := func(now string, width int, wide bool) (*app, []string) {
		a, _ := planAppWith(t, c266PlanRows(), nil)
		a.runSummaryNow = now
		a.width, a.height, a.railWide = width, 30, wide
		a.refreshElsewhere()
		return a, a.railRows(a.viewHeight())
	}
	a, got := rail(sentence, 110, false)
	pal := a.pal
	text := plain(strings.Join(got, "\n"))
	if !strings.Contains(text, "reviewing the") {
		t.Fatalf("the rail lacks the stored now sentence:\n%s", text)
	}

	// The sentence sits beneath the run's dot row, starting in the dot row's
	// own column, and every cell of it is dim. The rail's border and padding
	// are the rail's, so the columns are read off the plain text.
	column := func(row string) int {
		body := strings.TrimLeft(plain(row), "│ ")
		return ansi.StringWidth(plain(row)) - ansi.StringWidth(body)
	}
	var nowRows []string
	for i, row := range got {
		if strings.Contains(plain(row), "reviewing") {
			if i == 0 || !strings.HasSuffix(strings.TrimSpace(plain(got[i-1])), "2/4") {
				t.Fatalf("the now sentence is not under the run's dot row:\n%s", text)
			}
			if column(row) != column(got[i-1]) {
				t.Fatalf("the now sentence starts in column %d and the dot row in %d:\n%s", column(row), column(got[i-1]), text)
			}
			nowRows = append(nowRows, row, got[i+1])
			break
		}
	}
	if len(nowRows) != 2 || column(nowRows[1]) != column(nowRows[0]) {
		t.Fatalf("the now sentence does not take two lines in one column:\n%s", text)
	}
	for _, row := range nowRows {
		words := strings.TrimSpace(strings.TrimLeft(plain(row), "│ "))
		if !strings.Contains(row, pal.dim(words)) {
			t.Fatalf("now body is not wholly dim: %q", row)
		}
	}
	if !strings.Contains(plain(nowRows[1]), "…") {
		t.Fatalf("the second now line was not cut with the rail ellipsis:\n%s", text)
	}

	if _, empty := rail("", 110, false); strings.Contains(plain(strings.Join(empty, "\n")), "reviewing") {
		t.Fatal("an empty now sentence left a summary row behind")
	}
}
