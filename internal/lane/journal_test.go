package lane

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── THE FILE TWO PROCESSES SHARE ────────────────────────────────────────────
//
// The incident these tests are written from happened on a person's own machine:
// two aforge windows, one belief file, and an afternoon of one process's
// learning deleted because the other happened to save second. The state file
// alone cannot fix it — the hierarchy's μ, a[lane] and b[model] are SHARED
// components and there is no field-wise merge for them — so what crosses
// between processes is the observations, and they cross by being replayed.
//
// Every test here moves AFORGE_HOME as well as pointing the store at a
// temporary file, because a test that wrote the real one would cost somebody
// else their belief file (law_test.go, TestNoTestWritesTheRealHome).

// sharedFile is a belief file of this test's own, with the state root moved out
// of the way of whoever is running the tests.
func sharedFile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	return filepath.Join(root, "lanes.json")
}

// TestTwoLedgersOverOneFileSeeEachOthersObservations is the whole point of the
// journal, in the shape the incident happened in: two processes, one file, and
// neither of them able to delete what the other learned.
func TestTwoLedgersOverOneFileSeeEachOthersObservations(t *testing.T) {
	path := sharedFile(t)
	const mine, theirs = "moonshotai/kimi-k3", "z-ai/glm-5.3"

	first := newLedger()
	first.keepIn(newStore().at(path))
	for _, row := range twoProcessRows(mine) {
		first.Prime(row, SheetWeight)
	}

	second := newLedger()
	second.keepIn(newStore().at(path))
	second.Note(Sighting{
		ID: ID{Model: theirs, Lane: "Cloudflare"}, TTFT: 700 * time.Millisecond,
		Gen: time.Second, Tokens: 64, At: noon,
	})

	// The second process saw the first's three lanes the moment it opened, and
	// the first sees the second's after the beat reads back.
	if got := len(second.Beliefs(mine)); got != 3 {
		t.Fatalf("the second process opened and knew %d of the first's three lanes", got)
	}
	first.reload()
	if _, ok := first.Belief(ID{Model: theirs, Lane: "Cloudflare"}); !ok {
		t.Fatal("the first process reloaded and still had not heard of the second's model")
	}

	// And a third process, cold, reads both accounts out of the two files.
	third := newLedger()
	third.keepIn(newStore().at(path))
	if got := len(third.Beliefs(mine)) + len(third.Beliefs(theirs)); got != 4 {
		t.Fatalf("a cold process read %d of the four lanes two processes measured", got)
	}
	// Including what the first primed: the facts came through, not just the id.
	for _, belief := range third.Beliefs(mine) {
		if !belief.Facts.Known() {
			t.Fatalf("%s arrived with every fact blank: %+v", belief.ID.Lane, belief.Facts)
		}
	}
}

// TestAnObservationOutlivesTheProcessThatMadeIt keeps the journal from being a
// cache. Nothing compacts here after the first write, so what the second
// process reads is the log itself.
func TestAnObservationOutlivesTheProcessThatMadeIt(t *testing.T) {
	path := sharedFile(t)
	id := ID{Model: scriptedModel, Lane: "Cloudflare"}

	writing := newLedger()
	writing.keepIn(newStore().at(path))
	for n := range 6 {
		writing.Note(Sighting{
			ID: id, TTFT: 900 * time.Millisecond, Gen: 2 * time.Second, Tokens: 120,
			At: noon.Add(time.Duration(n) * time.Minute),
		})
	}
	before, _ := writing.Belief(id)

	reading := newLedger()
	reading.keepIn(newStore().at(path))
	after, ok := reading.Belief(id)
	if !ok {
		t.Fatal("a new process started from nothing")
	}
	if math.Abs(after.TTFT.X-before.TTFT.X) > 1e-12 || math.Abs(after.TTFT.P-before.TTFT.P) > 1e-12 {
		t.Fatalf("the belief changed on the way through the file: %+v then %+v", before.TTFT, after.TTFT)
	}
	// And so did the hierarchy, which is the half a flat file could not carry.
	early := reading.Wait(id, noon.Add(6*time.Minute))
	if !early[LevelPair].Known() || early[LevelLane].P >= levelVariance(LevelLane) {
		t.Fatalf("the chain did not survive the file: %+v", early)
	}
}

// TestACompactionEmptiesTheJournalAndKeepsTheBelief is the other half of the
// contract: an unbounded replay on every process start is a cold start that
// gets slower every day.
func TestACompactionEmptiesTheJournalAndKeepsTheBelief(t *testing.T) {
	path := sharedFile(t)
	id := ID{Model: scriptedModel, Lane: "Cloudflare"}

	l := newLedger()
	l.keepIn(newStore().at(path))
	for n := range 20 {
		l.Note(Sighting{ID: id, TTFT: time.Second, Gen: 2 * time.Second, Tokens: 120,
			At: noon.Add(time.Duration(n) * time.Minute)})
	}
	before, _ := l.Belief(id)
	l.reload()

	log, err := os.Stat(journalPath(path))
	if err != nil {
		t.Fatalf("the journal is not where the state file's name says it is: %v", err)
	}
	if log.Size() != 0 {
		t.Fatalf("a compaction left %d bytes in the journal", log.Size())
	}
	after, ok := l.Belief(id)
	if !ok || math.Abs(after.TTFT.X-before.TTFT.X) > 1e-12 {
		t.Fatalf("compacting moved the belief from %+v to %+v", before.TTFT, after.TTFT)
	}
	// And the compacted state carries the hierarchy, so the next cold process
	// does not have to replay anything to have one.
	held := readState(path)
	if held.Version != stateVersion || held.Wait.World.P <= 0 {
		t.Fatalf("the compacted state kept no chain: version %d, world %+v", held.Version, held.Wait.World)
	}
}

// TestAVersion1FileMigratesWithoutLoss is the upgrade a person gets by
// installing this build: yesterday's flat array is still every belief they had,
// and the hierarchy starts from that evidence rather than from nothing.
func TestAVersion1FileMigratesWithoutLoss(t *testing.T) {
	path := sharedFile(t)
	yesterday := []Belief{
		{
			ID:    ID{Model: scriptedModel, Lane: "Cloudflare"},
			Facts: Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 345_000, Uptime5m: 100},
			TTFT:  Posterior{X: math.Log(768), P: 0.055}, Rate: Posterior{X: math.Log(58), P: 0.13},
			Quality: Beta{A: 41, B: 2}, At: noon, QualityAt: noon,
		},
		{
			ID:   ID{Model: scriptedModel, Lane: "Modal"},
			TTFT: Posterior{X: math.Log(933), P: 0.06}, Rate: Posterior{X: math.Log(79), P: 0.1},
			At: noon.Add(time.Minute),
		},
	}
	data, err := json.Marshal(yesterday)
	if err != nil {
		t.Fatalf("writing a version 1 file: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing a version 1 file: %v", err)
	}

	l := newLedger()
	l.keepIn(newStore().at(path))
	for _, want := range yesterday {
		got, ok := l.Belief(want.ID)
		if !ok {
			t.Fatalf("%s did not survive the upgrade", want.ID.Lane)
		}
		if got.TTFT != want.TTFT || got.Rate != want.Rate || got.Facts != want.Facts ||
			got.Quality != want.Quality || !got.At.Equal(want.At) {
			t.Fatalf("%s came back as %+v rather than %+v", want.ID.Lane, got, want)
		}
	}
	// And what those beliefs were evidence OF is now in the chain: the world
	// has a pace, both providers have a term, and nothing was invented.
	chain := l.Wait(yesterday[0].ID, noon.Add(time.Minute))
	if chain[LevelWorld].P >= levelVariance(LevelWorld) {
		t.Fatalf("yesterday's file taught the world's pace nothing: %+v", chain[LevelWorld])
	}
	if believed := math.Exp(mustPredict(chain)); believed < 400 || believed > 2000 {
		t.Fatalf("the migrated chain believes the first token takes %.0fms", believed)
	}
}

// mustPredict is a chain's predicted median, in the chain's own unit.
func mustPredict(chain Chain) float64 {
	mu, _ := chain.Predict()
	return mu
}

// TestAJournalLineThatWillNotParseIsSkippedAndCounted is what a machine that
// lost power in the middle of a write is owed.
//
// One half-written record is one observation lost. Refusing to read the file
// around it would be an afternoon lost, and reading it as though it had parsed
// would be a belief about a lane nobody measured.
func TestAJournalLineThatWillNotParseIsSkippedAndCounted(t *testing.T) {
	path := sharedFile(t)
	good := ID{Model: scriptedModel, Lane: "Cloudflare"}
	lines := []string{
		mustLine(t, record{At: noon, Sight: &Sighting{ID: good, TTFT: time.Second, Gen: 2 * time.Second, Tokens: 120, At: noon}}),
		`{"at":"2026-08-30T12:00:00Z","sight":{"ID":{"Model":"m","La`,
		`not json at all`,
		`{"at":"2026-08-30T12:00:00Z"}`,
		mustLine(t, record{At: noon.Add(time.Minute), Out: &Outcome{ID: good, Accepted: true, At: noon.Add(time.Minute)}}),
	}
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(journalPath(path), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the journal by hand: %v", err)
	}

	l := newLedger()
	l.keepIn(newStore().at(path))
	belief, ok := l.Belief(good)
	if !ok || !belief.Known() {
		t.Fatal("three bad lines took the two good ones with them")
	}
	if !belief.Quality.Known() || belief.Quality.A < 1 {
		t.Fatalf("the outcome after the bad lines was never read: %+v", belief.Quality)
	}
	if got := l.Skipped(); got != 3 {
		t.Fatalf("three lines would not parse and %d were counted", got)
	}
}

// mustLine is one record as the journal would have written it.
func mustLine(t *testing.T, entry record) string {
	t.Helper()
	line, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("encoding a record: %v", err)
	}
	return string(line)
}
