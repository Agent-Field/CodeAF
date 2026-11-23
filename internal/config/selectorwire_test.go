package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// selectorProfile is a profile with an OpenRouter key and a catalog built for
// this file: three open, tooled models and one that can also read an image,
// so the engine's image gate has something to seat.
func selectorProfile(t *testing.T) string {
	t.Helper()
	dir := crewProfile(t)
	rows := []catalog.Model{
		{ID: "z-ai/glm-5.3-flash", CanonicalSlug: "z-ai/glm-5.3-flash-20260826", OpenWeights: true,
			PromptPrice: 4.5e-8, CompletionPrice: 6e-7, CacheReadPrice: 2.85e-8,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "moonshotai/kimi-k3", CanonicalSlug: "moonshotai/kimi-k3-20260715", OpenWeights: true,
			PromptPrice: 3e-6, CompletionPrice: 1.5e-5, CacheReadPrice: 3e-7,
			IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "deepseek/deepseek-v4-flash", CanonicalSlug: "deepseek/deepseek-v4-flash-20260423", OpenWeights: true,
			PromptPrice: 7.168e-8, CompletionPrice: 1.4336e-7, CacheReadPrice: 1.4336e-8,
			IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ContextLength: 1048576, Parameters: []string{"tools"}},
		{ID: "anthropic/claude-opus-5", CanonicalSlug: "anthropic/claude-opus-5-20260723",
			PromptPrice: 5e-6, CompletionPrice: 2.5e-5, CacheReadPrice: 5e-7,
			IntelligenceIndex: 50.8, CodingIndex: 78, AgenticIndex: 56.5, ContextLength: 1000000,
			Parameters: []string{"tools"}, InputModalities: []string{"text", "image"}},
	}
	CrewCatalog = func() []catalog.Model { return rows }
	return dir
}

// fittedSnapshot writes a SharedSnapshot that scores kimi-k3 highest for the
// worker role and opus-5 highest for checker and thinker, with every model
// scored and every variance tight — so the engine's gate has a clear answer
// for each role, and the overlay is provably the engine's, not the live
// router's.
func fittedSnapshot(t *testing.T, dir string) string {
	t.Helper()
	parameters, uncertainty := map[string]float64{}, map[string]float64{}
	counts := map[string]int{}
	for _, role := range []string{"worker", "checker", "thinker"} {
		scores := map[string]float64{
			"z-ai/glm-5.3-flash": 0.55, "moonshotai/kimi-k3": 0.6,
			"deepseek/deepseek-v4-flash": 0.5, "anthropic/claude-opus-5": 0.6,
		}
		switch role {
		case "worker":
			scores["moonshotai/kimi-k3"] = 0.9
		default:
			scores["anthropic/claude-opus-5"] = 0.9
		}
		for model, q := range scores {
			key := "quality/" + role + "/" + model
			parameters[key] = q
			uncertainty[key] = 0.0004
			counts[key] = 12
		}
		parameters["quality_w/"+role+"/residual_sigma"] = 0.02
	}
	type snapshot struct {
		SchemaVersion int                `json:"schema_version"`
		Parameters    map[string]float64 `json:"parameters"`
		Uncertainty   map[string]float64 `json:"uncertainty"`
		Counts        map[string]int     `json:"counts"`
		ModelIDs      []string           `json:"model_ids"`
		GeneratedBy   string             `json:"generated_by"`
	}
	body, err := json.Marshal(snapshot{
		SchemaVersion: 1, Parameters: parameters, Uncertainty: uncertainty, Counts: counts,
		ModelIDs:    []string{"z-ai/glm-5.3-flash", "moonshotai/kimi-k3", "deepseek/deepseek-v4-flash", "anthropic/claude-opus-5"},
		GeneratedBy: "test fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "shared-snapshot.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE WIRING, END TO END: a profile carrying a fitted snapshot seats the
// crew the engine picked — worker, checker and planner — on the live
// decision's own machinery, routed and laddered; the same profile without a
// snapshot seats the crew the live router always picked, byte for byte.
func TestASnapshotConfiguredSeatsTheEngineCrewAndNoneSeatsTheLiveCrew(t *testing.T) {
	dir := selectorProfile(t)
	path := fittedSnapshot(t, dir)

	// THE SAFETY PROPERTY FIRST: with no snapshot configured, the crew is the
	// live router's own and the request carries no overlay at all.
	bare, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if overlay := CrewSelectorPicks(dir, crewroute.Request{Candidates: CrewCandidatesAt(dir)}); overlay != nil {
		t.Fatalf("a profile with no snapshot produced an overlay: %v", overlay)
	}

	if err := SetCrewSelectorSnapshot(dir, path); err != nil {
		t.Fatal(err)
	}
	// A SNAPSHOT THAT VANISHED IS A SNAPSHOT THAT IS NOT CONFIGURED: the same
	// ask routes the bare crew, to the last byte of a pick.
	if err := os.Rename(path, path+".away"); err != nil {
		t.Fatal(err)
	}
	gone, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".away", path); err != nil {
		t.Fatal(err)
	}
	for _, seat := range crewroute.Seats {
		// Admission copies tariff pointers, so compare their values along with the rest of each pick.
		if !reflect.DeepEqual(gone.Seat(seat), bare.Seat(seat)) {
			t.Errorf("%s: a vanished snapshot changed the pick to %+v, want the bare %+v", seat, gone.Seat(seat), bare.Seat(seat))
		}
	}
	if gone.EstUSD != bare.EstUSD || len(gone.Ladder) != len(bare.Ladder) {
		t.Errorf("a vanished snapshot changed the decision: est $%.3f vs $%.3f, %d ladders vs %d",
			gone.EstUSD, bare.EstUSD, len(gone.Ladder), len(bare.Ladder))
	}

	// THE ENGINE'S CREW, on the machinery of the live decision: every seat
	// the engine could seat carries its model, still routed, still laddered,
	// with the estimate recomputed for the crew that runs.
	wired, err := RouteCrew(dir, CrewAsk{Task: crewroute.Task{Text: fixTask}})
	if err != nil {
		t.Fatal(err)
	}
	overlay := CrewSelectorPicks(dir, crewroute.Request{Candidates: CrewCandidatesAt(dir)})
	if len(overlay) != 3 {
		t.Fatalf("the engine seated %v, want all three seats", overlay)
	}
	for seat, want := range overlay {
		got := wired.Seat(seat)
		if got.Model != want {
			t.Errorf("%s runs %s, want the selector's %s", seat, got.Model, want)
		}
		if got.Pinned {
			t.Errorf("%s runs the selector's pick and says pinned", seat)
		}
		if len(wired.Ladder[seat]) == 0 {
			t.Errorf("%s lost its ladder under the overlay", seat)
		}
	}
	// The overlay demonstrably changed the crew: at least one seat differs
	// from what the live router picked for the same ask.
	changed := 0
	for _, seat := range crewroute.Seats {
		if wired.Seat(seat).Model != bare.Seat(seat).Model {
			changed++
		}
	}
	if changed == 0 {
		t.Error("the configured snapshot changed nothing; the overlay never ran")
	}
	if wired.EstUSD <= 0 {
		t.Errorf("the wired crew carries no estimate: $%.3f", wired.EstUSD)
	}
}

// A PIN WINS THROUGH THE FULL ROUTE: a seat the ask pinned runs its pin while
// the selector seats the rest.
func TestAPinnedSeatOutsitsTheSelectorAndTheRestAreSeated(t *testing.T) {
	dir := selectorProfile(t)
	path := fittedSnapshot(t, dir)
	if err := SetCrewSelectorSnapshot(dir, path); err != nil {
		t.Fatal(err)
	}
	d, err := RouteCrew(dir, CrewAsk{
		Task:  crewroute.Task{Text: fixTask},
		Pins:  map[crewroute.Seat]CrewPin{crewroute.Worker: {Model: "z-ai/glm-5.3-flash"}},
		Sends: map[crewroute.Seat]string{crewroute.Planner: "deepseek/deepseek-v4-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(crewroute.Worker); got.Model != "z-ai/glm-5.3-flash" || !got.Pinned {
		t.Errorf("worker %+v, want the person's pin", got)
	}
	if got := d.Seat(crewroute.Planner); got.Model != "deepseek/deepseek-v4-flash" || !got.Pinned {
		t.Errorf("planner %+v, want the one-task pin", got)
	}
	// The checker seat, which nobody pinned, is the selector's.
	overlay := CrewSelectorPicks(dir, crewroute.Request{Candidates: CrewCandidatesAt(dir)})
	if want := overlay[crewroute.Checker]; want != "" {
		if got := d.Seat(crewroute.Checker); got.Model != want {
			t.Errorf("checker %s, want the selector's %s", got.Model, want)
		}
	}
}

// THE OVERLAY IS BUILT FROM THE CANDIDATES, not from the whole catalog: a
// model the person's rule leaves out cannot be seated by the selector either,
// and a lineage the request says to avoid is left out of its pool.
func TestTheSelectorPoolIsTheCandidatePool(t *testing.T) {
	dir := selectorProfile(t)
	if err := SetCrewSelectorSnapshot(dir, fittedSnapshot(t, dir)); err != nil {
		t.Fatal(err)
	}
	req := crewroute.Request{Candidates: CrewCandidatesAt(dir)}
	overlay := CrewSelectorPicks(dir, req)
	if len(overlay) == 0 {
		t.Fatal("the engine seated nothing")
	}
	allowed := map[string]bool{}
	for _, c := range req.Candidates {
		allowed[c.Model.ID] = true
	}
	for seat, model := range overlay {
		if !allowed[model] {
			t.Errorf("%s: the selector picked %s, which no candidate offers", seat, model)
		}
	}
	// With every lineage avoided, the pool is empty and the overlay is nil —
	// the live engine's picks stand.
	avoid := map[string]bool{}
	for _, c := range req.Candidates {
		avoid[crewroute.Lineage(c.Model.ID)] = true
	}
	if overlay := CrewSelectorPicks(dir, crewroute.Request{Candidates: req.Candidates, Avoid: avoid}); overlay != nil {
		t.Errorf("a pool of nobody gave an overlay: %v", overlay)
	}
}

// THE WIRE'S CATALOG IS INDEXED: the engine's alias resolution and its
// line/vendor prior inheritance read that index, and a catalog built as a
// bare literal leaves it nil — no panic, but every unscored model silently
// seats on the global prior, the arm-collapse the selector exists to
// prevent. One alias in the pool proves the index is there.
func TestTheWiredCatalogResolvesAliases(t *testing.T) {
	dir := selectorProfile(t)
	base := CrewCatalog()
	CrewCatalog = func() []catalog.Model {
		return append(base, catalog.Model{
			ID: "z-ai/glm-5.3-flash-alias", CanonicalSlug: "z-ai/glm-5.3-flash-latest",
			AliasTarget: "z-ai/glm-5.3-flash", OpenWeights: true,
			PromptPrice: 4.5e-8, CompletionPrice: 6e-7, CacheReadPrice: 2.85e-8,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9,
			ContextLength: 1310720, Parameters: []string{"tools"},
		})
	}
	req := crewroute.Request{Candidates: CrewCandidatesAt(dir)}
	cat := selectorCatalogOf(req)
	if cat == nil {
		t.Fatal("no catalog from a full candidate pool")
	}
	for i := range cat.Models {
		if cat.Models[i].ID != "z-ai/glm-5.3-flash-alias" {
			continue
		}
		resolved, ok := cat.Resolve(&cat.Models[i])
		if !ok || resolved.ID != "z-ai/glm-5.3-flash" {
			t.Fatalf("the alias resolved to %q (ok=%v), want z-ai/glm-5.3-flash — the wire's catalog lost its index", resolved.ID, ok)
		}
		return
	}
	t.Fatal("the alias candidate never reached the wired catalog")
}

// A BAD PATH IS REFUSED AT SET TIME, not silently ignored at route time.
func TestABadSnapshotPathIsRefusedAndAnEmptyOneClears(t *testing.T) {
	dir := selectorProfile(t)
	if err := SetCrewSelectorSnapshot(dir, filepath.Join(dir, "no-such-snapshot.json")); err == nil {
		t.Fatal("an unloadable snapshot path was accepted")
	}
	if got := CrewSelectorSnapshotAt(dir); got != "" {
		t.Errorf("the refused path was written anyway: %q", got)
	}
	path := fittedSnapshot(t, dir)
	if err := SetCrewSelectorSnapshot(dir, path); err != nil {
		t.Fatal(err)
	}
	if got := CrewSelectorSnapshotAt(dir); got != path {
		t.Errorf("the path reads %q, want %q", got, path)
	}
	if err := SetCrewSelectorSnapshot(dir, "  "); err != nil {
		t.Fatalf("clearing was refused: %v", err)
	}
	if got := CrewSelectorSnapshotAt(dir); got != "" {
		t.Errorf("the cleared row reads %q", got)
	}
}
