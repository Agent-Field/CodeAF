package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// settleHomeRepo runs the reading an arrival ASKED FOR and files the answer,
// which is what the update loop does when the message comes back
// (homeband_repo.go's [app.refreshRepoOf] is a tea.Cmd because a keystroke may
// not wait for git).
func settleHomeRepo(a *app, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(homeRepoMsg); ok {
		a.tookHomeRepo(msg)
	}
}

func ambientBandContextAt(a *app, width int, now time.Time) bandContext {
	return bandContext{subject: bandSubject{kind: bandKindSession,
		row: session.SessionRow{Workspace: "/work", ProjectDir: "/bucket"}, dir: "/bucket"},
		width: width, now: now, pal: a.pal}
}

func TestRepoBandDrawsKnownFactsCachesAndObeysWidth(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Now()
	calls := 0
	old := homeGitStatus
	homeGitStatus = func(context.Context, string) ([]byte, error) {
		calls++
		return []byte("# branch.head feature/home\n# branch.ab +2 -1\n1 .M N... file.go\n? new.go\n"), nil
	}
	t.Cleanup(func() { homeGitStatus = old })
	a.home.lines = []homeLine{{kind: homeSession, row: session.SessionRow{Workspace: "/work"}}}
	settleHomeRepo(a, a.refreshRepoOf("/work", now))
	rows := drawRepoBand(a, ambientBandContextAt(a, 34, now))
	if got := plain(rows[0]); !strings.Contains(got, "feature/home · 2 files dirty") || ansi.StringWidth(got) > 34 {
		t.Fatalf("repo row = %q", got)
	}
	assertNarrowRows(t, "repo", drawRepoBand(a, ambientBandContextAt(a, 30, now)), 30, "behind 1")
	settleHomeRepo(a, a.refreshRepoOf("/work", now.Add(time.Second)))
	if calls != 1 {
		t.Fatalf("git status ran %d times inside its cache", calls)
	}
}

func TestRepoBandDrawsNothingWhenGitCannotAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	old := homeGitStatus
	homeGitStatus = func(context.Context, string) ([]byte, error) { return nil, errors.New("not git") }
	t.Cleanup(func() { homeGitStatus = old })
	a.home.lines = []homeLine{{kind: homeSession, row: session.SessionRow{Workspace: "/work"}}}
	settleHomeRepo(a, a.refreshRepoOf("/work", time.Now()))
	if rows := drawRepoBand(a, ambientBandContextAt(a, 40, time.Now())); len(rows) != 0 {
		t.Fatalf("failed git drew %q", rows)
	}
}

func TestKeysBandDrawsBothLegendsAndObeysWidth(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	ctx := ambientBandContextAt(a, 28, time.Now())
	rows := drawKeysBand(a, ctx)
	if got := plain(rows[0]); !strings.HasPrefix(got, "enter open") || ansi.StringWidth(got) > 28 {
		t.Fatalf("session keys = %q", got)
	}
	assertNarrowRows(t, "keys", drawKeysBand(a, ambientBandContextAt(a, 30, time.Now())), 30, "→ more")
	ctx.subject.kind = bandKindItem
	if got := plain(drawKeysBand(a, ctx)[0]); !strings.HasPrefix(got, "enter open where") {
		t.Fatalf("item keys = %q", got)
	}
	ctx.subject.kind = bandKindProject
	if rows := drawKeysBand(a, ctx); len(rows) != 0 {
		t.Fatalf("project keys drew %q", rows)
	}
}

func TestNextUpDrawsSoonestTwoFoldsAndDrawsNothingEmpty(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Now()
	item := func(id, words string, due time.Duration) StandingItemView {
		return StandingItemView{Item: standing.Item{ID: id, Words: words, Status: standing.StatusActive,
			NextDue: now.Add(due), When: standing.When{Words: "Mondays 9am"}}}
	}
	a.home.items = map[string][]StandingItemView{"/bucket": {
		item("late", "third", 12*time.Minute), item("first", "first", 4*time.Minute), item("next", "second", 8*time.Minute),
	}}
	ctx := ambientBandContextAt(a, 32, now)
	rows := drawNextUpBand(a, ctx)
	got := strings.Join([]string{plain(rows[0]), plain(rows[1]), plain(rows[2])}, "\n")
	if !strings.Contains(got, "◦ first") || !strings.Contains(got, "· in 4m") || !strings.Contains(got, "…1 more items") {
		t.Fatalf("next up = %q", got)
	}
	if ansi.StringWidth(plain(rows[0])) > 32 {
		t.Fatalf("next up exceeded width: %q", plain(rows[0]))
	}
	assertNarrowRows(t, "next up", drawNextUpBand(a, ambientBandContextAt(a, 30, now)), 30, "in 4m")
	a.home.items = nil
	if rows := drawNextUpBand(a, ctx); len(rows) != 0 {
		t.Fatalf("empty next up drew %q", rows)
	}
}

func TestSpendBandPreservesFactsAndEmptiness(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Now()
	ctx := ambientBandContextAt(a, 30, now)
	ctx.subject.row.Tasks = session.TaskRollup{Spend: 1.25, Tokens: 34000}
	if got := plain(drawSpendBand(a, ctx)[0]); !strings.Contains(got, "spent $1.25 · 34k tokens") || ansi.StringWidth(got) > 30 {
		t.Fatalf("spend = %q", got)
	}
	assertNarrowRows(t, "spend", drawSpendBand(a, ctx), 30, "34k tokens")
	ctx.subject.row = session.SessionRow{}
	if rows := drawSpendBand(a, ctx); len(rows) != 0 {
		t.Fatalf("empty spend drew %q", rows)
	}
}
