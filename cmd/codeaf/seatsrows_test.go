package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
)

// THE HEADLESS DOORS WAIT FOR THE ROWS A PICK NEEDS.
//
// A pick taken off the table, and a tier row that says auto, are answered from
// the rows the process holds (config.AutoModels) — and before the doors waited,
// every cold start read that seam while the warm was still in flight, so the
// seat fell to the family's table row on a machine whose catalog was sitting in
// its own cache file. The chat surface never met the defect because its picks
// happen after the warm has landed. These tests run the doors' own two calls —
// useAutoSeats and config.ResolveSeats, the same sequence `codeaf exec` and
// `codeaf do` climb — against a profile whose catalog is cached, absent and
// unpickable, and read the rung word off the receipt either way.

// seatPickRows is the small catalog a test cache holds: three priced rows,
// spread across the bill and across three vendors — a crew's worker and its
// careful seat may not share a vendor, and a candidate below 80% of the seat's
// best falls off its shortlist — so the rows sit close in quality, far apart in
// price, and each able to sit every seat (a window of 200k, tools, images).
// None of them is a family's table id.
func seatPickRows() []catalog.Model {
	return []catalog.Model{
		{ID: "north/penny", Name: "Penny", ContextLength: 262144,
			PromptPrice: 0.00001, CompletionPrice: 0.00002, CacheReadPrice: 0.000001,
			IntelligenceIndex: 85, CodingIndex: 85, AgenticIndex: 85,
			InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, Parameters: []string{"tools"}},
		{ID: "south/pound", Name: "Pound", ContextLength: 262144,
			PromptPrice: 0.002, CompletionPrice: 0.004, CacheReadPrice: 0.0002,
			IntelligenceIndex: 86, CodingIndex: 86, AgenticIndex: 86,
			InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, Parameters: []string{"tools"}},
		{ID: "east/crown", Name: "Crown", ContextLength: 262144,
			PromptPrice: 0.05, CompletionPrice: 0.1, CacheReadPrice: 0.005,
			IntelligenceIndex: 100, CodingIndex: 100, AgenticIndex: 100,
			InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, Parameters: []string{"tools"}},
	}
}

// blockedCatalogServer is a stand-in base whose /models fetch never returns:
// the cold cache behind a slow network, reduced to the one fact that matters —
// the warm does not land within any bound a test is willing to spend. The
// handler is let go exactly once, because the server refuses to close behind a
// wedged request.
func blockedCatalogServer(t *testing.T) string {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(letGo)
	return server.URL
}

// seatTheTestCatalog replaces the process's memoised catalog with a fresh one
// and restores the seams it seats when the test ends. The once is inside the
// accessor, so tests in one process would otherwise share a catalog pointed at
// whichever of them called first — which is the memoising this file is not
// about.
func seatTheTestCatalog(t *testing.T) {
	t.Helper()
	// The pool's refresh reaches the relay over the network and writes its
	// own cache into the profile as it lands — exactly the late write a
	// TempDir cleanup races. These tests read no measurements, so the pool is
	// off for them: the same off nothing a row saying auto can read, which is
	// the answer the seam already holds when there is no index at all.
	t.Setenv("CODEAF_MODEL_POOL", "off")
	previous := sharedCatalog
	models, index, ownCells := config.AutoModels, config.AutoIndex, config.AutoOwnCells
	sharedCatalog = newSharedCatalog()
	t.Cleanup(func() {
		sharedCatalog = previous
		config.AutoModels, config.AutoIndex, config.AutoOwnCells = models, index, ownCells
	})
}

// boundTheSeatWait shortens [autoSeatRowsBound] for one test and restores it,
// so the bound can be watched without spending the three seconds it carries.
func boundTheSeatWait(t *testing.T, bound time.Duration) {
	t.Helper()
	previous := autoSeatRowsBound
	autoSeatRowsBound = bound
	t.Cleanup(func() { autoSeatRowsBound = previous })
}

// cachedRowHeld fails the test unless the seat resolved to one of the cached
// rows — the brief's "a model from the catalog rows, not the table row".
func cachedRowHeld(t *testing.T, modelID string) {
	t.Helper()
	for _, row := range seatPickRows() {
		if row.ID == modelID {
			return
		}
	}
	t.Fatalf("the seat resolved %q, which is not one of the cached rows", modelID)
}

// A pick off the table is computed from the cached rows, and the receipt says
// which rung answered: `computed from the catalog` under the catalog word,
// `learned` under learn. Both doors resolve through this exact pair of calls,
// and `codeaf exec` prints the work seat's line as its whole report.
func TestADoorWithAPickWaitsForTheCachedRowsAndSaysTheRung(t *testing.T) {
	for _, pick := range []string{config.CrewPickCatalog, config.CrewPickLearn} {
		t.Run(pick, func(t *testing.T) {
			dir := t.TempDir()
			if err := catalog.Remember(catalog.Options{Dir: dir}, seatPickRows()); err != nil {
				t.Fatal(err)
			}
			if err := config.SetCrewPick(dir, pick); err != nil {
				t.Fatal(err)
			}
			seatTheTestCatalog(t)
			t.Setenv(config.ModelEnv, "")
			t.Setenv(config.PlanModelEnv, "")

			useAutoSeats(config.Config{ProfileDir: dir})
			seats := config.ResolveSeats(dir, "", "")

			want := "crew balanced, computed from the catalog"
			if pick == config.CrewPickLearn {
				want = "crew balanced, learned"
			}
			for _, seat := range []config.Seat{seats.Work, seats.Plan} {
				if seat.Rung() != want {
					t.Fatalf("the %s seat reads %q (%s), want the %q rung", seat.Role, seat.Model, seat.Rung(), want)
				}
				cachedRowHeld(t, seat.Model)
			}
			// And not the family's table row: [config.TierModelAt] climbs the same
			// ladder and would answer the computed id here too, so the table is read
			// straight off the family.
			table, ok := config.CrewModelsForSource(config.CrewSourceAt(dir), config.DefaultCrew)
			if !ok || seats.Work.Model == table[config.ModelTierWorker] {
				t.Fatalf("the work seat fell to the family's table row %q over a catalog it holds", table[config.ModelTierWorker])
			}
			// And the one line the exec door prints names the rung too.
			if !strings.Contains(seats.Work.Report(), want) {
				t.Fatalf("the work seat's report does not name the rung:\n%s", seats.Work.Report())
			}
		})
	}
}

// A tier row that says auto is the other way a seat needs the rows, and the
// door waits for them with no pick row in the profile at all.
func TestADoorWithAnAutoRowWaitsForTheCachedRows(t *testing.T) {
	dir := t.TempDir()
	if err := catalog.Remember(catalog.Options{Dir: dir}, seatPickRows()); err != nil {
		t.Fatal(err)
	}
	rows, err := json.Marshal(map[string]string{config.KeyTierWorkerModel: config.AutoValue})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(dir), rows, 0o600); err != nil {
		t.Fatal(err)
	}
	seatTheTestCatalog(t)
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")

	useAutoSeats(config.Config{ProfileDir: dir})
	seats := config.ResolveSeats(dir, "", "")

	if seats.Work.Rung() != "crew balanced, computed from the catalog" {
		t.Fatalf("an auto row resolved to %q (%s), want the computed rung", seats.Work.Model, seats.Work.Rung())
	}
	cachedRowHeld(t, seats.Work.Model)
	table, ok := config.CrewModelsForSource(config.CrewSourceAt(dir), config.DefaultCrew)
	if !ok || seats.Work.Model == table[config.ModelTierWorker] {
		t.Fatalf("the auto row fell to the family's table row %q over a catalog it holds", table[config.ModelTierWorker])
	}
}

// A pick left at the table reads no rows at all, and waits for nothing — not
// even for a warm this test never lets land. The bound is raised to five
// seconds so that a wrongly added wait cannot pass by timing out quietly
// first: the door must be back in under a second.
func TestADoorAtTheTablePickWaitsForNothing(t *testing.T) {
	url := blockedCatalogServer(t)
	dir := t.TempDir()
	if err := config.ApplyCrew(dir, config.CrewBalanced); err != nil {
		t.Fatal(err)
	}
	seatTheTestCatalog(t)
	boundTheSeatWait(t, 5*time.Second)
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")

	started := time.Now()
	useAutoSeats(config.Config{ProfileDir: dir, BaseURL: url})
	if waited := time.Since(started); waited > time.Second {
		t.Fatalf("a door with no pick and no auto row waited %s for rows it does not read", waited)
	}
	seats := config.ResolveSeats(dir, "", "")
	for _, seat := range []config.Seat{seats.Work, seats.Plan} {
		if seat.Rung() != "crew balanced" {
			t.Fatalf("the %s seat reads %q, want the crew's own table row", seat.Role, seat.Rung())
		}
	}
	if seats.Work.Model != config.TierModelAt(dir, config.ModelTierWorker) {
		t.Fatalf("the work seat read %q, want the table row %q", seats.Work.Model, config.TierModelAt(dir, config.ModelTierWorker))
	}
}

// A cold cache behind a network that never answers falls to the table row
// within the bound, and the receipt says so: `table` is a rung word on purpose,
// so a run that fell says it fell.
func TestADoorOnAColdCacheFallsToTheTableRowAndSaysSo(t *testing.T) {
	url := blockedCatalogServer(t)
	dir := t.TempDir()
	if err := config.SetCrewPick(dir, config.CrewPickCatalog); err != nil {
		t.Fatal(err)
	}
	seatTheTestCatalog(t)
	boundTheSeatWait(t, 250*time.Millisecond)
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")

	started := time.Now()
	useAutoSeats(config.Config{ProfileDir: dir, BaseURL: url})
	seats := config.ResolveSeats(dir, "", "")
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the door waited %s, past the bound it was given", waited)
	}
	if !strings.Contains(seats.Report(), "(crew balanced, table)") {
		t.Fatalf("a run that fell to the table row must say so:\n%s", seats.Report())
	}
	if seats.Work.Model != config.TierModelAt(dir, config.ModelTierWorker) {
		t.Fatalf("the work seat read %q, want the table row %q", seats.Work.Model, config.TierModelAt(dir, config.ModelTierWorker))
	}
}

// THE ERRAND'S RECEIPT IS THE PART A HARNESS READS. A profile with the pick
// off the table and a catalog cached beside it runs on the computed ids, and
// both places the run names its rungs — the opening stderr lines and the
// --json object — say `computed from the catalog`.
func TestAnErrandNamesTheRungTheCatalogAnswered(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	if err := config.SetCrewPick(script.dir, config.CrewPickCatalog); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remember(catalog.Options{Dir: script.dir, BaseURL: script.server.URL}, seatPickRows()); err != nil {
		t.Fatal(err)
	}
	seatTheTestCatalog(t)

	var mu sync.Mutex
	var built []string
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		timeout: 60 * time.Second,
		asJSON:  true,
		stdout:  &stdout,
		stderr:  &stderr,
		newClient: func(settings config.Config, model string) (*liveClient, error) {
			mu.Lock()
			built = append(built, model)
			mu.Unlock()
			return script.client(settings, model)
		},
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstderr:\n%s", err, stderr.String())
	}

	if !strings.Contains(stderr.String(), "(crew balanced, computed from the catalog)") {
		t.Fatalf("the opening lines never named the computed rung:\n%s", stderr.String())
	}
	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if outcome.ModelSource != "crew balanced, computed from the catalog" {
		t.Fatalf("--json named the rung %q, want the computed rung", outcome.ModelSource)
	}
	cachedRowHeld(t, outcome.Model)

	// And the computed id is the one the run actually sat somebody in.
	mu.Lock()
	models := append([]string(nil), built...)
	mu.Unlock()
	for _, model := range models {
		if model == outcome.Model {
			return
		}
	}
	t.Fatalf("no client was built on the computed seat %q; the run used %v", outcome.Model, models)
}
