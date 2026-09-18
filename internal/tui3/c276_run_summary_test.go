package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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

	first := a.refreshRunSummary()
	if first == nil {
		t.Fatal("a stale summary returned no off-frame command")
	}
	if fake.called != 0 {
		t.Fatal("refresh ran while the command was being made")
	}
	if second := a.refreshRunSummary(); second != nil {
		t.Fatal("a refresh already in flight admitted a second command")
	}
	close(fake.gate)
	msg := first()
	drive(t, a, msg)
	if fake.called != 1 {
		t.Fatalf("refresh calls = %d, want one", fake.called)
	}

	*at = (*at).Add(runSummaryRefreshEvery - time.Nanosecond)
	if cmd := a.refreshRunSummary(); cmd != nil {
		t.Fatal("summary refreshed inside its named once-a-minute throttle")
	}
	*at = (*at).Add(time.Nanosecond)
	if cmd := a.refreshRunSummary(); cmd == nil {
		t.Fatal("stale summary did not become eligible at the minute boundary")
	}

	fresh, _, _ := summaryApp(t, false)
	if cmd := fresh.refreshRunSummary(); cmd != nil {
		t.Fatal("a stored summary whose shape has not moved paid for a refresh")
	}
}

func TestPlanRowsDrawStoredNowUnderDotsAndRespectAbsenceAndWidth(t *testing.T) {
	rows := c263PlanRows()
	mine := tasksMine{plan: rows, now: "reviewing the deterministic summary contract across a rail that has only two lines to spare and must cut the rest"}
	reading := readTasks(session.World{}, mine, session.UsageWindow{}, tasksSort{}, time.Time{}, taskFixtureNow)

	width := planRailDotsUnder - 1
	pal := newPalette(tokens.ANSI256, false)
	got := reading.planRows(width, pal)
	text := plain(strings.Join(got, "\n"))
	if !strings.Contains(text, "reviewing the deterministic") {
		t.Fatalf("plan rows lack the stored now sentence:\n%s", text)
	}

	// The summary belongs beneath the root's dot row, wearing that row's exact
	// ANSI-preserving pad kin rather than a fresh tree connector.
	var nowRows []string
	var dotLead string
	for i, row := range got {
		if strings.Contains(plain(row), "reviewing") {
			if i == 0 {
				t.Fatal("now row has no dot row above it")
			}
			dots := got[i-1]
			dotBody := strings.TrimSpace(plain(dots))
			dotLead = strings.TrimSuffix(dots, pal.dim(dotBody))
			nowRows = append(nowRows, row)
		} else if len(nowRows) == 1 {
			nowRows = append(nowRows, row)
		}
	}
	if len(nowRows) != 2 {
		t.Fatalf("now rows = %d, want exactly two\n%s", len(nowRows), text)
	}
	for _, row := range nowRows {
		if dotLead == "" || !strings.HasPrefix(row, dotLead) {
			t.Fatalf("now row does not sit under the dot row's kin: %q, want prefix %q", row, dotLead)
		}
		body := strings.TrimPrefix(row, dotLead)
		if body == plain(body) || body != pal.dim(plain(body)) {
			t.Fatalf("now body is not wholly dim: %q", row)
		}
	}
	if !strings.Contains(plain(nowRows[1]), "…") {
		t.Fatalf("the second now line was not cut with the rail ellipsis:\n%s", text)
	}

	mine.now = ""
	empty := readTasks(session.World{}, mine, session.UsageWindow{}, tasksSort{}, time.Time{}, taskFixtureNow)
	if strings.Contains(plain(strings.Join(empty.planRows(planRailDotsUnder-1, palette{}), "\n")), "reviewing") {
		t.Fatal("an empty now sentence left a summary row behind")
	}
	mine.now = "must not fit"
	narrow := readTasks(session.World{}, mine, session.UsageWindow{}, tasksSort{}, time.Time{}, taskFixtureNow)
	if got := plain(strings.Join(narrow.planRows(1, palette{}), "\n")); strings.Contains(got, "must not fit") {
		t.Fatalf("a rail too narrow for the dot under-line drew now: %q", got)
	}
}
