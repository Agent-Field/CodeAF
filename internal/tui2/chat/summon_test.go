package chat

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
)

// The summon key and the catalog behind it.
//
// Two things are being defended here and they fail differently. The BINDING
// fails silently — a chord nobody can press looks exactly like a chord nobody
// has pressed — so it is checked at the spelling the decoder actually produces
// rather than at the string this package happens to write down. The CATALOG
// fails by omission: a store with forty jobs in it that shows twenty-four is a
// search surface that is quietly wrong about what exists, which is the one thing
// a search surface may never be.

// ledgerBackend is a boardBackend that also answers the wider job read, which
// is what makes it a [Ledger]. The nodes it returns are deliberately NOT the
// ones in the active snapshot: that is the whole point of the wider read, and a
// fake that returned the same list would test nothing.
type ledgerBackend struct {
	*boardBackend
	addressable []store.Node
}

func (l *ledgerBackend) AddressableNodes() ([]store.Node, error) {
	return l.addressable, nil
}

// summonPress drives one keystroke through the real ladder from a KEY EVENT
// rather than from a name, because the name is the thing under test. The events
// below are what internal decoders in this stack produce for the bytes a
// terminal actually sends.
func summonPress(app *App, msg tea.KeyPressMsg) tea.Msg {
	cmd := app.drain(app.key(msg))
	if cmd == nil {
		return nil
	}
	return cmd()
}

// TestTheSummonChordIsSpelledTheWayTheDecoderSpellsIt is the check that keeps
// this from being a keybinding nobody can reach.
//
// ctrl+space is the NUL byte. The decoder resolves that byte to a space key
// carrying the ctrl modifier and names the pair "ctrl+space" — so that is the
// string the ladder must switch on, and the registry row must carry the same
// one, or the surface would teach a chord it does not route.
func TestTheSummonChordIsSpelledTheWayTheDecoderSpellsIt(t *testing.T) {
	nul := tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}
	if got := nul.String(); got != summonKey {
		t.Fatalf("the decoder spells the NUL byte %q, the ladder binds %q", got, summonKey)
	}
	entry, ok := registry.ByID(paletteEntryID)
	if !ok {
		t.Fatal("the palette has no registry row, so no surface can teach its key")
	}
	if entry.KeyOn(registry.SurfaceComposerFirst) != summonKey {
		t.Fatalf("the registry teaches %q, the ladder binds %q",
			entry.KeyOn(registry.SurfaceComposerFirst), summonKey)
	}
}

// TestBothSummonChordsOpenThePalette: one door, and muscle memory is a real
// user of a keybinding — ctrl+k goes on working beside the key that replaced it.
func TestBothSummonChordsOpenThePalette(t *testing.T) {
	chords := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{summonKey, tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}},
		{summonKeyNUL, tea.KeyPressMsg{Code: '@', Mod: tea.ModCtrl}},
		{summonKeyLegacy, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}},
	}
	for _, chord := range chords {
		app, _ := boardApp(t)
		if got := chord.msg.String(); got != chord.name {
			t.Fatalf("the event for %s spells itself %q", chord.name, got)
		}
		summonPress(app, chord.msg)
		if app.overlay != overlayPalette {
			t.Errorf("%s raised overlay %d, want the palette", chord.name, app.overlay)
		}
		// And esc puts it away again, so the next chord is testing a fresh open
		// rather than an overlay that never closed.
		press(app, "esc")
		if app.overlay != overlayNone {
			t.Errorf("%s left an overlay up after esc", chord.name)
		}
	}
}

// TestTheRegistryRowRunsThePaletteDoor: the palette lists its own summon key
// (5.22 admits no typed-only action), so choosing that row has to open the
// thing it names rather than fall through runEntry's default.
func TestTheRegistryRowRunsThePaletteDoor(t *testing.T) {
	app, _ := boardApp(t)
	app.runEntry(paletteEntryID)
	if app.overlay != overlayPalette {
		t.Fatalf("the palette's own row raised overlay %d", app.overlay)
	}
	if reason := app.entryReason(paletteEntryID); reason != "" {
		t.Errorf("a door this room can open is refused with %q", reason)
	}
}

// TestTheCatalogHoldsEveryJobTheStoreHasNotJustTheRailsWindow is the reason the
// palette pays for a read of its own. The rail draws the newest twenty-four job
// roots of the active snapshot; a search surface that could only find those
// would be a filter over the rail wearing a search field.
func TestTheCatalogHoldsEveryJobTheStoreHasNotJustTheRailsWindow(t *testing.T) {
	const jobs = 40
	backend := board()
	wide := make([]store.Node, 0, jobs)
	for i := 0; i < jobs; i++ {
		node := store.Node{
			ID:         "old-" + strconv.Itoa(i),
			Title:      "archived job " + strconv.Itoa(i),
			Status:     store.Done,
			CreatedSeq: int64(100 + i),
			Summary:    "finished long ago",
		}
		if i%2 == 0 {
			node.Status = store.Running
		}
		wide = append(wide, node)
	}
	ledger := &ledgerBackend{boardBackend: backend, addressable: wide}
	app := newTestApp(ledger, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	catalog := app.catalog("")
	if len(catalog.Jobs) != jobs {
		t.Fatalf("the catalog holds %d jobs, the store has %d", len(catalog.Jobs), jobs)
	}
	// Newest first, which is the reverse of admission order and is a walk
	// rather than a sort (catalogJobs).
	if catalog.Jobs[0].Title != "archived job "+strconv.Itoa(jobs-1) {
		t.Errorf("the newest job is not first: %q", catalog.Jobs[0].Title)
	}
	// Every one of them jumps through the rail's own id grammar, so choosing a
	// job is choosing its room and not a second door onto the same place.
	for _, job := range catalog.Jobs {
		if !strings.HasPrefix(job.ID, rowTaskPrefix) {
			t.Fatalf("job %q carries id %q, which the rail cannot resolve", job.Title, job.ID)
		}
	}
}

// TestJobRowsCarryTheirAgeAndCost: the two cells that decide which of forty
// jobs the reader meant, formatted once when the door opens because a render
// that reads a clock is not a pure function of its state.
func TestJobRowsCarryTheirAgeAndCost(t *testing.T) {
	backend := board()
	backend.usage = map[string]store.JobUsage{"old-1": {Runs: 1, Cost: 2.50}}
	started := fixedNow().Add(-90 * 1e9) // 90 seconds ago, in nanoseconds.
	ledger := &ledgerBackend{boardBackend: backend, addressable: []store.Node{
		{ID: "old-1", Title: "index the corpus", Status: store.Running, CreatedSeq: 100,
			Brief: "walk the tree", Summary: "wrote index.json", StartedAt: started},
		{ID: "old-2", Title: "never started", Status: store.Pending, CreatedSeq: 101},
	}}
	app := newTestApp(ledger, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	byTitle := map[string]palette.Job{}
	for _, job := range app.catalog("").Jobs {
		byTitle[job.Title] = job
	}
	running, ok := byTitle["index the corpus"]
	if !ok {
		t.Fatalf("the running job never reached the catalog: %v", byTitle)
	}
	if running.Age == "" {
		t.Error("a job that has been running for 90s reports no age")
	}
	if running.Cost != "$2.50" {
		t.Errorf("cost = %q, want $2.50", running.Cost)
	}
	if running.Receipt != "wrote index.json" {
		t.Errorf("receipt = %q", running.Receipt)
	}
	// A queued job has not started, and a zero is a reading rather than an
	// absence — so the cell stays empty instead of claiming "0s".
	queued, ok := byTitle["never started"]
	if !ok {
		t.Fatal("the queued job never reached the catalog")
	}
	if queued.Age != "" {
		t.Errorf("a job that never started reports an age of %q", queued.Age)
	}
	if queued.Cost != "" {
		t.Errorf("a job that spent nothing reports a cost of %q", queued.Cost)
	}
}

// TestABackendWithNoWiderReadStillOpens: the ledger is an optional interface,
// and a window driven by a backend that is only a message log must still raise
// the palette — with an honest empty group, not a broken one.
func TestABackendWithNoWiderReadStillOpens(t *testing.T) {
	app, _ := boardApp(t)
	if _, ok := app.backend.(Ledger); ok {
		t.Skip("the board backend grew a wider read; this test needs one without")
	}
	if jobs := app.catalog("").Jobs; jobs != nil {
		t.Errorf("a backend with no wider read produced %d jobs", len(jobs))
	}
	press(app, "ctrl+k")
	if app.overlay != overlayPalette {
		t.Fatalf("the palette refused to open without a ledger: overlay %d", app.overlay)
	}
}
