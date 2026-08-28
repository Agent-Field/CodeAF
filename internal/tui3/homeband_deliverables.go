package tui3

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

type homeDeliverablesCache struct {
	at   time.Time
	rows []session.Artifact
}

func init() {
	registerHomeBand(homeBand{name: "deliverables", order: bandOrderDeliverable, draw: drawDeliverablesBand})
}

func drawDeliverablesBand(a *app, ctx bandContext) []string {
	key := ctx.subject.id()
	if a.home.deliverables == nil {
		a.home.deliverables = map[string]homeDeliverablesCache{}
	}
	cached, ok := a.home.deliverables[key]
	if !ok || ctx.now.Sub(cached.at) >= homeEvery {
		sessionID := ctx.subject.row.ID
		var rows []session.Artifact
		artifacts := session.ReadArtifacts(a.artifactsIndex())
		if a.hosted() {
			// The index belongs to the machine that made the files. A hosted
			// surface receives those rows with the cached world; reading the path
			// above would read this machine's index and join unrelated rows to far
			// conversation ids.
			artifacts = a.home.world.Artifacts
		}
		for _, artifact := range artifacts {
			if artifact.Session == sessionID {
				rows = append(rows, artifact)
			}
		}
		cached = homeDeliverablesCache{at: ctx.now, rows: rows}
		a.home.deliverables[key] = cached
	}
	rows := cached.rows
	if len(rows) == 0 {
		return nil
	}
	drawn := make([][]string, 0, len(rows))
	for _, artifact := range rows {
		name := filepath.Base(strings.TrimSpace(artifact.Path))
		labelInk := func(text string) string { return ctx.pal.muted(a.pathLink(artifact.Path, text)) }
		drawn = append(drawn, bandSidesWithSeparator(ctx.width, 2, 8, "· ", name,
			sinceAt(artifact.Created, ctx.now), labelInk, ctx.pal.dim))
	}
	return a.bandFoldPacked(ctx, "deliverables", drawn, 3, "files")
}
