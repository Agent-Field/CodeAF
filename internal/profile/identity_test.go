package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetIdentity puts the process-wide seams back for one test. Identity is
// memoised and the adoption is one-shot per path, both deliberately, so a test
// that installs a resolver has to clear what an earlier one remembered.
func resetIdentity(t *testing.T) {
	t.Helper()
	clear := func() {
		UseIdentity(nil)
		adopted.Range(func(key, _ any) bool { adopted.Delete(key); return true })
	}
	clear()
	t.Cleanup(clear)
}

// TestIdentityCollapsesTheRoutingMarkerAndCase is the free half, and the one
// that has to hold with no catalog anywhere: "~x" and "x" are one model, which
// is the doctrine every other reader in the tree already honours.
func TestIdentityCollapsesTheRoutingMarkerAndCase(t *testing.T) {
	resetIdentity(t)
	for _, spelling := range []string{"minimax/minimax-m2.7", "~minimax/minimax-m2.7", "  ~MiniMax/MiniMax-M2.7 "} {
		if got := Identity(spelling); got != "minimax/minimax-m2.7" {
			t.Errorf("Identity(%q) = %q, want the one model behind it", spelling, got)
		}
	}
}

// TestIdentityAppliesTheInstalledResolver is the catalog half: two spellings the
// operator used on two days, resolved to the identity the records belong to.
func TestIdentityAppliesTheInstalledResolver(t *testing.T) {
	resetIdentity(t)
	UseIdentity(func(model string) string {
		if model == "anthropic/claude-opus-5" || model == "anthropic/claude-opus-5-latest" {
			return "anthropic/claude-opus-5-20260723"
		}
		return model
	})
	for _, spelling := range []string{"anthropic/claude-opus-5", "~anthropic/claude-opus-5-latest"} {
		if got := Identity(spelling); got != "anthropic/claude-opus-5-20260723" {
			t.Errorf("Identity(%q) = %q, want the resolved identity", spelling, got)
		}
	}
	if got := Identity("nobody/nothing"); got != "nobody/nothing" {
		t.Errorf("Identity of an unknown model = %q, want it forwarded as written", got)
	}
}

// TestIdentityDoesNotMoveUnderAWarmingCatalog is the stability guarantee. The
// installed resolver reads a catalog that lands in the background, so asked
// twice it would honestly give two answers — and a file key that moved halfway
// through a run would split one process's history in two.
func TestIdentityDoesNotMoveUnderAWarmingCatalog(t *testing.T) {
	resetIdentity(t)
	warm := false
	UseIdentity(func(model string) string {
		if warm {
			return "anthropic/claude-opus-5-20260723"
		}
		return model
	})
	first := Identity("anthropic/claude-opus-5")
	warm = true
	if second := Identity("anthropic/claude-opus-5"); second != first {
		t.Fatalf("identity moved under a warming catalog: %q then %q", first, second)
	}
}

// TestLoadKeysTheFileOnTheIdentity is the fix at the file name, which is where
// the divergence actually happened.
func TestLoadKeysTheFileOnTheIdentity(t *testing.T) {
	resetIdentity(t)
	dir := t.TempDir()
	UseIdentity(func(string) string { return "anthropic/claude-opus-5-20260723" })

	measured, err := Load(dir, "anthropic/claude-opus-5", "linear")
	if err != nil {
		t.Fatal(err)
	}
	measured.Add(Record{Title: "a leaf", Size: "atomic", Turns: 3, Tokens: 1000})
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "profile-anthropic-claude-opus-5-20260723-linear.json")
	if _, err := os.Stat(want); err != nil {
		listing, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		t.Fatalf("the profile was not keyed on the identity; found %v", listing)
	}
	// The other spelling opens the same history rather than starting one.
	other, err := Load(dir, "~anthropic/claude-opus-5-latest", "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Records) != 1 {
		t.Fatalf("the second spelling opened %d records, want the one history", len(other.Records))
	}
}

// TestLoadAdoptsTheHistoryWrittenUnderAnotherSpelling is the migration, and it
// is the whole reason a late-installed resolver costs nothing: the evidence
// written before anybody could resolve the name is not lost, it is merged.
func TestLoadAdoptsTheHistoryWrittenUnderAnotherSpelling(t *testing.T) {
	resetIdentity(t)
	dir := t.TempDir()
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	writeProfile(t, dir, "profile-anthropic-claude-opus-5-linear.json", &Profile{
		Model: "anthropic/claude-opus-5", Subharness: "linear", Anchors: "the older ruler",
		Records: []Record{
			{Title: "undated spelling", Size: "atomic", Turns: 3, Tokens: 13_223, Time: base},
		},
	})
	writeProfile(t, dir, "profile-anthropic-claude-opus-5-latest-linear.json", &Profile{
		Model: "~anthropic/claude-opus-5-latest", Subharness: "linear",
		Records: []Record{
			{Title: "floating spelling", Size: "atomic", Turns: 9, Tokens: 44_704, Time: base.Add(time.Hour)},
		},
	})
	// A different model's file, and a different worker's, neither of which may
	// be adopted.
	writeProfile(t, dir, "profile-someone-else-linear.json", &Profile{
		Model: "someone/else", Subharness: "linear",
		Records: []Record{{Title: "another model", Size: "atomic", Turns: 1, Tokens: 1, Time: base}},
	})
	writeProfile(t, dir, "profile-anthropic-claude-opus-5-swe.json", &Profile{
		Model: "anthropic/claude-opus-5", Subharness: "swe",
		Records: []Record{{Title: "another worker", Size: "atomic", Turns: 1, Tokens: 2, Time: base}},
	})

	UseIdentity(func(model string) string {
		if strings.HasPrefix(model, "anthropic/claude-opus-5") {
			return "anthropic/claude-opus-5-20260723"
		}
		return model
	})

	measured, err := Load(dir, "anthropic/claude-opus-5", "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(measured.Records) != 2 {
		t.Fatalf("adopted %d records, want the two histories merged: %+v", len(measured.Records), measured.Records)
	}
	if measured.Records[0].Title != "undated spelling" || measured.Records[1].Title != "floating spelling" {
		t.Fatalf("the merge is not in the order it happened: %+v", measured.Records)
	}
	if measured.Anchors != "the older ruler" {
		t.Errorf("anchors = %q, want the ruler the adopted history had rewritten", measured.Anchors)
	}
	// Idempotent: the sources are never mutated, so a second read is the same
	// read, and materialising the merge does not double it.
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir, "~anthropic/claude-opus-5-latest", "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Records) != 2 {
		t.Fatalf("a second read saw %d records, want the same two", len(again.Records))
	}
}

// TestLoadWithoutAResolverKeepsEveryExistingFileName is the compatibility claim
// that matters most: on a surface with no catalog, nothing about the file layout
// changes, so every profile already on disk keeps being the one that is read.
func TestLoadWithoutAResolverKeepsEveryExistingFileName(t *testing.T) {
	resetIdentity(t)
	dir := t.TempDir()
	for _, spelling := range []string{"deepseek/deepseek-v4-flash", "~deepseek/deepseek-v4-flash", "DeepSeek/DeepSeek-V4-Flash"} {
		measured, err := Load(dir, spelling, "linear")
		if err != nil {
			t.Fatal(err)
		}
		measured.Add(Record{Title: "a leaf", Size: "atomic", Turns: 1, Tokens: 10})
		if err := measured.Save(); err != nil {
			t.Fatal(err)
		}
	}
	listing, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 1 {
		t.Fatalf("three spellings of one model wrote %d files: %v", len(listing), listing)
	}
	if filepath.Base(listing[0]) != "profile-deepseek-deepseek-v4-flash-linear.json" {
		t.Fatalf("the file name moved: %s", filepath.Base(listing[0]))
	}
}

func writeProfile(t *testing.T, dir, name string, contents *Profile) {
	t.Helper()
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
