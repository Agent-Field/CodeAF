package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

func ambientBandContext(a *app, width int, now time.Time) bandContext {
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
	a.refreshHomeRepo(now)
	rows := drawRepoBand(a, ambientBandContext(a, 34, now))
	if got := plain(rows[0]); !strings.Contains(got, "feature/home · 2 files dirty") || ansi.StringWidth(got) > 34 {
		t.Fatalf("repo row = %q", got)
	}
	a.refreshHomeRepo(now.Add(time.Second))
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
	a.refreshHomeRepo(time.Now())
	if rows := drawRepoBand(a, ambientBandContext(a, 40, time.Now())); len(rows) != 0 {
		t.Fatalf("failed git drew %q", rows)
	}
}

func TestKeysBandDrawsBothLegendsAndObeysWidth(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	ctx := ambientBandContext(a, 28, time.Now())
	rows := drawKeysBand(a, ctx)
	if got := plain(rows[0]); !strings.HasPrefix(got, "enter open · n new") || ansi.StringWidth(got) > 28 {
		t.Fatalf("session keys = %q", got)
	}
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
	ctx := ambientBandContext(a, 32, now)
	rows := drawNextUpBand(a, ctx)
	got := strings.Join([]string{plain(rows[0]), plain(rows[1]), plain(rows[2])}, "\n")
	if !strings.Contains(got, "◦ first · in 4m") || !strings.Contains(got, "…1 more items") {
		t.Fatalf("next up = %q", got)
	}
	if ansi.StringWidth(plain(rows[0])) > 32 {
		t.Fatalf("next up exceeded width: %q", plain(rows[0]))
	}
	a.home.items = nil
	if rows := drawNextUpBand(a, ctx); len(rows) != 0 {
		t.Fatalf("empty next up drew %q", rows)
	}
}

func TestSpendBandPreservesFactsAndEmptiness(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Now()
	ctx := ambientBandContext(a, 30, now)
	ctx.subject.row.Tasks = session.TaskRollup{Spend: 1.25, Tokens: 34000}
	if got := plain(drawSpendBand(a, ctx)[0]); !strings.Contains(got, "spent $1.25 · 34k tokens") || ansi.StringWidth(got) > 30 {
		t.Fatalf("spend = %q", got)
	}
	ctx.subject.row = session.SessionRow{}
	if rows := drawSpendBand(a, ctx); len(rows) != 0 {
		t.Fatalf("empty spend drew %q", rows)
	}
}
