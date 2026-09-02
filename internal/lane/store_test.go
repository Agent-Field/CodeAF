package lane

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// TestABeliefSurvivesBeingWrittenDown is the point of persisting at all: what
// comes back is yesterday's evidence, with the moment it was true still on it,
// so that whoever reads it can discount it correctly.
func TestABeliefSurvivesBeingWrittenDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	keeper := newStore().at(path)
	want := []Belief{{
		ID:      ID{Model: scriptedModel, Lane: "Cloudflare"},
		Facts:   Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 345_000, Uptime5m: 100, PriceOut: 0.00000132, Caches: true},
		TTFT:    Posterior{X: math.Log(768), P: 0.055},
		Rate:    Posterior{X: math.Log(58), P: 0.13},
		Quality: Beta{A: 41, B: 2},
		At:      noon,
	}}
	if err := keeper.Save(want); err != nil {
		t.Fatalf("saving beliefs: %v", err)
	}
	got, err := keeper.Load()
	if err != nil {
		t.Fatalf("loading beliefs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("one belief was written and %d came back", len(got))
	}
	if got[0].ID != want[0].ID || got[0].Facts != want[0].Facts || got[0].Quality != want[0].Quality {
		t.Fatalf("the belief came back as %+v", got[0])
	}
	if math.Abs(got[0].TTFT.Mean()-768) > 1e-9 || math.Abs(got[0].Rate.Mean()-58) > 1e-9 {
		t.Fatalf("the numbers came back as %+v and %+v", got[0].TTFT, got[0].Rate)
	}
	if !got[0].At.Equal(noon) {
		t.Fatalf("the moment came back as %v", got[0].At)
	}
}

// TestAMachineThatHasRoutedNothingLoadsNothingAndSaysNothingIsWrong keeps
// "nothing was kept" from being reported as a failure every time a new machine
// opens a session.
func TestAMachineThatHasRoutedNothingLoadsNothingAndSaysNothingIsWrong(t *testing.T) {
	keeper := newStore().at(filepath.Join(t.TempDir(), "never", "lanes.json"))
	got, err := keeper.Load()
	if err != nil || len(got) != 0 {
		t.Fatalf("a machine with no belief file loaded %d beliefs and %v", len(got), err)
	}
}

// TestTheBeliefFileIsWrittenWholeOrNotAtAll is the atomic write. A reader of
// this file is a cold process deciding where to send its first request, and a
// half-written one would be a belief nobody ever held.
func TestTheBeliefFileIsWrittenWholeOrNotAtAll(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "lanes.json")
	keeper := newStore().at(path)
	for n := range 3 {
		belief := Belief{ID: ID{Model: scriptedModel, Lane: "Cloudflare"}, TTFT: Posterior{X: float64(n), P: 1}, At: noon}
		if err := keeper.Save([]Belief{belief}); err != nil {
			t.Fatalf("saving beliefs: %v", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	// Three names are legitimate and no more: the state, the lock that
	// serialises writers of it, and the observation journal beside it. A
	// TEMPORARY is what this test refuses — a half-written set with a name
	// somebody could read.
	allowed := map[string]bool{"lanes.json": true, "lanes.json" + lockSuffix: true, "lanes.log": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Fatalf("a write left %q behind", entry.Name())
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), `{"version":2`) {
		t.Fatalf("the belief file reads %q (%v)", string(data), err)
	}
}

// TestARowThatNamesNoLaneIsNotLoaded keeps a fact about a machine that was
// never involved out of the ledger, however it got into the file.
func TestARowThatNamesNoLaneIsNotLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	if err := os.WriteFile(path, []byte(`[{"ID":{"Model":"m","Lane":""}},{"ID":{"Model":"m","Lane":"Real"}}]`), 0o600); err != nil {
		t.Fatalf("writing the file by hand: %v", err)
	}
	got, err := newStore().at(path).Load()
	if err != nil {
		t.Fatalf("loading beliefs: %v", err)
	}
	if len(got) != 1 || got[0].ID.Lane != "Real" {
		t.Fatalf("a nameless lane was loaded: %+v", got)
	}
}

// TestTheStoreFollowsTheStateRootWhereverItMoves is why the path is resolved on
// every call: a disposable run moves AFORGE_HOME under a process that is
// already running, and a store holding the path it was born with would keep
// writing into the home it was pointed at first.
func TestTheStoreFollowsTheStateRootWhereverItMoves(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	keeper := newStore()
	if err := keeper.Save([]Belief{{ID: ID{Model: "m", Lane: "l"}, At: noon}}); err != nil {
		t.Fatalf("saving beliefs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "v3", "lanes.json")); err != nil {
		t.Fatalf("the beliefs did not land under the state root: %v", err)
	}
}

// TestANewProcessStartsFromYesterdaysBelief is the whole chain, end to end: a
// ledger writes through its store on every observation, and the next process
// picks the beliefs up without having measured anything.
func TestANewProcessStartsFromYesterdaysBelief(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	row := cloudflareRow()

	yesterday := newLedger()
	yesterday.keepIn(newStore().at(path))
	yesterday.Prime(row, SheetWeight)
	yesterday.Note(Sighting{ID: row.ID, TTFT: 2 * time.Second, Gen: 2 * time.Second, Tokens: 120, At: noon})
	yesterday.NoteOutcome(Outcome{ID: row.ID, Accepted: true, At: noon})
	before, _ := yesterday.Belief(row.ID)

	today := newLedger()
	today.keepIn(newStore().at(path))
	after, ok := today.Belief(row.ID)
	if !ok {
		t.Fatal("a new process started from nothing")
	}
	if math.Abs(after.TTFT.X-before.TTFT.X) > 1e-12 || math.Abs(after.Rate.X-before.Rate.X) > 1e-12 {
		t.Fatalf("the belief changed on the way through the file: %+v then %+v", before, after)
	}
	if after.Quality != before.Quality || after.Facts != before.Facts || !after.At.Equal(noon) {
		t.Fatalf("what the lane IS did not survive: %+v", after)
	}
	// And the moment is still on it, so a caller with a clock can age it. The
	// ledger does not: it has no clock, and ageing on read is the chooser's.
	if aged := after.TTFT.Predict(HalfLife, HalfLife); !(aged.P > after.TTFT.P) {
		t.Fatal("a belief loaded from yesterday could not be aged by its reader")
	}
}

// TestAMeasurementAlreadyMadeBeatsTheFileItLandedBeside keeps a slow first read
// from undoing a sighting that arrived while it was happening.
func TestAMeasurementAlreadyMadeBeatsTheFileItLandedBeside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	id := ID{Model: scriptedModel, Lane: "Cloudflare"}
	stale := newStore().at(path)
	if err := stale.Save([]Belief{{ID: id, TTFT: Posterior{X: math.Log(9000), P: 0.2}, At: noon}}); err != nil {
		t.Fatalf("saving beliefs: %v", err)
	}
	l := newLedger()
	l.Note(Sighting{ID: id, TTFT: 400 * time.Millisecond, Tokens: 1, At: noon.Add(time.Hour)})
	l.keepIn(stale)
	got, _ := l.Belief(id)
	if math.Abs(got.TTFT.Mean()-400) > 1e-9 {
		t.Fatalf("the file overwrote a measurement this process had already made: %.0fms", got.TTFT.Mean())
	}
}

// TestAnUnattachedLedgerKeepsBelievingAnyway holds the seam's own emptiness
// rule: a ledger with nowhere to write is a ledger that forgets at exit, never
// one that refuses to believe.
func TestAnUnattachedLedgerKeepsBelievingAnyway(t *testing.T) {
	l := newLedger()
	l.Note(Sighting{ID: ID{Model: "m", Lane: "l"}, TTFT: time.Second, Tokens: 1, At: noon})
	if belief, ok := l.Belief(ID{Model: "m", Lane: "l"}); !ok || !belief.Known() {
		t.Fatal("a ledger with no store believed nothing")
	}
}

// ── TWO PROCESSES, ONE FILE ─────────────────────────────────────────────────
//
// The scenario these three tests are written from happened on a person's own
// machine and cost them five seconds of staring at an empty line, so it is
// written here in the shape it happened in rather than as an abstraction: two
// aforge processes, one belief file, one of them holding a seventeen-lane
// sheet the other has never heard of.

// twoProcessRows is a sheet for a model only one of the two processes knows
// about — the kimi lanes of the report, shortened to the three that matter.
func twoProcessRows(model string) []Row {
	return []Row{
		{
			ID:    ID{Model: model, Lane: "DeepInfra"},
			Facts: Facts{Tools: true, Quant: "fp8", MaxOut: 16_384, Context: 256_000, Uptime5m: 100, PriceIn: 0.0000005, PriceOut: 0.000002},
			At:    noon, TTFTp50: 1490, TTFTp90: 2400, Ratep50: 21, Ratep90: 30,
		},
		{
			ID:    ID{Model: model, Lane: "Modal"},
			Facts: Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100, PriceIn: 0.0000006, PriceOut: 0.0000024},
			At:    noon, TTFTp50: 933, TTFTp90: 1500, Ratep50: 79, Ratep90: 95,
		},
		{
			ID:    ID{Model: model, Lane: "Fireworks"},
			Facts: Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100, PriceIn: 0.0000007, PriceOut: 0.0000028},
			At:    noon, TTFTp50: 1025, TTFTp90: 1700, Ratep50: 72, Ratep90: 88,
		},
	}
}

// TestASecondProcessSavingCannotDeleteTheFirstsModel is the whole incident, in
// one test: A primes a model B has never heard of, B saves a sighting about its
// own model, and the file afterwards holds both — because a save is a
// read-merge-write and not a replace.
func TestASecondProcessSavingCannotDeleteTheFirstsModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	const mine, theirs = "moonshotai/kimi-k3", "z-ai/glm-5.3"

	a := newLedger()
	a.keepIn(newStore().at(path))
	for _, row := range twoProcessRows(mine) {
		a.Prime(row, SheetWeight)
	}

	b := newLedger()
	b.keepIn(newStore().at(path))
	b.Note(Sighting{ID: ID{Model: theirs, Lane: "Cloudflare"}, TTFT: 700 * time.Millisecond, Gen: time.Second, Tokens: 64, At: noon})

	held, err := newStore().at(path).Load()
	if err != nil {
		t.Fatalf("loading the file both processes wrote: %v", err)
	}
	kept := map[string]int{}
	for _, belief := range held {
		kept[belief.ID.Model]++
	}
	if kept[mine] != 3 || kept[theirs] != 1 {
		t.Fatalf("the file holds %d lanes of %s and %d of %s; the last writer deleted the other's model",
			kept[mine], mine, kept[theirs], theirs)
	}
	// And what the first process primed is still a lane anybody could choose:
	// the facts came through, not just the id.
	for _, belief := range held {
		if belief.ID.Model == mine && !belief.Facts.Known() {
			t.Fatalf("%s survived the merge with every fact blank: %+v", belief.ID.Lane, belief.Facts)
		}
	}

	// Each process now reads back what the other learned. A reads on the beat;
	// B learned it for nothing, because its own save came back merged.
	a.reload()
	if _, ok := a.Belief(ID{Model: theirs, Lane: "Cloudflare"}); !ok {
		t.Fatal("the first process reloaded and still had not heard of the second's model")
	}
	if _, ok := b.Belief(ID{Model: mine, Lane: "Modal"}); !ok {
		t.Fatal("the second process saved into a merge and learned nothing from it")
	}
}

// TestAMergeKeepsTheHalfEachProcessHolds is why the merge is field-wise. One
// process has primed a lane from the sheet and never measured it; the other has
// measured it and never seen a row. Neither reading is newer than the other in
// any sense that would let one of them be dropped.
func TestAMergeKeepsTheHalfEachProcessHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	row := cloudflareRow()
	row.At = noon

	primed := newLedger()
	primed.keepIn(newStore().at(path))
	primed.Prime(row, SheetWeight)

	seen := newLedger()
	seen.keepIn(newStore().at(path))
	seen.Note(Sighting{ID: row.ID, TTFT: 300 * time.Millisecond, Gen: time.Second, Tokens: 120, At: noon.Add(time.Hour)})

	held, _ := newStore().at(path).Load()
	if len(held) != 1 {
		t.Fatalf("one lane, two processes, and %d rows in the file", len(held))
	}
	belief := held[0]
	if !belief.At.Equal(noon.Add(time.Hour)) {
		t.Fatalf("the newer sighting did not win the timing: At is %v", belief.At)
	}
	if belief.Facts != row.Facts {
		t.Fatalf("the sheet's facts were thrown away by a sighting that carried none: %+v", belief.Facts)
	}
	if !belief.Quality.Known() {
		t.Fatal("the primed quality belief did not survive the merge")
	}
}

// TestFourLedgersOverOneFileKeepEverybodysBeliefs is the same law under -race,
// with four processes standing in for the two a person actually runs.
func TestFourLedgersOverOneFileKeepEverybodysBeliefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lanes.json")
	const writers, each = 4, 8
	var running sync.WaitGroup
	for writer := range writers {
		running.Add(1)
		go func(writer int) {
			defer running.Done()
			l := newLedger()
			l.keepIn(newStore().at(path))
			model := fmt.Sprintf("vendor/model-%d", writer)
			for n := range each {
				l.Note(Sighting{
					ID:     ID{Model: model, Lane: fmt.Sprintf("lane-%d", n)},
					TTFT:   time.Duration(200+n) * time.Millisecond,
					Gen:    time.Second,
					Tokens: 64,
					At:     noon.Add(time.Duration(n) * time.Minute),
				})
			}
		}(writer)
	}
	running.Wait()

	// Read back the way a fifth process would: the compacted state plus every
	// observation journalled since it was written. What a process HAS NOT
	// COMPACTED IS NOT LOST — that is the whole point of the journal — so the
	// state file alone is not the question anybody asks of this store.
	fifth := newLedger()
	fifth.keepIn(newStore().at(path))
	held := 0
	for writer := range writers {
		held += len(fifth.Beliefs(fmt.Sprintf("vendor/model-%d", writer)))
	}
	if held != writers*each {
		t.Fatalf("four processes wrote %d beliefs between them and a fifth reads %d",
			writers*each, held)
	}
}
