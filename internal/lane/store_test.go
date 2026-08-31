package lane

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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
	for _, entry := range entries {
		if entry.Name() != "lanes.json" {
			t.Fatalf("a write left %q behind", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("three writes left %d files", len(entries))
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "[") {
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
	l.Note(Sighting{ID: id, TTFT: 400 * time.Millisecond, At: noon.Add(time.Hour)})
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
	l.Note(Sighting{ID: ID{Model: "m", Lane: "l"}, TTFT: time.Second, At: noon})
	if belief, ok := l.Belief(ID{Model: "m", Lane: "l"}); !ok || !belief.Known() {
		t.Fatal("a ledger with no store believed nothing")
	}
}
