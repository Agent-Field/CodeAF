package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// TestLedgerPersistsAndReloads is the continual-learning claim in one test.
// Nothing about routing improves across runs unless what one process measured is
// on disk when the next one starts.
func TestLedgerPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		first.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	}
	first.Alias("~a/one-latest", "a/one-0731")
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	rating, count := second.Rating("a/one", provider.ClassPlanSpine, 0)
	if count != 5 || rating <= 0 {
		t.Fatalf("reloaded rating = %.3f over %d observations, want the five that were recorded", rating, count)
	}
	if got := second.Resolve("~a/one-latest"); got != "a/one-0731" {
		t.Fatalf("Resolve = %q, want the snapshot recorded by the first process", got)
	}
}

// TestLedgerIgnoresUngradedVerdicts is the taxonomy doing its job. A transport
// failure and an answer nobody checked are both real events and neither is
// evidence about a model.
func TestLedgerIgnoresUngradedVerdicts(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, verdict := range []provider.Verdict{provider.VerdictProviderFailure, provider.VerdictUnverifiedSuccess} {
		ledger.Observe("a/one", provider.ClassExecLeaf, 0, verdict)
	}
	if _, count := ledger.Rating("a/one", provider.ClassExecLeaf, 0); count != 0 {
		t.Fatalf("ungraded verdicts produced %d observations", count)
	}
	for _, verdict := range []provider.Verdict{provider.VerdictBudgetStop, provider.VerdictTurnCap, provider.VerdictEmptyResponse} {
		ledger.Observe("a/one", provider.ClassExecLeaf, 0, verdict)
	}
	rating, count := ledger.Rating("a/one", provider.ClassExecLeaf, 0)
	if count != 3 || rating >= 0 {
		t.Fatalf("rating = %.3f over %d, want three negatives from the leaf failures", rating, count)
	}
}

// TestRatingsStayFiniteUnderOneSidedEvidence is the regularization guard, and it
// is here because the unregularized version of this fit genuinely broke: a model
// that passed every item it was shown ran to +42 logits, made the information
// matrix singular and collapsed separation reliability to zero.
func TestRatingsStayFiniteUnderOneSidedEvidence(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range 2000 {
		ledger.Observe("perfect/model", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		ledger.Observe("hopeless/model", provider.ClassPlanSpine, 0, provider.VerdictFormatFailure)
	}
	high, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	low, _ := ledger.Rating("hopeless/model", provider.ClassPlanSpine, 0)
	if high > ratingBound || low < -ratingBound {
		t.Fatalf("ratings escaped their bounds: %.3f and %.3f", high, low)
	}
	if high <= low {
		t.Fatalf("ratings did not separate: %.3f against %.3f", high, low)
	}
	// A single observation may never swing a rating: that is what stops one
	// unlucky call from rewriting what a hundred calls established.
	before, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	ledger.Observe("perfect/model", provider.ClassPlanSpine, 0, provider.VerdictFormatFailure)
	after, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	if before-after > maxStep {
		t.Fatalf("one observation moved a rating by %.3f, over the %.2f clamp", before-after, maxStep)
	}
}

// TestColdStartUsesTheRoleHintAndNeverThePrice restates the finding this whole
// package is built to respect. Price does not predict ability: measured over the
// router lab's panel the correlation was 0.46 at n = 7, and the second-cheapest
// model had the second-highest ability while the second-dearest sat sixth of
// seven.
func TestColdStartUsesTheRoleHintAndNeverThePrice(t *testing.T) {
	if coldStart("top") <= coldStart("base") {
		t.Fatal("an operator's role hint is not being read")
	}
	if coldStart("") != 0 {
		t.Fatal("an unhinted model must start diffuse, not somewhere")
	}
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rating, count := ledger.Rating("unseen/model", provider.ClassPlanSpine, 1.5); rating != 1.5 || count != 0 {
		t.Fatalf("unseen model = %.2f over %d, want the prior and no evidence", rating, count)
	}
}

// TestConcurrentWritesDoNotCorruptTheLedger covers the case that actually
// happens: several aforge processes running at once, all doing read-modify-write
// on one small file. Without the lock the last writer out wins and everyone
// else's evidence disappears; without the atomic rename a reader finds half a
// file.
func TestConcurrentWritesDoNotCorruptTheLedger(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 8, 10

	var group sync.WaitGroup
	for writer := range writers {
		group.Add(1)
		go func(writer int) {
			defer group.Done()
			// A separate Ledger per goroutine is the point: they stand in for
			// separate processes, so the in-process mutex is not what is under
			// test here — the file lock is.
			ledger, err := LoadLedger(dir)
			if err != nil {
				t.Error(err)
				return
			}
			for range each {
				ledger.Observe("shared/model", provider.ClassExecLeaf, 0, provider.VerdictVerifiedSuccess)
			}
			if err := ledger.Save(); err != nil {
				t.Error(err)
			}
		}(writer)
	}
	group.Wait()

	data, err := os.ReadFile(filepath.Join(dir, "router-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file ledgerFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the ledger is not valid JSON after concurrent writes: %v\n%s", err, data)
	}
	final, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, count := final.Rating("shared/model", provider.ClassExecLeaf, 0)
	if count != writers*each {
		t.Fatalf("recorded %d observations, want all %d — the merge lost evidence", count, writers*each)
	}
}

// TestPanelParsesBothForms covers the whole configuration surface. The
// comma-separated list is the one people type and it has to work with nothing
// else set up; the file is for a panel worth keeping.
func TestPanelParsesBothForms(t *testing.T) {
	empty, err := LoadPanel("  ")
	if err != nil || len(empty.Models) != 0 {
		t.Fatalf("an unset AFORGE_MODELS must be the kill switch, got %+v %v", empty, err)
	}

	list, err := LoadPanel(" a/one , b/two ,, c/three ")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Models) != 3 || list.Models[0].Slug != "a/one" || list.Models[2].Slug != "c/three" {
		t.Fatalf("list panel = %+v", list.Models)
	}

	path := filepath.Join(t.TempDir(), "models.json")
	body := `{"max_output_price":3.0,"models":[
	  {"slug":"a/one","role":"base"},
	  {"slug":"b/two","role":"top","price":2.4}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := LoadPanel(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.MaxOutputPrice != 3.0 || len(file.Models) != 2 || file.Models[1].Role != "top" || file.Models[1].Price != 2.4 {
		t.Fatalf("file panel = %+v", file)
	}
	if _, err := LoadPanel(filepath.Join(t.TempDir(), "panel.yaml")); err == nil {
		t.Fatal("a YAML path must be refused with an explanation, not parsed as a slug")
	}
}

// TestPanelRefusesAModelOverTheCap is the sanity cap doing what it is for. A
// typo in a slug that resolves to a frontier model should be found at startup,
// not on the invoice.
func TestPanelRefusesAModelOverTheCap(t *testing.T) {
	panel := Panel{MaxOutputPrice: 2.0, Models: []Spec{{Slug: "a/one", Price: 15.0}}}
	if _, err := New(panel, provider.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}, t.TempDir()); err == nil {
		t.Fatal("a model over the cap was accepted")
	}
}
