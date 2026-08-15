package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE VISION GATE READS THE PUBLISHED MODALITIES AND NOTHING ELSE. A model that
// says it takes images takes them; every other answer — a text-only row, a row
// that says nothing, an id the catalog never carried, a catalog still warming —
// is a no, because the alternative is a base64 photo sent to a model that
// cannot read one and a provider error that points nowhere near here.
func TestTheVisionGateAnswersFromTheCatalogsInputModalities(t *testing.T) {
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

	// A catalog still warming answers no — and answers it per call, so the same
	// session says yes once the rows land rather than being pinned at boot.
	warming := fakeV3Catalog{}
	if v3SeesImages(warming)("vendor/sees") {
		t.Fatal("a warming catalog vouched for a model it has not read")
	}
	if v3SeesImages(nil)("vendor/sees") {
		t.Fatal("no catalog at all vouched for a model")
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
