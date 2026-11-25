package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A CREW'S MANAGER LINKS WHAT THE CREW MADE (team_link) ───────────────────
//
// A crew's linked items live in the app's crews layer (appcrewlayer.go), and
// used to be written only by the desk over `crews.link`. The session's
// team_link verb (internal/session's tools_team_link.go) is a door each
// process sets:
//
//   - IN THE APP it is [appBackend.CrewLink], which checks the crew, records
//     the item and tells the window (crew.item), exactly as a person's link.
//   - IN AN ENGINE PROCESS (a terminal's host, the pipe engine) it records the
//     item in the same layer file, under the layer's file lock, and the app
//     reads it the next time it is asked for the crew's items.

// armEngineTeamLink gives an engine process the door.
func armEngineTeamLink() {
	layer := &appCrewLayer{path: appCrewLayerPath()}
	session.SetTeamLinker(func(link session.TeamLink) (string, error) {
		item, err := linkCrewItem(layer, link, time.Now())
		return item.ID, err
	})
}

// armAppTeamLink gives the app's process the door, and returns the disarm.
func (b *appBackend) armAppTeamLink() func() {
	session.SetTeamLinker(func(link session.TeamLink) (string, error) {
		item, err := b.CrewLink(link.Team, link.Kind, link.Ref, link.Title)
		return item.ID, err
	})
	return func() { session.SetTeamLinker(nil) }
}

// appCrewLayerPath is the crews layer's file as any process finds it: beside
// the app's state file under codeaf's home.
func appCrewLayerPath() string {
	return filepath.Join(filepath.Dir(appStatePath()), appCrewLayerName)
}

// linkCrewItem records one item in a layer. The same ref linked again is the
// item already there, so a manager that links its final draft twice lists it
// once.
func linkCrewItem(layer *appCrewLayer, link session.TeamLink, now time.Time) (appwire.CrewItem, error) {
	var item appwire.CrewItem
	err := layer.write(func(f *appCrewLayerFile) error {
		extra := f.crew(link.Team)
		ref := strings.TrimSpace(link.Ref)
		for _, have := range extra.Items {
			if have.Kind == link.Kind && have.Ref == ref {
				item = have
				return nil
			}
		}
		item = appwire.CrewItem{ID: f.next("i"), Crew: link.Team, Kind: link.Kind, Ref: ref, Title: strings.TrimSpace(link.Title), At: now}
		extra.Items = append(extra.Items, item)
		return nil
	})
	return item, err
}
