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
	registerHomeBand(homeBand{name: "news", order: bandOrderNews, draw: drawNewsBand})
}

func drawNewsBand(a *app, ctx bandContext) []string {
	key := ctx.subject.id()
	if a.home.news == nil {
		a.home.news = map[string]homeNewsCache{}
	}
	cached, ok := a.home.news[key]
	if !ok || ctx.now.Sub(cached.at) >= homeEvery {
		cached = homeNewsCache{at: ctx.now, notes: readHomeNews(filepath.Dir(ctx.subject.row.Transcript))}
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
	heading := "◆ " + itoa(len(cached.notes)) + " things since you left"
	return append([]string{ctx.pal.accent(fit(heading, ctx.width))}, rows...)
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
