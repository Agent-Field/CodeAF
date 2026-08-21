package tui3

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

type homeNewsCache struct {
	at    time.Time
	notes []standing.Note
}

func init() {
	// project inbox — the band draws for a PROJECT card too, because a firing
	// whose origin was an `ask here` exchange has no conversation to wait in
	// and lands in the project's own inbox instead (internal/session's
	// standing_run.go).
	registerHomeBand(homeBand{
		name:  "news",
		order: bandOrderNews,
		kinds: []bandKind{bandKindSession, bandKindProject},
		draw:  drawNewsBand,
	})
}

func drawNewsBand(a *app, ctx bandContext) []string {
	key := ctx.subject.id()
	if a.home.news == nil {
		a.home.news = map[string]homeNewsCache{}
	}
	cached, ok := a.home.news[key]
	if !ok || ctx.now.Sub(cached.at) >= homeEvery {
		cached = homeNewsCache{at: ctx.now, notes: newsNotes(a, ctx)}
		a.home.news[key] = cached
	}
	if len(cached.notes) == 0 {
		return nil
	}
	groups := make([][]string, 0, len(cached.notes))
	for _, note := range cached.notes {
		lead := joinDot(sinceAt(note.At, ctx.now), strings.TrimSpace(note.Words))
		groups = append(groups, bandClauses(ctx.width, 2, ctx.pal.dim,
			lead, strings.TrimSpace(note.Text)))
	}
	rows := a.bandFoldPacked(ctx, "news", groups, 3, "things")
	// ONE THING IS NOT ONE THINGS. The count is a real number a person reads,
	// and every other counted row on this surface is spelled through [plural]
	// (homeband_projectfacts.go's `1 conversation`, the task column's
	// `1 task`); a heading that said `1 things` would be the one place the
	// screen forgot how to count.
	heading := "◆ " + itoa(len(cached.notes)) + plural(" thing", len(cached.notes)) + " since you left"
	return append([]string{ctx.pal.accent(fit(heading, ctx.width))}, rows...)
}

// newsNotes is which inbox this subject's news comes from.
//
// A CONVERSATION HAS ITS OWN AND A PROJECT HAS THE PROJECT'S. Both are read and
// NEITHER is emptied here — draining is what an opening conversation does
// ([session.Agent.drainStandingInbox]), and a screen that consumed the news
// while drawing it would take the fold away from the person it was for.
func newsNotes(a *app, ctx bandContext) []standing.Note {
	// project inbox
	if ctx.subject.kind == bandKindProject {
		project, ok := bandProjectOf(ctx.subject)
		if !ok || strings.TrimSpace(project.Path) == "" {
			return nil
		}
		return standing.PeekProjectInbox(a.standingHome(), project.Path)
	}
	return readHomeNews(filepath.Dir(ctx.subject.row.Transcript))
}

func readHomeNews(dir string) []standing.Note {
	file, err := os.Open(standing.InboxPath(dir))
	if err != nil {
		return nil
	}
	defer file.Close()
	var notes []standing.Note
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var note standing.Note
		if json.Unmarshal(scanner.Bytes(), &note) == nil {
			notes = append(notes, note)
		}
	}
	return notes
}
