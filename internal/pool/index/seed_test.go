package index

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// seedCap bounds the embedded document's size. It mirrors the cap the generator
// (internal/pool/index/cmd/seedgen) refuses to write past, and sits well under
// the 4 MiB a fetched document is allowed: a seed is a fallback carried in the
// binary, and one that grew into a document is a payload every launch pays for.
const seedCap = 1 << 20

// rawSeedDoc is the embedded document read back as JSON, so a test can assert
// what the file says rather than only what Parse made of it: the fields the
// reader ignores (version) and the ones it floors on (installs) are the seed's
// fidelity to the relay, and neither survives a round trip through Index.
type rawSeedDoc struct {
	Generated string         `json:"generated"`
	Version   json.Number    `json:"version"`
	Rubrics   map[string]int `json:"rubrics"`
	Metrics   map[string]struct {
		Kind string   `json:"kind"`
		Dims []string `json:"dims"`
	} `json:"metrics"`
	Cells []map[string]any `json:"cells"`
}

// decodeRawSeed reads the embedded bytes as the raw document.
func decodeRawSeed(t *testing.T) rawSeedDoc {
	t.Helper()
	var raw rawSeedDoc
	if err := json.Unmarshal(Seed(), &raw); err != nil {
		t.Fatalf("the embedded seed does not decode: %v", err)
	}
	return raw
}

// The seed this build carries is a document a reader accepts: it parses,
// declares the pool's quality metric as gaussian, and carries at least one
// measurement for each of the three seats the picker resolves — so a machine
// with no cache has numbers on its first run rather than an empty index.
func TestSeedCarriesRoleQualityForEverySeat(t *testing.T) {
	seed, err := SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	if kind, ok := seed.Kind("role_quality"); !ok || kind != "gaussian" {
		t.Fatalf("role_quality: kind %q, declared %v", kind, ok)
	}
	byRole := map[string]int{}
	for _, cell := range seed.Cells("role_quality") {
		byRole[cell.Role]++
	}
	for _, role := range []string{"worker", "high", "mastermind"} {
		if byRole[role] == 0 {
			t.Errorf("the seed carries no role_quality cell for %q", role)
		}
	}
}

// Every cell the file carries survives Parse, per metric: the reader floors a
// cell on its installs when it carries them and on its rows when it does not,
// and drops what falls below the floor — so a cell in the file that Parse
// dropped would mean the seed is not the pool the relay published.
func TestEverySeedCellSurvivesParse(t *testing.T) {
	seed, err := SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	raw := decodeRawSeed(t)
	inFile := map[string]int{}
	for _, cell := range raw.Cells {
		metric, _ := cell["metric"].(string)
		inFile[metric]++
	}
	// Every metric the file names, declared or not, is checked — a cell whose
	// metric never parsed in would otherwise pass this test by being invisible.
	metrics := map[string]bool{}
	for _, metric := range seed.Metrics() {
		metrics[metric] = true
	}
	for metric := range inFile {
		metrics[metric] = true
	}
	for metric := range metrics {
		if got, want := len(seed.Cells(metric)), inFile[metric]; got != want {
			t.Errorf("metric %q: Parse kept %d of the file's %d cells", metric, got, want)
		}
	}
}

// Both metrics and both rubrics are DECLARED in the seed — the acceptable
// metric as a declaration and not as a count of its cells, because the pool
// publishes the metric before it has any acceptable measurement to put under
// it, and a fresh install must know the metric exists.
func TestSeedDeclaresBothMetricsAndRubrics(t *testing.T) {
	seed, err := SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	for _, want := range []struct{ metric, kind string }{
		{"role_quality", "gaussian"},
		{"acceptable", "bernoulli"},
	} {
		if kind, ok := seed.Kind(want.metric); !ok || kind != want.kind {
			t.Errorf("metric %q: kind %q, declared %v; want %q", want.metric, kind, ok, want.kind)
		}
	}
	rubrics := seed.Rubrics()
	for _, name := range []string{"role_quality", "acceptable"} {
		if _, ok := rubrics[name]; !ok {
			t.Errorf("the seed declares no rubric for %q", name)
		}
	}
}

// Every cell carries installs, the count the reader floors on: the relay
// publishes it on every cell, and a seed that dropped it would floor a cell on
// its rows instead — a different measurement than the one the pool means.
func TestEverySeedCellCarriesInstalls(t *testing.T) {
	raw := decodeRawSeed(t)
	for i, cell := range raw.Cells {
		if _, ok := cell["installs"]; !ok {
			t.Errorf("cell %d (%v/%v) carries no installs", i, cell["metric"], cell["model"])
		}
	}
}

// The seed is a versioned, dated document: version is a positive integer and
// generated parses as a real day, so Fallback can order it against a cache and
// a reader can say when it was published.
func TestSeedCarriesAPositiveVersionAndARealDay(t *testing.T) {
	raw := decodeRawSeed(t)
	version, err := strconv.ParseInt(raw.Version.String(), 10, 64)
	if err != nil {
		t.Fatalf("the seed's version %q is not an integer: %v", raw.Version, err)
	}
	if version <= 0 {
		t.Errorf("the seed's version is %d, want a positive integer", version)
	}
	stamp, err := time.Parse("2006-01-02", raw.Generated)
	if err != nil {
		t.Fatalf("the seed's generated %q does not parse as a day: %v", raw.Generated, err)
	}
	if stamp.IsZero() {
		t.Error("the seed's generated day is the zero time")
	}
}

// The seed is a fallback carried in the binary, so it is bounded: well under
// the 4 MiB a fetched document is allowed, and under the cap the generator
// refuses to write past.
func TestSeedStaysUnderItsSizeBound(t *testing.T) {
	if size := len(Seed()); size > seedCap {
		t.Fatalf("the seed is %d bytes, over the %d-byte bound", size, seedCap)
	}
}

// Seed hands back a copy: the document is shared by many readers, and one that
// mutated the slice it was given would change what every later call returns.
func TestSeedHandsBackACopy(t *testing.T) {
	first := Seed()
	if len(first) == 0 {
		t.Fatal("the seed is empty")
	}
	first[0] ^= 0xff
	if second := Seed(); second[0] == first[0] {
		t.Fatal("mutating a Seed copy changed the next one")
	}
}
