package tui3

// ── ONE GRAIN OF CLOCK ──────────────────────────────────────────────────────
//
// A task's row said `42m` and the page it opened said `ran 6m 0s`, so the same
// clock changed grain across one keypress — and four of those cells were spent
// on a zero the emptiness law would strike anywhere else on the surface
// (docs/design/polish/audit-tasks.md row 17). [reltime.Elapsed] already states
// the rule for the settled figure; this is the live one saying it too.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestACountUpDropsARungWhoseRemainderIsZero(t *testing.T) {
	for _, c := range []struct {
		took time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{time.Minute, "1m"},
		{6 * time.Minute, "6m"},
		{4*time.Minute + 30*time.Second, "4m 30s"},
		{65 * time.Second, "1m 5s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 5*time.Minute, "2h 5m"},
	} {
		if got := countUpWord(c.took); got != c.want {
			t.Fatalf("a duration of %v is spelled %q, and a rung whose remainder is zero is dropped rather than padded: %q",
				c.took, got, c.want)
		}
	}
}

// AND THE PAGE A PERSON OPENS SAYS IT. A task that ran six minutes exactly draws
// `ran 6m` on its record card, beside a list row that has already said `42m`.
func TestATaskThatRanAWholeNumberOfMinutesSaysSoOnItsCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 40
	a.raisePlace(pageTasks)
	entry := session.TaskIndexEntry{
		ID: "1", Title: "Port the picker onto the new list",
		Label:  "Port the picker onto the new list",
		Status: string(session.TaskDone), DurationMS: 360000,
	}
	a.taskSheet = tasksPlace{detailOn: true, detail: entry}
	line := plain(a.taskCardWhenLine(entry))
	if !strings.Contains(line, "ran 6m") {
		t.Fatalf("the card's clock line is %q and a six-minute run is `ran 6m`", line)
	}
	if strings.Contains(line, "6m 0s") {
		t.Fatalf("the card's clock line is %q and it spends four cells on a zero", line)
	}
}
