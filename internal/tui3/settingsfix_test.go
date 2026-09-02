package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The four defects this slice closed, each pinned by the behaviour a person
// reported rather than by the code that answers it:
//
//   - a model slot in the settings panel opens THE picker, not a plainer list
//     that looks like one;
//   - the context-reuse row says what its 250 means;
//   - a model you cannot talk to is not on offer anywhere the picker draws;
//   - the filter ranks prefix over substring over subsequence, and a query of
//     several words narrows rather than fails.

// mixedModels is a catalog with one of everything the door can hand over: text
// models, a drawing model that also writes (the shape that got past the old
// gate), models that answer in speech and video, and two rows that publish no
// modalities at all — the cache written before the field existed.
var mixedModels = []Model{
	{
		ID: "anthropic/claude-sonnet-4.5", ContextLength: 200_000,
		PromptPrice: 3e-6, CompletionPrice: 1.5e-5, ArenaElo: 1300,
		Output: []string{"text"},
	},
	{ID: "openai/gpt-4.1-mini", ContextLength: 128_000, Output: []string{"text"}},
	// The one the report named: it captions what it draws, so it publishes
	// "text" too, and "text is in the list" kept it.
	{ID: "google/gemini-3.1-flash-image", ContextLength: 131_072, Output: []string{"image", "text"}},
	{ID: "openai/gpt-4o-mini-tts", Output: []string{"speech"}},
	{ID: "bytedance/seedance-1-5-pro", Output: []string{"video"}},
	// Silence: a row from a cache written before modalities travelled. The
	// first is a chat model and stays; the second says what it is in its name.
	{ID: "moonshotai/kimi-k3", ContextLength: 256_000},
	{ID: "openai/gpt-5-image-mini", ContextLength: 400_000},
}

// chatOnly is what mixedModels should come back as, in source order.
var chatOnly = []string{
	"anthropic/claude-sonnet-4.5", "openai/gpt-4.1-mini", "moonshotai/kimi-k3",
}

func modelIDs(models []Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

// ── 1. the settings slot opens the picker itself ────────────────────────────

// A MODEL SLOT OPENS THE MODEL PICKER: filter box, ranking, and rows that carry
// the window, the price and the arena score. Then it writes the row.
func TestASettingsSlotOpensTheModelPickerAndWritesTheRow(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mixedModels }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right")) // Providers
	}
	cursorTo(t, a, config.ModelSettingKey("talk"))
	drive(t, a, key("enter"))

	if a.sheet.sel == nil {
		t.Fatal("a model slot did not open a picker")
	}
	// It is the picker and not a list of ids: every row says what the /model
	// overlay says about the same model.
	screen := plain(frame(a))
	if !strings.Contains(screen, "$3/$15 per M · 200k · elo 1300") {
		t.Fatalf("the slot's rows are not the picker's informative rows:\n%s", screen)
	}
	// And it opens on the model in use, so enter confirms rather than changes.
	if chosen, _ := a.sheet.sel.choice(); chosen != "openai/gpt-4.1-mini" {
		t.Fatalf("the picker opened on %q, want the model in use", chosen)
	}

	// TYPE-TO-FILTER IS THE PICKER'S OWN: tokens, ranked, not a substring scan.
	for _, r := range "son 4.5" {
		drive(t, a, key(string(r)))
	}
	if got := pickedIDs(a.sheet.sel); len(got) != 1 || got[0] != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the slot's filter answered %v, want the one model both words name", got)
	}
	if !strings.Contains(plain(frame(a)), "son 4.5") {
		t.Fatalf("the filter box does not show what was typed:\n%s", plain(frame(a)))
	}

	// Enter writes the row through the registry, which for the conversation's
	// own slot is the running session — one door, the same one /model takes.
	drive(t, a, key("enter"))
	if a.sheet.sel != nil {
		t.Fatal("enter did not close the picker")
	}
	if a.model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the slot wrote %q", a.model)
	}
	if got := a.agent.(*fakeAgent).window; got != 200_000 {
		t.Fatalf("the row was written without its window: %d", got)
	}
	row, _ := a.sheet.registry.Row(config.ModelSettingKey("talk"))
	if row.Value() != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the registry row reads %q", row.Value())
	}
	if !sheetHas(a, "anthropic/claude-sonnet-4.5") {
		t.Fatalf("the panel is still showing the old value:\n%s", strings.Join(sheetLabels(a), "\n"))
	}

	// esc leaves a slot exactly as it was.
	drive(t, a, key("enter"), key("down"), key("esc"))
	if a.sheet.sel != nil {
		t.Fatal("esc did not close the picker")
	}
	if a.model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("esc changed the slot to %q", a.model)
	}
}

// A DRAWING MODEL IS NOT OFFERED IN A SLOT EITHER. The slot and /model read one
// list, so the rule cannot hold in one place and not the other.
func TestASettingsSlotOffersOnlyModelsYouCanTalkTo(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mixedModels }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
	cursorTo(t, a, config.ModelSettingKey("talk"))
	drive(t, a, key("enter"))

	if got := pickedIDs(a.sheet.sel); strings.Join(got, ",") != strings.Join(chatOnly, ",") {
		t.Fatalf("the slot offers %v, want %v", got, chatOnly)
	}
}

// pickedIDs is what a slot's picker currently has on offer, in rank order.
func pickedIDs(sel *sheetSelect) []string {
	out := make([]string, 0, len(sel.pick.hits))
	for _, at := range sel.pick.hits {
		out = append(out, sel.pick.all[at].ID)
	}
	return out
}

// ── 2. the context-reuse row's unit ─────────────────────────────────────────

// THE ROW SAYS WHAT ITS NUMBER MEANS. 250 is not tokens and not a percentage of
// a window: it is cumulative re-sends of the whole context in hundredths, so
// 100 is once. The registry FLOORS it at 100 — below one whole context is a
// refusal, not a governor — which is why the panel must not read as a 0-100
// dial.
func TestTheContextReuseRowStatesItsUnit(t *testing.T) {
	meta := settingUI[config.KeyContextReuse]
	for _, want := range []string{"100 is once", "two and a half times"} {
		if !strings.Contains(meta.about, want) {
			t.Fatalf("the reuse row's line is missing %q: %q", want, meta.about)
		}
	}
	if strings.Contains(meta.about, "as a percent") {
		t.Fatalf("the reuse row still reads as a percentage of something: %q", meta.about)
	}

	a, dir := sheetApp(t)
	a.openSettings()
	drive(t, a, key("right")) // Context
	cursorTo(t, a, config.KeyContextReuse)

	// The number and the sentence that explains it are on screen together.
	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, "250") || !strings.Contains(screen, "100 is once") {
		t.Fatalf("the panel does not show the row beside its unit:\n%s", screen)
	}

	// It is a MULTIPLE and not a percentage: the registry takes 300 and refuses
	// 50 as too small, which is the opposite end from a 0-100 clamp.
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("300")
	drive(t, a, key("enter"))
	if got := config.ContextReuseAt(dir); got != 300 {
		t.Fatalf("a multiple above 100 was refused: the row reads %d", got)
	}
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("50")
	drive(t, a, key("enter"))
	if a.sheet.msg == "" {
		t.Fatal("less than one whole context was accepted silently")
	}
	if got := config.ContextReuseAt(dir); got != 300 {
		t.Fatalf("the refused value was written anyway: %d", got)
	}
}

// V3 and V6: the selected searching row is a live explanation of the current
// pin and keys, and each write promises the very next search rather than a new
// session.
func TestTheSearchingRowFollowsProviderAndKeyWrites(t *testing.T) {
	for _, env := range []string{"EXA_API_KEY", "FIRECRAWL_API_KEY", "JINA_API_KEY"} {
		t.Setenv(env, "")
	}
	a, _ := sheetApp(t)
	a.openSettings()
	drive(t, a, key("right")) // Context
	cursorTo(t, a, config.KeySearchProvider)
	if screen := plain(frame(a)); !strings.Contains(screen, "now firecrawl, keyless — set search.exaKey or search.firecrawlKey to raise it") {
		t.Fatalf("the untouched searching row reads:\n%s", screen)
	}

	// V5: the first press is the safe keyless default, not a keyed pin.
	drive(t, a, key("enter"))
	row, _ := a.sheet.registry.Row(config.KeySearchProvider)
	if got := row.Value(); got != "firecrawl" {
		t.Fatalf("one cycle from auto landed on %q, want firecrawl", got)
	}
	// The remaining order is duckduckgo, exa, which reaches the broken-pin
	// explanation through the same cycle a person uses.
	drive(t, a, key("enter"), key("enter"))
	if screen := plain(frame(a)); !strings.Contains(strings.Join(strings.Fields(screen), " "), "exa is pinned but search.exaKey is not set — every search answers \"Search failed (exa): no API key\". Choose auto, or set the key.") {
		t.Fatalf("the pinned searching row reads:\n%s", screen)
	}

	cursorTo(t, a, config.KeyExaKey)
	drive(t, a, key("enter"))
	a.sheet.edit.box.setText("exa-live")
	drive(t, a, key("enter"))
	cursorTo(t, a, config.KeySearchProvider)
	if screen := plain(frame(a)); !strings.Contains(screen, "exa, with your key") {
		t.Fatalf("the searching row did not follow the key write:\n%s", screen)
	}
}

// ── 3. a picker row is a model you can talk to ──────────────────────────────

// THE FILTER IS ON EVERY RUNG: the door's list, the disk cache and the
// built-ins. The cache is the one that actually broke — it is a file the door
// wrote before this rule existed, and it is full of drawing models.
func TestOnlyModelsThatAnswerInTextReachThePicker(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})

	a.models = func() []Model { return mixedModels }
	if got := modelIDs(a.modelList()); strings.Join(got, ",") != strings.Join(chatOnly, ",") {
		t.Fatalf("the door's list came through as %v, want %v", got, chatOnly)
	}

	// The cache rung, written with the same mixture.
	if err := WriteModelCache(mixedModels); err != nil {
		t.Fatalf("WriteModelCache: %v", err)
	}
	a.models = nil
	if got := modelIDs(a.modelList()); strings.Join(got, ",") != strings.Join(chatOnly, ",") {
		t.Fatalf("the cache let %v through, want %v", got, chatOnly)
	}
	// A cache written with no modality field at all — every row silent — is the
	// live shape of ~/.aforge/v3/models.json, and the id rung is what catches
	// the drawing models in it.
	silent := make([]Model, 0, len(mixedModels))
	for _, model := range mixedModels {
		model.Output = nil
		silent = append(silent, model)
	}
	if err := WriteModelCache(silent); err != nil {
		t.Fatalf("WriteModelCache: %v", err)
	}
	for _, gone := range []string{"google/gemini-3.1-flash-image", "openai/gpt-5-image-mini", "openai/gpt-4o-mini-tts"} {
		if strings.Contains(strings.Join(modelIDs(a.modelList()), ","), gone) {
			t.Fatalf("%q is still on offer from a cache that declares nothing", gone)
		}
	}
	// And a silent chat model is still a chat model: silence alone hides
	// nothing.
	if !strings.Contains(strings.Join(modelIDs(a.modelList()), ","), "moonshotai/kimi-k3") {
		t.Fatal("a row that publishes no modalities was dropped for its silence")
	}

	// The built-ins are all models you can talk to, and none of them is dropped.
	if got, want := len(chatModels(BuiltinModels())), len(BuiltinModels()); got != want {
		t.Fatalf("the built-in list lost %d rows to the filter", want-got)
	}

	// And none of it reaches the screen.
	a.models = func() []Model { return mixedModels }
	a.openPicker()
	screen := plain(frame(a))
	for _, gone := range []string{"image", "tts", "seedance"} {
		if strings.Contains(screen, gone) {
			t.Fatalf("the picker is drawing a model you cannot talk to (%q):\n%s", gone, screen)
		}
	}
}

// The rule for one row, stated as a table: what it PUBLISHES decides, and only
// a row that publishes nothing is read by its name.
func TestAnswersText(t *testing.T) {
	for _, test := range []struct {
		model Model
		want  bool
	}{
		{Model{ID: "openai/gpt-4.1-mini", Output: []string{"text"}}, true},
		{Model{ID: "google/gemini-3.1-flash-image", Output: []string{"image", "text"}}, false},
		{Model{ID: "hexgrad/kokoro-82m", Output: []string{"speech"}}, false},
		// A published "text" beats the name: the id rung never overrides a row
		// that said what it does.
		{Model{ID: "vendor/image-critic", Output: []string{"text"}}, true},
		// Silence, read by the name.
		{Model{ID: "moonshotai/kimi-k3"}, true},
		{Model{ID: "google/gemini-3.1-flash-image"}, false},
		{Model{ID: "openai/gpt-5-image-mini"}, false},
		{Model{ID: "openai/gpt-4o-mini-tts"}, false},
		{Model{ID: "google/veo-3"}, false},
		// The marks are whole words of the id and never substrings: a chat
		// model whose name merely carries the letters is not hidden.
		{Model{ID: "vendor/imagemaster-8b"}, true},
		{Model{ID: "qwen/qwen3-audio-instruct"}, true},
	} {
		if got := answersText(test.model); got != test.want {
			t.Fatalf("answersText(%q, %v) = %v, want %v",
				test.model.ID, test.model.Output, got, test.want)
		}
	}
}

// ── 4. the filter's ranking ─────────────────────────────────────────────────

var rankCatalog = []Model{
	{ID: "anthropic/claude-gpt-echo"},
	{ID: "openai/gpt-4.1-mini"},
	{ID: "gpt-5-classic"},
	{ID: "google/gemma-plus-turbo"},
	{ID: "deepseek/deepseek-v4-flash"},
	{ID: "moonshotai/kimi-k3"},
}

// ranked is the ids a query leaves, in the order the picker would draw them.
func ranked(query string) []string {
	p := picker{}
	p.start(rankCatalog, "")
	p.filter.setText(query)
	p.rank()
	out := make([]string, 0, len(p.hits))
	for _, at := range p.hits {
		out = append(out, p.all[at].ID)
	}
	return out
}

// PREFIX, THEN SUBSTRING, THEN SUBSEQUENCE — a ladder, so the fuzzy hits land
// under the real ones instead of mixed through them.
func TestTheFilterRanksPrefixThenSubstringThenSubsequence(t *testing.T) {
	want := []string{
		"gpt-5-classic",             // the id starts with it
		"openai/gpt-4.1-mini",       // contains it, at offset 7
		"anthropic/claude-gpt-echo", // contains it, further along
		"google/gemma-plus-turbo",   // g … p … t, in order, with gaps
	}
	if got := ranked("gpt"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered to %v, want %v", got, want)
	}
}

// A QUERY OF SEVERAL WORDS IS AN AND, not a phrase: every token has to match,
// each in whichever tier it can, and the tokens need not be adjacent in the id.
func TestTheFilterMatchesEveryTokenOfAQuery(t *testing.T) {
	// The reported case: "ds" is a subsequence of deepseek, "v4" a substring.
	if got := ranked("ds v4"); len(got) != 1 || got[0] != "deepseek/deepseek-v4-flash" {
		t.Fatalf("'ds v4' matched %v, want deepseek/deepseek-v4-flash", got)
	}
	// Order between the words does not matter — they are a set of requirements.
	if got := ranked("v4 ds"); len(got) != 1 || got[0] != "deepseek/deepseek-v4-flash" {
		t.Fatalf("'v4 ds' matched %v, want deepseek/deepseek-v4-flash", got)
	}
	// A second word NARROWS: gpt alone matches four rows, gpt+mini one.
	if got := ranked("gpt mini"); len(got) != 1 || got[0] != "openai/gpt-4.1-mini" {
		t.Fatalf("'gpt mini' matched %v, want openai/gpt-4.1-mini", got)
	}
	// One token nothing carries empties the list, however well the other one
	// matched.
	if got := ranked("deepseek zzz"); len(got) != 0 {
		t.Fatalf("'deepseek zzz' matched %v, want nothing", got)
	}
	// An empty box is the whole list, in the order it was handed over.
	if got := ranked("   "); strings.Join(got, ",") != strings.Join(modelIDs(rankCatalog), ",") {
		t.Fatalf("an empty query filtered to %v", got)
	}
}
