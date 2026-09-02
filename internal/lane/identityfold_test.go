package lane

// ── A LEDGER ALREADY SPLIT, READ BACK WHOLE ─────────────────────────────────
//
// The doors fold what this process learns. These tests are about what it
// INHERITS: a file written by a build that could not resolve a floating alias
// — or by one that died before its catalog landed — holds two or three rows for
// one model, the facts on one of them and the timing on another, and the
// belief the chooser would read is whichever of them is empty.
//
// The fixture is written as JSON rather than built from this package's own
// types on purpose: it is a file an OLDER BUILD wrote, and a test that
// round-trips today's structs would not notice if today's structs stopped being
// able to read one.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The shipped default's three spellings, as the live catalog publishes them.
// The bare undated id is deliberately NOT among them: it is the 0423 snapshot,
// a different model with its own machines, and folding it in would be this fix
// merging two models rather than three names.
const (
	foldAlias    = "deepseek/deepseek-v4-flash-latest"
	foldServable = "deepseek/deepseek-v4-flash-0731"
	foldOther    = "deepseek/deepseek-v4-flash"
)

// useShippedDefaultFold installs the answer catalog.Servable gives for those
// rows, and takes it back out again afterwards.
func useShippedDefaultFold(t *testing.T) {
	t.Helper()
	resetServable(t)
	UseServable(func(model string) string {
		if strings.TrimPrefix(model, "~") == foldAlias {
			return foldServable
		}
		return model
	})
}

// splitLedgerFile is `v3/lanes.json` as a build that could not fold left it:
// one model, one lane, three keys. The sheet was fetched under the served id
// and carries the facts; the sightings were filed under the alias and carry the
// timing; and the tier-suffixed spelling is the third way the same split used
// to happen, kept here so both layers of the fold are exercised at once.
const splitLedgerFile = `{
  "version": 2,
  "beliefs": [
    {"ID":{"Model":"deepseek/deepseek-v4-flash-0731","Lane":"Relace"},
     "Facts":{"Tools":true,"Quant":"fp8","Context":345000,"Uptime5m":100,"PriceIn":0.0000008,"PriceOut":0.0000016},
     "Quality":{"A":9,"B":1},"QualityAt":"2026-09-01T10:00:00Z"},
    {"ID":{"Model":"deepseek/deepseek-v4-flash-latest","Lane":"Relace"},
     "TTFT":{"X":7.5,"P":0.05},"Rate":{"X":4.6,"P":0.09},"At":"2026-09-01T11:00:00Z"},
    {"ID":{"Model":"deepseek/deepseek-v4-flash-latest:high","Lane":"Relace"},
     "TTFT":{"X":9.9,"P":0.5},"Rate":{"X":1.0,"P":0.5},"At":"2026-08-01T11:00:00Z"},
    {"ID":{"Model":"deepseek/deepseek-v4-flash","Lane":"Relace"},
     "TTFT":{"X":3.0,"P":0.2},"At":"2026-09-01T12:00:00Z"}
  ],
  "priors": [
    {"id":{"Model":"deepseek/deepseek-v4-flash-0731","Lane":"Relace"},"ttft":0.06,"rate":0.11},
    {"id":{"Model":"deepseek/deepseek-v4-flash-latest","Lane":"Relace"}}
  ],
  "wait": {
    "model": {
      "deepseek/deepseek-v4-flash-latest": {"x":0.4,"p":0.3,"at":"2026-09-01T11:00:00Z"},
      "deepseek/deepseek-v4-flash-0731":   {"x":0.1,"p":0.8,"at":"2026-08-20T11:00:00Z"},
      "deepseek/deepseek-v4-flash":        {"x":9.9,"p":0.9,"at":"2026-09-01T12:00:00Z"}
    },
    "pair": {
      "deepseek/deepseek-v4-flash-latest|Relace": {"x":0.2,"p":0.2,"at":"2026-09-01T11:00:00Z"},
      "deepseek/deepseek-v4-flash-0731|Relace":   {"x":0.9,"p":0.4,"at":"2026-08-20T11:00:00Z"}
    },
    "drift": {"deepseek/deepseek-v4-flash-latest|Relace": {"up":1.5}}
  },
  "think": {
    "model": {
      "deepseek/deepseek-v4-flash-latest": {"x":0.7,"p":0.3,"at":"2026-09-01T11:00:00Z"},
      "deepseek/deepseek-v4-flash-0731":   {"x":0.2,"p":0.6,"at":"2026-08-20T11:00:00Z"}
    },
    "pair": {
      "deepseek/deepseek-v4-flash-latest|high": {"x":0.7,"p":0.3,"at":"2026-09-01T11:00:00Z"},
      "deepseek/deepseek-v4-flash-0731|high":   {"x":0.2,"p":0.6,"at":"2026-08-20T11:00:00Z"}
    }
  },
  "judged": {
    "model": {
      "deepseek/deepseek-v4-flash-latest": {"A":30,"B":1,"at":"2026-09-01T11:00:00Z"},
      "deepseek/deepseek-v4-flash-0731":   {"A":8,"B":1,"at":"2026-08-20T11:00:00Z"}
    }
  }
}`

// splitLedger writes that file into a temporary home and opens a ledger on it.
func splitLedger(t *testing.T) (*ledger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lanes.json")
	if err := os.WriteFile(path, []byte(splitLedgerFile), 0o600); err != nil {
		t.Fatalf("stage the split belief file: %v", err)
	}
	held := newLedger()
	held.keepIn(newStore().at(path))
	return held, path
}

// TestAFileSplitAcrossThreeNamesLoadsAsOneBelief is the general fold's half of
// issue #289: the four doors stop new splits, and this stops the ones already
// on every machine that has run this build.
//
// The two halves must both survive, which is why the fixture separates them: a
// merge that simply kept the newer row would keep the timing and lose the
// facts, and a lane with no facts is a lane the gate cannot judge.
func TestAFileSplitAcrossThreeNamesLoadsAsOneBelief(t *testing.T) {
	useShippedDefaultFold(t)
	held, _ := splitLedger(t)

	served := held.Beliefs(foldServable)
	if len(served) != 1 {
		t.Fatalf("the served id holds %d beliefs, want the three spellings folded into one: %+v", len(served), served)
	}
	one := served[0]
	if one.ID.Model != foldServable {
		t.Fatalf("the surviving belief is keyed on %q", one.ID.Model)
	}
	// The newer sighting won the timing...
	if math.Abs(one.TTFT.X-7.5) > 1e-9 || math.Abs(one.Rate.X-4.6) > 1e-9 {
		t.Errorf("the timing came back as %+v and %+v, want the freshest row's", one.TTFT, one.Rate)
	}
	// ...and the row that was never measured still gave up what it knew.
	if !one.Facts.Known() || !one.Facts.Tools || one.Facts.Context != 345_000 {
		t.Errorf("the facts did not survive the fold: %+v", one.Facts)
	}
	if one.Quality.A != 9 {
		t.Errorf("the quality did not survive the fold: %+v", one.Quality)
	}

	// ASKING UNDER EITHER SPELLING IS ASKING ABOUT ONE MODEL — that is what the
	// key being one key MEANS, and it is what the chooser depends on.
	if alias := held.Beliefs(foldAlias); len(alias) != 1 || alias[0].ID != one.ID {
		t.Errorf("asked under the alias the ledger answered %+v", alias)
	}

	// AND THE OTHER SNAPSHOT IS STILL ITS OWN MODEL. `deepseek/deepseek-v4-flash`
	// is the 0423 build with its own machines; folding it in would be this fix
	// merging two models rather than three names for one.
	other := held.Beliefs(foldOther)
	if len(other) != 1 || math.Abs(other[0].TTFT.X-3.0) > 1e-9 {
		t.Fatalf("the older snapshot was folded away or altered: %+v", other)
	}
}

// TestTheHierarchyIsRekeyedWithTheBeliefsItExplains keeps the four levels and
// the flat belief from disagreeing about which model they are about. A belief
// folded onto the served id while b[model] stayed under the alias would be a
// pair whose parents say nothing about it — cold start, restored from a file
// that was not cold.
func TestTheHierarchyIsRekeyedWithTheBeliefsItExplains(t *testing.T) {
	useShippedDefaultFold(t)
	held, _ := splitLedger(t)
	// One door is enough to make the ledger read its file.
	held.Beliefs(foldServable)

	for name, level := range map[string]map[string]node{
		"wait model": held.wait.Model,
		"wait pair":  held.wait.Pair,
		"think":      held.think.Model,
	} {
		for key := range level {
			if strings.Contains(key, foldAlias) {
				t.Errorf("the %s level is still keyed on %q", name, key)
			}
		}
	}
	// THE FRESHER NODE WINS, for the same reason the fresher belief does.
	if got := held.wait.Model[foldServable]; math.Abs(got.X-0.4) > 1e-9 {
		t.Errorf("b[model] came back as %+v, want the fresher of the two spellings", got)
	}
	if got := held.wait.Pair[foldServable+"|Relace"]; math.Abs(got.X-0.2) > 1e-9 {
		t.Errorf("e[model,lane] came back as %+v", got)
	}
	if got := held.judged.Model[foldServable]; got.A != 30 {
		t.Errorf("the model's quality evidence came back as %+v", got)
	}
	// The other snapshot keeps its own levels.
	if got := held.wait.Model[foldOther]; math.Abs(got.X-9.9) > 1e-9 {
		t.Errorf("the older snapshot's level was folded away: %+v", got)
	}
	// A thinking chain is keyed "model|rung" and its model half folds too.
	if got := held.think.Pair[foldServable+"|high"]; math.Abs(got.X-0.7) > 1e-9 {
		t.Errorf("the thinking leaf came back as %+v", got)
	}
	if _, still := held.wait.Drift[foldAlias+"|Relace"]; still {
		t.Error("a drift sum is still keyed on the alias")
	}
}

// TestTheFoldedFileIsWrittenBackFolded is why this is a one-time cost rather
// than a fold repeated on every process for ever: the merge that writes is the
// merge that re-keys, so the next build to open this file opens one row.
func TestTheFoldedFileIsWrittenBackFolded(t *testing.T) {
	useShippedDefaultFold(t)
	held, path := splitLedger(t)
	held.Note(Sighting{
		ID: ID{Model: foldAlias, Lane: "Relace"}, TTFT: 900 * time.Millisecond,
		Gen: time.Second, Tokens: 120, At: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
	})
	if err := held.Flush(); err != nil {
		t.Fatalf("flushing the ledger: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the belief file back: %v", err)
	}
	var written struct {
		Beliefs []struct{ ID ID }
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode the belief file: %v", err)
	}
	names := map[string]bool{}
	for _, belief := range written.Beliefs {
		names[belief.ID.Model] = true
	}
	if names[foldAlias] || names[foldAlias+":high"] {
		t.Errorf("the file was written back split: %v", names)
	}
	if !names[foldServable] || !names[foldOther] || len(names) != 2 {
		t.Errorf("the file holds %v, want the served id and the other snapshot", names)
	}
}

// TestASheetIsAskedForUnderTheNameTheBeliefsAreFiledUnder is door 4: a picker
// hands a raw spelling to [WantSheet], and a sheet queued as typed is fetched
// from a page the router does not publish and filed where nothing reads it.
func TestASheetIsAskedForUnderTheNameTheBeliefsAreFiledUnder(t *testing.T) {
	useShippedDefaultFold(t)
	pages := newSheet()
	pages.Wants("~" + foldAlias)
	select {
	case queued := <-pages.want:
		if queued != foldServable {
			t.Fatalf("the beat was asked to fetch %q, want the id the router publishes a page for", queued)
		}
	default:
		t.Fatal("nothing was queued for the beat at all")
	}
	// And once, because both spellings are one model: the claim is held under
	// the folded name, so the alias cannot buy a second fetch of one page.
	pages.Wants(foldServable)
	select {
	case queued := <-pages.want:
		t.Fatalf("one model was queued twice, the second time as %q", queued)
	default:
	}
}
