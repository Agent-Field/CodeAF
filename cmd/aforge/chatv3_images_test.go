package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// THE VISION GATE READS THE PUBLISHED MODALITIES AND NOTHING ELSE. A model that
// says it takes images takes them; every other answer — a text-only row, a row
// that says nothing, an id the catalog never carried, a catalog still warming —
// is a no, because the alternative is a base64 photo sent to a model that
// cannot read one and a provider error that points nowhere near here.
func TestTheVisionGateAnswersFromTheCatalogsInputModalities(t *testing.T) {
	// The gate falls through to internal/tui3's on-disk cache while a catalog
	// is warming, so ~/.aforge/v3/models.json — a real file on a developer's
	// laptop — is moved somewhere empty before any of this is asked.
	t.Setenv("AFORGE_HOME", t.TempDir())
	models := fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/sees", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}},
		{ID: "vendor/reads", InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
		// A row cached before modalities were recorded. Silence is not consent.
		{ID: "vendor/quiet", OutputModalities: []string{"text"}},
	}}

	cfg := session.Config{Model: "vendor/sees", SupportsImages: v3SeesImages(models)}
	if !cfg.SupportsImages("vendor/sees") {
		t.Fatal("a model that publishes image input was gated out")
	}
	// Case is not a difference: it is the same id either way.
	if !cfg.SupportsImages("Vendor/Sees") {
		t.Fatal("the gate answered case rather than identity")
	}
	for _, model := range []string{"vendor/reads", "vendor/quiet", "vendor/unheard-of", ""} {
		if cfg.SupportsImages(model) {
			t.Fatalf("%q was told it can read images", model)
		}
	}

	// A catalog still warming, with no cache on disk either, answers no — and
	// answers it per call, so the same session says yes once the rows land
	// rather than being pinned at boot.
	warming := fakeV3Catalog{}
	if v3SeesImages(warming)("vendor/sees") {
		t.Fatal("a warming catalog vouched for a model it has not read")
	}
	if v3SeesImages(nil)("vendor/sees") {
		t.Fatal("no catalog at all vouched for a model")
	}
}

// THE COLD-CACHE RUNG. On a launch whose catalog is still warming there IS a
// witness — the list internal/tui3 wrote to disk after the last fetch — and
// since the door stopped narrowing that list (docs/MULTIMODAL.md Decision 6) it
// carries every row's input modalities. Without this rung the first minute of
// every launch answered "cannot see" for a model that can, so a photo attached
// in that minute went as its text placeholder and the person was told to switch
// to a model with vision while already sitting on one.
func TestTheVisionGateReadsTheDiskCacheWhileTheCatalogIsWarming(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	if err := tui3.WriteModelCache([]tui3.Model{
		{ID: "vendor/sees", Input: []string{"text", "image"}, Output: []string{"text"}},
		{ID: "vendor/reads", Input: []string{"text"}, Output: []string{"text"}},
		{ID: "vendor/painter", Input: []string{"text"}, Output: []string{"image"}},
	}); err != nil {
		t.Fatal(err)
	}

	gate := v3SeesImages(fakeV3Catalog{})
	if !gate("vendor/sees") {
		t.Fatal("the cache's own witness that a model can see was ignored")
	}
	for _, model := range []string{"vendor/reads", "vendor/painter", "vendor/unheard-of"} {
		if gate(model) {
			t.Fatalf("%q was told it can read images off the cache", model)
		}
	}

	// AND A CATALOG THAT HAS ANSWERED WINS. The cache is the warming rung and
	// never a second opinion: a row the live catalog carries is answered from
	// the live catalog, whatever a stale file says.
	live := v3SeesImages(fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/sees", InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	}})
	if live("vendor/sees") {
		t.Fatal("a stale cache overruled the catalog that had already answered")
	}
}

// The modality test itself, spelled out: the direction that matters is INPUT,
// and an output-image row is a painter rather than a model that can see.
func TestTheImageModalityTestIsAboutWhatGoesIn(t *testing.T) {
	if !v3ReadsImages([]string{"text", "IMAGE"}) {
		t.Fatal("a published image modality was missed on case")
	}
	if v3ReadsImages(nil) || v3ReadsImages([]string{"text", "audio"}) {
		t.Fatal("a row with no image input was read as one that has it")
	}
}
