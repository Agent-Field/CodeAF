package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// fakeV3Catalog is the seam [v3Catalog] exists for: catalog rows with no cache
// file, no network, and no fifteen-second timeout in a unit test.
type fakeV3Catalog struct{ rows []catalog.Model }

func (f fakeV3Catalog) ModelsNow() []catalog.Model { return f.rows }

func TestTheCatalogsContextLengthReachesTheSessionConfig(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/small", ContextLength: 32_000, OutputModalities: []string{"text"}},
		{ID: "vendor/big", ContextLength: 1_000_000, OutputModalities: []string{"text"}},
	}}

	cfg := session.Config{
		Workspace:     t.TempDir(),
		Model:         "vendor/big",
		ContextWindow: v3Window(models, "vendor/big"),
	}
	if cfg.ContextWindow != 1_000_000 {
		t.Fatalf("session.Config.ContextWindow = %d, want the catalog's 1000000", cfg.ContextWindow)
	}

	// A model the catalog does not carry, and a catalog still warming, both
	// answer zero — which is what leaves session on its own default rather than
	// sizing compaction off a guess.
	if got := v3Window(models, "vendor/unheard-of"); got != 0 {
		t.Fatalf("an unknown model answered %d, want 0", got)
	}
	if got := v3Window(fakeV3Catalog{}, "vendor/big"); got != 0 {
		t.Fatalf("a warming catalog answered %d, want 0", got)
	}
	if got := v3Window(nil, "vendor/big"); got != 0 {
		t.Fatalf("no catalog at all answered %d, want 0", got)
	}
}

func TestTheV3ModelListKeepsTheModelsAChatCanTalkTo(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/text", ContextLength: 200_000, OutputModalities: []string{"text"}},
		{ID: "vendor/painter", OutputModalities: []string{"image"}},
		{ID: "vendor/voice", OutputModalities: []string{"speech"}},
		// A row cached before modalities were recorded says nothing, and
		// silence is not "answers in nothing".
		{ID: "vendor/quiet", ContextLength: 8_000},
	}}

	got := v3Models(models)
	want := []tui3.Model{
		{ID: "vendor/text", ContextLength: 200_000},
		{ID: "vendor/quiet", ContextLength: 8_000},
	}
	if len(got) != len(want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is %v, want %v", i, got[i], want[i])
		}
	}

	// Nothing to offer is nil and not an empty list: nil is what sends the
	// picker to its own cache.
	if rows := v3Models(fakeV3Catalog{rows: []catalog.Model{{ID: "vendor/painter", OutputModalities: []string{"image"}}}}); rows != nil {
		t.Fatalf("a catalog of no chat models answered %v, want nil", rows)
	}
}
