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
		for _, artifact := range session.ReadArtifacts(a.artifactsIndex()) {
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
	drawn := make([]string, 0, len(rows))
	for _, artifact := range rows {
		name := filepath.Base(strings.TrimSpace(artifact.Path))
		shown := a.pathLink(artifact.Path, name)
		if age := sinceAt(artifact.Created, ctx.now); age != "" {
			shown += ctx.pal.dim(" · " + age)
		}
		drawn = append(drawn, ctx.pal.muted(fit(shown, ctx.width)))
	}
	return a.bandFold(ctx, "deliverables", drawn, 3, "files")
}
