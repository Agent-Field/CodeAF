package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The three defects this slice closed, each pinned by what a person reported:
//
//   - the five media slots on the Providers tab opened a picker in which no row
//     could answer them — "drawing" over the chat list is not a narrow list, it
//     is the exact complement of the right one;
//   - a slot this surface cannot write said the wrong thing about why;
//   - the legend's hint slot named keys the keyboard was not pointed at.

// mediaCatalog is one row of every family a door can hand over, each publishing
// what it makes, plus a silent chat row.
var mediaCatalog = []Model{
	{ID: "anthropic/claude-sonnet-4.5", Output: []string{"text"}, Input: []string{"text", "image"}},
	{ID: "google/gemini-3.1-flash-image", Output: []string{"image", "text"}, Input: []string{"text"}},
	{ID: "openai/gpt-4o-mini-tts", Output: []string{"speech"}},
	{ID: "google/lyria-3", Output: []string{"music"}},
	{ID: "bytedance/seedance-1-5-pro", Output: []string{"video"}},
	{ID: "openai/whisper-large-v3", Output: []string{"text"}, Input: []string{"audio"}},
	{ID: "moonshotai/kimi-k3"},
}

// ── 1. one slot, one question ───────────────────────────────────────────────

// EVERY SLOT ASKS ITS OWN QUESTION, and the five media ones ask about what a
// model MAKES rather than about whether you can talk to it. Answered with the
// chat law — which is what every one of them was answered with — each of these
// lists is empty of the only rows that could fill it.
func TestEachModelSlotAsksItsOwnQuestion(t *testing.T) {
	for _, test := range []struct {
		slot string
		want []string
	}{
		{"talk", []string{"anthropic/claude-sonnet-4.5", "moonshotai/kimi-k3"}},
		{"plan", []string{"anthropic/claude-sonnet-4.5", "moonshotai/kimi-k3"}},
		{"image", []string{"google/gemini-3.1-flash-image"}},
		{"speech", []string{"openai/gpt-4o-mini-tts"}},
		{"music", []string{"google/lyria-3"}},
		{"video", []string{"bytedance/seedance-1-5-pro"}},
		{"voice", []string{"openai/whisper-large-v3"}},
	} {
		keep := filterFor(config.ModelSettingKey(test.slot))
		if keep == nil {
			t.Fatalf("the %q slot has no question", test.slot)
		}
		got := modelIDs(keepModels(mediaCatalog, keep))
		if strings.Join(got, ",") != strings.Join(test.want, ",") {
			t.Fatalf("the %q slot offers %v, want %v", test.slot, got, test.want)
		}
	}
}

// The media predicates as a table: what a row PUBLISHES decides, and a row that
// publishes nothing is read by its name — which for a media slot means it is
// DROPPED unless the name says what it makes. That polarity is the opposite of
// the chat law's and it is the point: the same silent cache that must not lose
// its chat models must not fill a drawing picker with them either.
func TestTheMediaPredicates(t *testing.T) {
	for _, test := range []struct {
		model                                 Model
		draws, speaks, composes, films, hears bool
	}{
		{Model{ID: "google/gemini-3.1-flash-image", Output: []string{"image", "text"}}, true, false, false, false, false},
		{Model{ID: "openai/gpt-4o-mini-tts", Output: []string{"speech"}}, false, true, false, false, false},
		{Model{ID: "google/lyria-3", Output: []string{"music"}}, false, false, true, false, false},
		{Model{ID: "bytedance/seedance-1-5-pro", Output: []string{"video"}}, false, false, false, true, false},
		{Model{ID: "openai/whisper-large-v3", Output: []string{"text"}, Input: []string{"audio"}}, false, false, false, false, true},
		// Silence, read by the name — the families whose names mean one thing.
		{Model{ID: "black-forest-labs/flux-1.1-pro"}, true, false, false, false, false},
		{Model{ID: "hexgrad/kokoro-82m"}, false, true, false, false, false},
		{Model{ID: "google/veo-3"}, false, false, false, true, false},
		{Model{ID: "qwen/qwen3-asr-flash"}, false, false, false, false, true},
		// A silent chat row answers no media slot at all, which is the whole
		// difference from [answersText]'s reading of the same silence.
		{Model{ID: "moonshotai/kimi-k3"}, false, false, false, false, false},
		// The marks are whole words of the id and never substrings.
		{Model{ID: "vendor/videographer-8b"}, false, false, false, false, false},
		{Model{ID: "vendor/imagemaster-8b"}, false, false, false, false, false},
	} {
		for _, ask := range []struct {
			name string
			got  bool
			want bool
		}{
			{"drawsImages", drawsImages(test.model), test.draws},
			{"speaksAloud", speaksAloud(test.model), test.speaks},
			{"composesMusic", composesMusic(test.model), test.composes},
			{"filmsVideo", filmsVideo(test.model), test.films},
			{"hearsSpeech", hearsSpeech(test.model), test.hears},
		} {
			if ask.got != ask.want {
				t.Fatalf("%s(%q, in=%v out=%v) = %v, want %v",
					ask.name, test.model.ID, test.model.Input, test.model.Output, ask.got, ask.want)
			}
		}
	}
}

// ── 2. the slot's picker is resolved through that question ──────────────────

// THE LIST IS NARROWED INSIDE THE LADDER, not after it. A slot that filtered
// [app.modelList]'s answer would be filtering a list its own rows had already
// been taken out of, which is exactly how the drawing slot came to offer a
// picker that could not contain a drawing model.
func TestAMediaSlotsPickerHoldsTheModelsThatAnswerIt(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mediaCatalog }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right")) // Providers
	}
	cursorTo(t, a, config.ModelSettingKey("image"))
	drive(t, a, key("enter"))

	if a.sheet.sel == nil {
		t.Fatal("the drawing slot did not open a picker")
	}
	want := []string{"google/gemini-3.1-flash-image"}
	if got := pickedIDs(a.sheet.sel); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the drawing slot offers %v, want %v", got, want)
	}

	// AND THE REFUSAL NAMES WHERE THE VALUE LIVES. A media slot is an
	// environment slot; "chosen where its session is opened" is true of the role
	// rows and sends a person looking for a door that does not exist.
	drive(t, a, key("enter"))
	if !strings.Contains(a.sheet.msg, "AFORGE_IMAGE_MODEL") {
		t.Fatalf("the refusal does not say where the drawing model is set: %q", a.sheet.msg)
	}
}

// A slot with no candidate at all says so where the list was, rather than
// falling back to a list that answers a different question.
func TestASlotWithNoCandidateSaysSo(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mediaCatalog }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
	cursorTo(t, a, config.ModelSettingKey("image"))
	drive(t, a, key("enter"))
	for _, r := range "zzz" {
		drive(t, a, key(string(r)))
	}
	if got := pickedIDs(a.sheet.sel); len(got) != 0 {
		t.Fatalf("a query nothing carries matched %v", got)
	}
	if !strings.Contains(plain(frame(a)), "no model matches") {
		t.Fatalf("an empty list did not say so:\n%s", plain(frame(a)))
	}
}

// ── 3. the hint slot follows the KEYBOARD ───────────────────────────────────

// THE ORDER IS input.go's ROUTING ORDER. A hint is only true if it names the
// keys the handler that reads first would take, and copy mode is the state this
// slot was most wrong about: every key means something else while the viewport
// is frozen, and the slot was drawing the input box's own two affordances.
func TestTheHintSlotFollowsTheKeyboard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})

	a.copy.on = true
	if got := a.hintWord(); got != "v select · a block · y yank · esc" {
		t.Fatalf("copy mode offered %q", got)
	}
	// And the handed-over pointer leads even copy mode, because the key that
	// ends it is read above everything (input.go's [app.key]).
	a.released = true
	if got := a.hintWord(); got != "drag to select · any key ends it" {
		t.Fatalf("a handed-over pointer offered %q", got)
	}
	a.released = false
	// The picker is read ABOVE copy mode (input.go reads it before ctrl+c), so
	// it wins the slot when both are somehow up.
	a.pick.open = true
	if got := a.hintWord(); got != "enter switch · esc" {
		t.Fatalf("an open picker offered %q", got)
	}
	a.pick.open, a.copy.on = false, false

	a.menu.open = true
	if got := a.hintWord(); got != "↑↓ · enter · esc" {
		t.Fatalf("the command list offered %q", got)
	}
	a.menu.open = false

	// A path completing under a command argument keeps enter for the LINE, and
	// tab is the key that takes the row (input.go).
	a.comp.open, a.comp.arg = true, true
	if got := a.hintWord(); got != "tab take · enter run · esc" {
		t.Fatalf("an argument completion offered %q", got)
	}
	a.comp.arg = false
	if got := a.hintWord(); got != "↑↓ · enter · esc" {
		t.Fatalf("the @ completion offered %q", got)
	}
	a.comp.open = false

	// And a state with no keys of its own gives the slot back to the input's own
	// affordances.
	if got := a.hintWord(); got != "" {
		t.Fatalf("an idle surface offered %q", got)
	}
	if !strings.Contains(plain(a.legend(120)), microcopy) {
		t.Fatal("an idle legend lost the input's own affordances")
	}
}
