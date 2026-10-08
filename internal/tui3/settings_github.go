package tui3

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── GITHUB ON THE CONNECTIONS TAB ───────────────────────────────────────────
//
// How this machine reaches GitHub for the factory floor is a standing answer,
// so it has one row where the other accounts are: the Connections tab.
//
//	✓ github                          santoshkumarradha · via gh
//
// It is the tab's own row grammar (connectcaps.go's [connRow]), the name at
// the left and the one fact at the right: who the token belongs to and the way
// it was found, `not connected` when none resolves. THE FACT IS READ OFF THE
// LOOP ONCE, when the place opens ([app.readGitHubLink]); until it lands the
// row says nothing at its right rather than a guess. `enter` runs the floor's
// own connect prompt (factory_settings.go), on the floor, because what a
// person does next with a connected GitHub is choose what the floor watches.
//
// A SEAM WITH NO GITHUB DOOR HAS NO ROW: a capability that cannot work is
// absent, not broken.

// githubService is the row's id on the tab. It is not a catalog id and never
// reaches the engine's account doors.
const githubService = "factory:github"

// githubName is the row's name.
const githubName = "github"

// githubWait is how long the row's one read may take.
const githubWait = 10 * time.Second

// githubReading is what the row's read said.
type githubReading struct {
	link factory.GitHubLink
	err  error
}

// readGitHubLink asks the seam who this machine reaches GitHub as, beside the
// door line, and rebuilds the tab when it lands. It answers nil with no door.
func (a *app) readGitHubLink() tea.Cmd {
	door := a.factory.GitHub
	if door == nil {
		return nil
	}
	return a.besideLine(func() func(bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), githubWait)
		defer cancel()
		link, err := door(ctx)
		return func(bool) tea.Cmd {
			a.sheet.github = &githubReading{link: link, err: err}
			if a.at(pageSettings) && a.sheet.onConnections() && a.sheet.edit == nil && a.sheet.sel == nil && a.sheet.choice == nil {
				a.sheet.build()
			}
			a.touch()
			return nil
		}
	})
}

// githubNote is the row's fact: the login and the way, `not connected`, or
// nothing before the read lands.
func githubNote(r *githubReading) string {
	switch {
	case r == nil:
		return ""
	case r.err != nil:
		return "not reachable"
	case !r.link.Connected():
		return "not connected"
	}
	way := ""
	switch r.link.Via {
	case factory.ViaGH:
		way = "via gh"
	case factory.ViaToken:
		way = "token"
	case factory.ViaEnv:
		way = "from the environment"
	}
	return strings.Join(nonEmpty([]string{r.link.Login, way}), rowSep)
}

// appendGitHub is the row, first on the tab's own rows, when the seam has the
// door and the filter (if any) reaches the word.
func (s *sheet) appendGitHub(query string) {
	if !s.githubDoor {
		return
	}
	if q := strings.ToLower(strings.TrimSpace(query)); q != "" && !strings.Contains(githubName, q) {
		return
	}
	connected := s.github != nil && s.github.err == nil && s.github.link.Connected()
	row := &connRow{kind: connService, service: githubService, name: githubName, connected: connected}
	if connected {
		row.account = githubNote(s.github)
	} else {
		row.blurb = githubNote(s.github)
	}
	s.items = append(s.items, sheetItem{conn: row})
}

// githubAct is `enter` on the row: the floor opens with its connect prompt.
func (a *app) githubAct() tea.Cmd {
	if !a.factory.Has("connectgithub") {
		return nil
	}
	return tea.Batch(a.showPage(pageFactory), a.factoryConnectAsk())
}
