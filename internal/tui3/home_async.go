package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Home keeps the last completed reading across visits. A keystroke paints that
// reading; scanning journals, SQLite and the ledger belongs to a command.
// Only immutable readings cross the goroutine boundary, never the live app.
type homeReading struct {
	world     session.World
	known     bool
	spend     homeSpendReading
	ledger    switcherLedgerInput
	artifacts homeArtifactIndex
	made      homeMadeIndex
	gone      map[string]bool
}

func (a *app) rememberHomeReading() {
	a.homeReading = homeReading{world: a.home.world, known: a.home.known,
		spend: a.home.spend, ledger: a.home.ledger, artifacts: a.home.artifacts,
		made: a.home.made, gone: a.home.gone}
}

func (a *app) useHomeReading(r homeReading) {
	a.home.world, a.home.known = r.world, r.known
	a.home.spend, a.home.ledger = r.spend, r.ledger
	a.home.artifacts, a.home.made, a.home.gone = r.artifacts, r.made, r.gone
}

// furnishHomeCached builds only from readings already held by the window.
func (a *app) furnishHomeCached() {
	a.home.phone = a.homePhone()
	a.home.autos, a.home.active = a.watch.list, a.watch.active
	a.readPlaceSummaries()
	if len(a.home.spend.days) > 0 {
		a.machine.spent, a.machine.ceiling = a.home.spend.today, a.home.spend.ceiling
	}
	a.machine.hands, a.machine.wants = machineCounts(a.now(), a.home.world.Sessions(), a.exchanges)
	a.home.build()
}

func (a *app) refreshHomeLater() tea.Cmd {
	if !a.at(pageHome) || a.homeReadingPending {
		return nil
	}
	a.homeReadingPending = true
	gen := a.homeGen
	// These are the reader's complete inputs. Its helper methods may read files,
	// but cannot reach a live composer, cursor, subscription or mutable app map.
	now := a.now()
	reader := &app{host: a.host, homeRoot: a.placesRoot(), farPlaces: a.worldRoot(),
		world: a.world, file: a.file, title: a.title, workspace: a.workspace, model: a.model,
		profileDir: a.profileDir, memory: a.memory, ledger: a.ledger, usageLedger: a.usageLedger,
		artifacts: a.artifactsIndex(), clock: func() time.Time { return now },
		home: homeView{seen: a.home.seen, far: a.hosted(), artifacts: a.home.artifacts, made: a.home.made}}
	return a.besideLine(func() func(bool) tea.Cmd {
		reader.home.world, reader.home.known = reader.readWorldKnown()
		reader.readSwitchLedger()
		reader.readHomeSpend()
		reader.readHomeArtifacts()
		reader.home.readGone()
		reader.rememberHomeReading()
		reading := reader.homeReading
		return func(bool) tea.Cmd {
			a.homeReadingPending = false
			if !a.at(pageHome) {
				return nil
			}
			if gen != a.homeGen {
				return a.refreshHomeLater()
			}
			a.useHomeReading(reading)
			a.rememberHomeReading()
			a.furnishHomeCached()
			a.pointMovedRow()
			a.touch()
			asked := tea.Batch(a.refreshHomeCard(a.now()), a.refreshGridReadings(a.now()), a.askEngines())
			if a.homeAnimating() {
				return tea.Batch(asked, a.wake())
			}
			return asked
		}
	})
}
