package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/session"
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

// THE DOOR CARRIES THE WHOLE CATALOG (docs/MULTIMODAL.md Decision 6), and each
// list narrows it at the moment it is drawn. The door used to drop every row
// that answered in anything but text, which starved the surface of its own
// supply: the drawing slot's picker is opened over THIS list, so a list with no
// drawing model in it was a picker that could not be answered.
func TestTheV3ModelListCarriesTheWholeCatalog(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/text", ContextLength: 200_000, OutputModalities: []string{"text"}},
		{ID: "vendor/painter", OutputModalities: []string{"image"}},
		{ID: "vendor/voice", OutputModalities: []string{"speech"}},
		// A row cached before modalities were recorded says nothing, and
		// silence is not "answers in nothing".
		{ID: "vendor/quiet", ContextLength: 8_000},
	}}

	want := []string{"vendor/text", "vendor/painter", "vendor/voice", "vendor/quiet"}
	got := v3Models(models)
	if len(got) != len(want) {
		t.Fatalf("kept %v, want the whole catalog %v", got, want)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("row %d is %q, want %q — the catalog's own order", i, got[i].ID, want[i])
		}
	}
	// And the modalities travel, because every reader's own filter is asked of
	// them: a row that arrived with only its id would be filtered on nothing.
	if len(got[1].Output) != 1 || got[1].Output[0] != "image" {
		t.Fatalf("the drawing row lost what it makes: %+v", got[1])
	}

	// A catalog with nothing in it at all is nil and not an empty list: nil is
	// what sends the picker to its own cache.
	if rows := v3Models(fakeV3Catalog{}); rows != nil {
		t.Fatalf("a warming catalog answered %v, want nil", rows)
	}
}

// And the list a TASK may be handed to still asks the chat law, because a node
// is an agent with the same belt. This is where the door's old filter went.
func TestTheTaskModelListStaysChatOnly(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		{ID: "vendor/text", OutputModalities: []string{"text"}, InputModalities: []string{"text"}},
		{ID: "vendor/painter", OutputModalities: []string{"image"}},
		{ID: "vendor/ear", OutputModalities: []string{"text"}, InputModalities: []string{"audio"}},
		{ID: "vendor/quiet"},
	}}
	got := v3TaskModels(models)()
	want := []string{"vendor/text", "vendor/quiet"}
	if len(got) != len(want) {
		t.Fatalf("a task may be handed to %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// THE PRICES RIDE THE ROW. The surface has to price something the session
// cannot — what a turn's cache reads saved, which is cached tokens times the gap
// between the prompt price and the cache-read price — and only a catalog knows
// what a token costs. They come off the /models fetch that already happens.
func TestTheV3RowsCarryThePricesAndTheArenaElo(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		{
			ID: "vendor/priced", ContextLength: 128_000, OutputModalities: []string{"text"},
			PromptPrice: 0.00001, CompletionPrice: 0.00005, CacheReadPrice: 0.000001,
			ArenaElo: 1346,
		},
		{
			// OpenRouter's own routers publish "-1": they charge whatever the
			// model they pick charges, and nobody yet knows what that is.
			// Passing the parsed zeros through would tell the surface a
			// router's prompt token is free and let it report a saving that is
			// not a fact.
			ID: "vendor/router", ContextLength: 200_000, OutputModalities: []string{"text"},
			PriceUnknown: true, PromptPrice: -1, CompletionPrice: -1,
		},
	}}

	got := v3Models(models)
	if len(got) != 2 {
		t.Fatalf("kept %d rows, want 2", len(got))
	}
	priced := got[0]
	if priced.PromptPrice != 0.00001 || priced.CacheReadPrice != 0.000001 {
		t.Fatalf("the priced row lost its prices: %+v", priced)
	}
	if priced.CompletionPrice != 0.00005 {
		t.Fatalf("the priced row lost its completion price: %+v", priced)
	}
	if priced.ArenaElo != 1346 {
		t.Fatalf("the priced row lost its arena score: %+v", priced)
	}
	// 9,800 cached tokens saved nine dollars per million: $0.0882. This is the
	// arithmetic internal/tui3 does with the row, spelled out where the row is
	// assembled.
	if saved := 9_800 * (priced.PromptPrice - priced.CacheReadPrice); saved < 0.0881 || saved > 0.0883 {
		t.Fatalf("9.8k cached tokens price out at %v, want ~0.0882", saved)
	}

	router := got[1]
	if router.PromptPrice != 0 || router.CompletionPrice != 0 || router.CacheReadPrice != 0 {
		t.Fatalf("a router's unknown pricing reached the surface as numbers: %+v", router)
	}
	if router.ContextLength != 200_000 {
		t.Fatalf("the router row lost its window: %+v", router)
	}
}

// THE REASONING GATE RIDES THE ROW TOO. The picker offers ctrl+t only on a
// model whose published `supported_parameters` accept a reasoning knob; without
// this field the surface would have to guess, and a guess here is a 400 at the
// next turn.
func TestTheV3RowsCarryWhetherTheModelTakesAReasoningKnob(t *testing.T) {
	models := fakeV3Catalog{rows: []catalog.Model{
		// The dialable kind: `reasoning_effort`, a level this harness can set.
		{ID: "vendor/dial", OutputModalities: []string{"text"},
			Parameters: []string{"max_tokens", "reasoning", "reasoning_effort"}},
		// The switchable kind: a reasoning knob and no level. It is still
		// offered — the adapter resolves what actually travels — because a
		// model that can be asked to think is a model this row is about.
		{ID: "vendor/thinks", OutputModalities: []string{"text"},
			Parameters: []string{"max_tokens", "include_reasoning"}},
		{ID: "vendor/plain", OutputModalities: []string{"text"},
			Parameters: []string{"max_tokens", "temperature"}},
		// Silence is not "supports nothing", but it is not a promise either,
		// and the gate is conservative in the one direction it can afford.
		{ID: "vendor/quiet", OutputModalities: []string{"text"}},
	}}

	want := map[string]bool{
		"vendor/dial": true, "vendor/thinks": true,
		"vendor/plain": false, "vendor/quiet": false,
	}
	rows := v3Models(models)
	if len(rows) != len(want) {
		t.Fatalf("kept %d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		if row.Reasoning != want[row.ID] {
			t.Fatalf("%s carries Reasoning=%v, want %v", row.ID, row.Reasoning, want[row.ID])
		}
	}
}

// --reasoning is validated at the door, before a session file is opened or a
// key is read: a typo is a usage error, never a knob that quietly did nothing
// for a whole conversation.
func TestTheReasoningFlagTakesOnlyTheFourLevels(t *testing.T) {
	for _, good := range []struct{ in, want string }{
		{"", ""}, {"off", ""}, {"low", "low"}, {"MEDIUM", "medium"}, {" high ", "high"},
	} {
		got, ok := session.ParseReasoning(good.in)
		if !ok || got != good.want {
			t.Fatalf("--reasoning %q resolved to %q, %v; want %q, true", good.in, got, ok, good.want)
		}
	}
	if _, ok := session.ParseReasoning("very"); ok {
		t.Fatal("--reasoning very was accepted; the door has to refuse a level nobody has")
	}
}
