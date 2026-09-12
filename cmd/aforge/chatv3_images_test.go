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
		// A row cached before modalities were recorded. Silence is not consent —
		// and it is not a verdict either.
		{ID: "vendor/quiet", OutputModalities: []string{"text"}},
	}}

	cfg := session.Config{Model: "vendor/sees", SeesImages: v3SeesImages(models)}
	if got := sightWord(cfg.SeesImages("vendor/sees")); got != "sees" {
		t.Fatalf("a model that publishes image input reads as %v", got)
	}
	// Case is not a difference: it is the same id either way.
	if got := sightWord(cfg.SeesImages("Vendor/Sees")); got != "sees" {
		t.Fatalf("the gate answered case rather than identity: %v", got)
	}
	// A ROW THAT PUBLISHED ITS MODALITIES IS A VERDICT. `vendor/reads` says text
	// and stops: this door has read that and knows the model is blind.
	if got := sightWord(cfg.SeesImages("vendor/reads")); got != "cannot see" {
		t.Fatalf("a row that published text-only input answers %v", got)
	}
	// AND EVERYTHING ELSE IS IGNORANCE, INCLUDING A ROW THAT SAID NOTHING.
	// `vendor/quiet` published no input modalities at all, which is the shape a
	// router uses for a model with no architecture block — not a claim that it
	// cannot see. Grading that silence as a verdict was this door calling its own
	// ignorance a fact, and it was the irreversible one of the two answers for as
	// long as the guard that read it rewrote the transcript (internal/session's
	// blindswap.go). The SEND gate still refuses all three of these, which is the
	// silence law; it refuses them for not being a positive yes.
	for _, model := range []string{"vendor/quiet", "vendor/unheard-of", ""} {
		if got := sightWord(cfg.SeesImages(model)); got != "nobody has said" {
			t.Fatalf("%q, which nobody has published anything about, answers %v rather than ignorance", model, got)
		}
	}

	// A catalog still warming, with no cache on disk either, knows NOTHING — and
	// answers per call, so the same session says yes once the rows land rather
	// than being pinned at boot.
	warming := fakeV3Catalog{}
	if got := sightWord(v3SeesImages(warming)("vendor/sees")); got != "nobody has said" {
		t.Fatalf("a warming catalog answered %v for a model it has not read", got)
	}
	if got := sightWord(v3SeesImages(nil)("vendor/sees")); got != "nobody has said" {
		t.Fatalf("no catalog at all answered %v for a model", got)
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
	if got := sightWord(gate("vendor/sees")); got != "sees" {
		t.Fatalf("the cache's own witness that a model can see reads as %v", got)
	}
	for _, model := range []string{"vendor/reads", "vendor/painter"} {
		if got := sightWord(gate(model)); got != "cannot see" {
			t.Fatalf("%q, whose cached row this door has read, answers %v", model, got)
		}
	}
	// AND A MODEL THE CACHE HAS NEVER HEARD OF IS IGNORANCE, not a verdict.
	if got := sightWord(gate("vendor/unheard-of")); got != "nobody has said" {
		t.Fatalf("a model the cache does not carry answers %v rather than ignorance", got)
	}

	// AND A CATALOG THAT HAS ANSWERED WINS. The cache is the warming rung and
	// never a second opinion: a row the live catalog carries is answered from
	// the live catalog, whatever a stale file says.
	live := v3SeesImages(fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/sees", InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	}})
	if got := sightWord(live("vendor/sees")); got != "cannot see" {
		t.Fatalf("a stale cache overruled the catalog that had already answered: %v", got)
	}
}

// The modality test itself, spelled out: the direction that matters is INPUT,
// an output-image row is a painter rather than a model that can see, and a row
// that listed nothing has said nothing.
func TestTheImageModalityTestIsAboutWhatGoesIn(t *testing.T) {
	if got := sightWord(v3ReadsImages([]string{"text", "IMAGE"})); got != "sees" {
		t.Fatalf("a published image modality was missed on case: %v", got)
	}
	if got := sightWord(v3ReadsImages([]string{"text", "audio"})); got != "cannot see" {
		t.Fatalf("a row that listed its input and left image out answers %v", got)
	}
	// AN EMPTY LIST IS THE THIRD ANSWER. It is a row with no architecture block,
	// and it is the difference between refusing to send a picture — which costs a
	// turn — and hiding one, which used to cost the picture.
	if got := sightWord(v3ReadsImages(nil)); got != "nobody has said" {
		t.Fatalf("a row that published no modalities answers %v rather than ignorance", got)
	}
}

// sightWord is an oracle's two answers as one, so a table can say what it
// expected in the words the design uses.
func sightWord(sees, known bool) string {
	switch {
	case !known:
		return "nobody has said"
	case sees:
		return "sees"
	default:
		return "cannot see"
	}
}
