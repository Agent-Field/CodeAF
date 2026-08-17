package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// docs/MULTIMODAL.md's v3 revision, Decisions 5 and 6, on this surface:
//
//   - the media slots' writes are ACCEPTED and land in the profile, where the
//     use-time resolver reads them;
//   - the silence law is ONE law — an unpublished modality list means
//     text-in/text-out and nothing more;
//   - a filtered list is explicable, because the rows say what they can do;
//   - /model <slug> warns instead of putting a music model in the status line.

// A SLOT WRITE LANDS IN THE PROFILE, and it lands under the key the resolver
// reads. This is the whole of the one-knob change on this side: the pick is not
// a preference the surface keeps, it is the value the next picture comes out of.
func TestAMediaSlotPickWritesTheKeyTheResolverReads(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mediaCatalog }

	for _, test := range []struct {
		slot string
		want string
	}{
		{"image", "google/gemini-3.1-flash-image"},
		{"speech", "openai/gpt-4o-mini-tts"},
		{"video", "bytedance/seedance-1-5-pro"},
		{"voice", "openai/whisper-large-v3"},
	} {
		row, found := a.registry().Row(config.ModelSettingKey(test.slot))
		if !found {
			t.Fatalf("the %q slot has no settings row", test.slot)
		}
		// An untouched slot reads as the word that says what will happen, not
		// as a blank a reader has to guess at.
		if row.Value() != "automatic" {
			t.Fatalf("the untouched %q slot reads %q", test.slot, row.Value())
		}
		if err := row.Apply(test.want); err != nil {
			t.Fatalf("the %q slot refused a pick from its own list: %v", test.slot, err)
		}
		if got := config.MediaSlotModelAt(a.profileDir, test.slot); got != test.want {
			t.Fatalf("the profile holds %q for the %q slot, want %q", got, test.slot, test.want)
		}
	}

	// AND THE ROLE SLOTS STILL REFUSE, because they genuinely belong to the
	// session that opens them — that is the only refusal left.
	role, found := a.registry().Row(config.ModelSettingKey("plan"))
	if !found {
		t.Fatal("the planning slot has no settings row")
	}
	if err := role.Apply("vendor/planner"); err == nil ||
		!strings.Contains(err.Error(), "chosen where its session is opened") {
		t.Fatalf("the planning slot answered %v", err)
	}
}

// THE LOOKING ROW WRITES THE KEY THE RESOLVER READS, which is what kills the
// double knob: the row and the resolver used to be two names for one question
// and disagreed on a fresh profile.
func TestTheLookingRowWritesTheVisionKey(t *testing.T) {
	a, _ := sheetApp(t)
	row, found := a.registry().Row(config.KeyVisionModel)
	if !found {
		t.Fatal("there is no looking row")
	}
	if err := row.Apply("anthropic/claude-sonnet-4.5"); err != nil {
		t.Fatalf("the looking row refused: %v", err)
	}
	if got := config.VisionModelAt(a.profileDir); got != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the profile holds %q for looking", got)
	}
}

// THE MODALITY TAIL, and the emptiness law that governs it: a plain text chat
// model — the overwhelming majority of every list — says NOTHING NEW, and a row
// that published nothing says nothing either, because silence IS text in and
// text out.
func TestTheRowSaysWhatTheModelCanDo(t *testing.T) {
	for _, test := range []struct {
		model Model
		want  string
	}{
		{Model{ID: "vendor/plain", Input: []string{"text"}, Output: []string{"text"}}, ""},
		{Model{ID: "vendor/quiet"}, ""},
		{Model{ID: "vendor/seer", Input: []string{"text", "image"}, Output: []string{"text"}}, "sees"},
		{Model{ID: "vendor/painter", Input: []string{"text"}, Output: []string{"image"}}, "draws"},
		{Model{ID: "vendor/tts", Input: []string{"text"}, Output: []string{"speech"}}, "speaks"},
		{Model{ID: "vendor/film", Input: []string{"text"}, Output: []string{"video"}}, "films"},
		{Model{ID: "vendor/ear", Input: []string{"audio"}, Output: []string{"text"}}, "hears"},
		{Model{ID: "vendor/watcher", Input: []string{"text", "video"}, Output: []string{"text"}}, "watches"},
		// Input before output, and both when both are true.
		{Model{ID: "vendor/omni", Input: []string{"text", "image"}, Output: []string{"image", "text"}}, "sees · draws"},
		// The catalog files synthesized sound as audio or as music depending on
		// the family; both are one word here.
		{Model{ID: "vendor/song", Input: []string{"text"}, Output: []string{"music"}}, "speaks"},
	} {
		if got := ModalityWord(test.model.Input, test.model.Output); got != test.want {
			t.Fatalf("ModalityWord(%q, in=%v out=%v) = %q, want %q",
				test.model.ID, test.model.Input, test.model.Output, got, test.want)
		}
	}

	// And on the row itself it is the last part of the dim tail, after the
	// facts the catalog published about size and price.
	note := modelNote(Model{
		ID: "vendor/seer", ContextLength: 128_000,
		Input: []string{"text", "image"}, Output: []string{"text"},
	})
	if note != "128k · sees" {
		t.Fatalf("the row's note reads %q", note)
	}
	if plain := modelNote(Model{ID: "vendor/plain", ContextLength: 128_000, Output: []string{"text"}}); plain != "128k" {
		t.Fatalf("a plain chat row grew a tail: %q", plain)
	}
}

// /model <slug> ON A MODEL THAT CANNOT TALK warns and changes nothing. The
// catalog carries the whole list now, so "this one answers in mp3" is a
// published fact this surface can check before it accepts a name.
func TestModelBySlugWarnsOnAModelThatCannotTalk(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "moonshotai/kimi-k3"}, mediaCatalog)
	typeLine(t, a, "/model openai/gpt-4o-mini-tts")

	if a.model != "moonshotai/kimi-k3" {
		t.Fatalf("the conversation moved to %q", a.model)
	}
	last := a.entries[len(a.entries)-1].text
	if !strings.Contains(last, "cannot hold a conversation") || !strings.Contains(last, "speaks") {
		t.Fatalf("the warning reads %q", last)
	}

	// A CHAT MODEL IS TAKEN, and so is a slug no list here carries — the
	// offline law: a surface that refused every unfamiliar name would stop
	// working the moment the catalog did.
	typeLine(t, a, "/model anthropic/claude-sonnet-4.5")
	if a.model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("a chat slug was refused; the model is %q", a.model)
	}
	typeLine(t, a, "/model vendor/never-listed")
	if a.model != "vendor/never-listed" {
		t.Fatalf("an unknown slug was refused; the model is %q", a.model)
	}
}
