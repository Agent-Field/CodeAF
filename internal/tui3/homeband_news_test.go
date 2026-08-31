package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ONE THING IS NOT ONE THINGS. The heading counts real things a person reads,
// and a single one is the commonest case there is — one reminder fired while
// the window was shut.
func TestOneThingSinceYouLeftIsSpelledSingular(t *testing.T) {
	dir := t.TempDir()
	row := session.SessionRow{ID: "0123456789abcdef", Dir: dir, Transcript: filepath.Join(dir, session.TranscriptName)}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	if err := standing.Deliver(dir, standing.Note{
		At: now.Add(-time.Minute), Words: "remind me in 1 minute to stretch", Text: "time to stretch",
	}); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(nil)
	ctx := ambientBandContext(a, row, now, 40)
	takeHomeNews(a, ctx.subject, now)
	got := plain(strings.Join(drawNewsBand(a, ctx), "\n"))
	if !strings.Contains(got, "◆ 1 thing since you left") {
		t.Fatalf("the heading did not count to one:\n%s", got)
	}
	if strings.Contains(got, "1 things") {
		t.Fatalf("the heading says `1 things`:\n%s", got)
	}
}
