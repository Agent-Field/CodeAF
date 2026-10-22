package config

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/selector"
)

// ── the pareto selector, wired ──────────────────────────────────────────────
//
// internal/selector's engine (the ported pareto selection change-set) picks a
// model per seat from a FITTED SharedSnapshot — posterior quality means per
// role and model, with a UCB-enter/LCB-rank gate — over the same candidate
// list the live router reads. This file is the only place the engine is
// built, and the only place its answer reaches a real decision:
//
//   - No snapshot configured: the selector never runs, and the crew is the
//     live router's exactly as before — byte for byte the pre-wiring
//     behavior, which is the safety property of the whole feature.
//   - A snapshot configured but unloadable, or a seat the engine cannot seat
//     (no candidate passes its capability gates): that seat keeps the live
//     router's pick. The engine is a per-seat overlay, never a second router.
//
// Either way the decision, its estimates, its ladder, its rescue, its log row
// and its receipt are the live engine's own ([Request.Chosen] in
// internal/crewroute): the selector replaces the MODEL a seat runs, never the
// machinery around it. A pin still wins, a redo still routes by the live
// engine alone, and a learned offset still moves the crew on top of the
// selector's pick.

// KeyCrewSelectorSnapshot is the path to a fitted SharedSnapshot JSON the
// selector reads. PROFILE-ONLY, like every crew row: a path is a choice about
// what may route a person's work, and a repository that could write one could
// route a visitor's credit to a model nobody chose.
const KeyCrewSelectorSnapshot = "models.crew.selector_snapshot"

// CrewSelectorSnapshotAt is the configured snapshot path, empty for none —
// and none is the default: with no snapshot the live router picks alone.
func CrewSelectorSnapshotAt(profileDir string) string {
	value, _ := persistedString(profileDir, KeyCrewSelectorSnapshot)
	return strings.TrimSpace(value)
}

// SetCrewSelectorSnapshot writes the snapshot path, or clears it with an
// empty one. A path that does not load is refused HERE rather than ignored at
// route time: a person who configured a snapshot is owed a refusal they can
// read, not a crew that silently kept the old picker.
func SetCrewSelectorSnapshot(profileDir, raw string) error {
	path := strings.TrimSpace(raw)
	if path != "" {
		if _, err := selector.LoadSnapshot(path); err != nil {
			return fmt.Errorf("that snapshot does not load: %w", err)
		}
	}
	return writeProfileValue(profileDir, KeyCrewSelectorSnapshot, path)
}

// CrewSelectorPicks builds the seat overlay the router is handed: the
// selector's pick per crew seat, or nil when no snapshot is configured, one
// does not load, or the engine seats nothing. A variable so a test can stub
// the engine the way [CrewCatalog] and [CrewHistory] are stubbed.
var CrewSelectorPicks = selectorCrewPicks

// snapshotCache is the one snapshot in effect, cached by path + mtime +
// size: routing calls selectorCrewPicks on every crewed request, and a
// fitted file is read once per change, not once per route. The cached
// snapshot is only ever read — the engine keeps its own per-call state.
var snapshotCache struct {
	mu    sync.Mutex
	path  string
	mtime time.Time
	size  int64
	snap  *selector.SharedSnapshot
}

// cachedSnapshot loads path, or serves the cached copy when the file has
// not changed. A nil return is the fallback: unreadable, unparseable or
// invalid all mean unconfigured (see selectorCrewPicks).
func cachedSnapshot(path string) *selector.SharedSnapshot {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	snapshotCache.mu.Lock()
	defer snapshotCache.mu.Unlock()
	if snapshotCache.snap != nil && snapshotCache.path == path &&
		snapshotCache.mtime.Equal(fi.ModTime()) && snapshotCache.size == fi.Size() {
		return snapshotCache.snap
	}
	snap, err := selector.LoadSnapshot(path)
	if err != nil {
		return nil
	}
	snapshotCache.path, snapshotCache.mtime = path, fi.ModTime()
	snapshotCache.size, snapshotCache.snap = fi.Size(), snap
	return snap
}

// selectorCrewPicks runs the engine over this request's candidates and the
// configured snapshot. The classes are the engine's seat shapes: the worker
// class seats the worker, the high class the checker, the mastermind class
// the planner — the three roles a crew has. A class the engine cannot seat
// leaves that seat out of the overlay, which is the live pick standing.
func selectorCrewPicks(profileDir string, req crewroute.Request) map[crewroute.Seat]string {
	path := CrewSelectorSnapshotAt(profileDir)
	if path == "" || len(req.Candidates) == 0 {
		return nil
	}
	snap := cachedSnapshot(path)
	if snap == nil {
		// UNLOADABLE IS UNCONFIGURED, on purpose: the fallback to the live
		// router is the safety property, and a route that fails because a
		// fitted file went stale is a route that fails for nothing. The
		// refusal to WRITE a bad path is where the person is told.
		return nil
	}
	cat := selectorCatalogOf(req)
	if cat == nil {
		return nil
	}
	eng := selector.NewEngine(snap, cat, nil)
	picks := map[crewroute.Seat]string{}
	for _, slot := range []struct {
		seat  crewroute.Seat
		class string
	}{
		{crewroute.Worker, "worker"},
		{crewroute.Checker, "high"},
		{crewroute.Planner, "mastermind"},
	} {
		sd, err := eng.DecideClass(shapeOf(slot.class))
		if err != nil {
			continue
		}
		if id := strings.TrimSpace(sd.Chosen); id != "" {
			picks[slot.seat] = id
		}
	}
	return picks
}

// shapeOf is one of the engine's five default classes by its id.
func shapeOf(id string) selector.ClassShape {
	for _, cs := range selector.DefaultClasses() {
		if cs.ID == id {
			return cs
		}
	}
	return selector.ClassShape{}
}

// selectorCatalogOf is the engine's catalog: this request's candidates, read
// into the selector's schema-field row. ONE MODEL IS ONE CANDIDATE, so a
// candidate's model id is the id the pick carries back, and the catalog rows
// only lend the fields the candidate does not carry — the modalities and the
// release line the engine's prior inheritance reads. A candidate whose
// lineage the request says to avoid is left out, the same set the live
// engine's own picking reads.
func selectorCatalogOf(req crewroute.Request) *selector.ModelCatalog {
	rows := crewCatalogRows()
	byLineage := make(map[string]catalog.Model, len(rows))
	for _, row := range rows {
		byLineage[crewroute.Lineage(row.ID)] = row
	}
	var models []selector.Model
	for _, c := range req.Candidates {
		if req.Avoid[crewroute.Lineage(c.Model.ID)] {
			continue
		}
		models = append(models, selectorModelOf(c, byLineage[crewroute.Lineage(c.Model.ID)]))
	}
	if len(models) == 0 {
		return nil
	}
	// NewModelCatalog, not a literal: the constructor indexes the rows for
	// alias resolution and prior inheritance. A literal builds a catalog
	// whose byID/bySlug maps are nil — no panic, but every unscored model
	// silently seats on the global prior, the arm-collapse the engine
	// exists to prevent.
	return selector.NewModelCatalog(models)
}

// selectorModelOf is one candidate as the engine reads it. The candidate is
// the authority on price, indexes and tools — the figures the router already
// routed on; the catalog row, when one matches, is the authority on the
// modalities and the release line the engine's gates and prior inheritance
// read. A candidate with no catalog row is carried on its own figures, and a
// row that publishes no window is carried as one that refused for nothing.
func selectorModelOf(c crewroute.Candidate, row catalog.Model) selector.Model {
	m := selector.Model{
		ID:                c.Model.ID,
		ContextLength:     c.Model.Context,
		PromptPrice:       c.Model.PromptPrice,
		Completion:        c.Model.CompletionPrice,
		CacheRead:         c.Model.CacheReadPrice,
		IntelligenceIndex: c.Model.Intelligence,
		CodingIndex:       c.Model.Coding,
		AgenticIndex:      c.Model.Agentic,
	}
	m.Reasoning.Known, m.Reasoning.Mandatory = row.Reasoning.Known, row.Reasoning.Mandatory
	if row.ID != "" {
		m.CanonicalSlug, m.Created, m.AliasTarget = row.CanonicalSlug, row.Created, row.AliasTarget
		if row.ContextLength > 0 {
			m.ContextLength = row.ContextLength
		}
		m.ModalitiesIn = append([]string(nil), row.InputModalities...)
		m.ModalitiesOut = append([]string(nil), row.OutputModalities...)
	}
	if c.Model.Tools && !hasString(m.ModalitiesIn, "text") {
		// THE SEAT GATE IS THE ROUTER'S, not the modalities list's: a row
		// that lists no input modalities has said nothing either way, and the
		// router already judged the model able to take a tool call.
		m.ModalitiesIn = append(m.ModalitiesIn, "text")
	}
	return m
}

func hasString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
