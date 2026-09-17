package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
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
		// A TAIL LEAVES `text` OUT — it carries what tells rows apart, and a
		// plain chat model has nothing to say here. The COLUMN puts it in,
		// because a cell has to be true of every row under its head
		// ([modalitySay]'s withText, and [modelFields] beside it).
		{Model{ID: "vendor/plain", Input: []string{"text"}, Output: []string{"text"}}, ""},
		{Model{ID: "vendor/quiet"}, ""},
		{Model{ID: "vendor/seer", Input: []string{"text", "image"}, Output: []string{"text"}}, "inputs image"},
		{Model{ID: "vendor/painter", Input: []string{"text"}, Output: []string{"image"}}, "outputs image"},
		{Model{ID: "vendor/tts", Input: []string{"text"}, Output: []string{"speech"}}, "outputs speech"},
		{Model{ID: "vendor/film", Input: []string{"text"}, Output: []string{"video"}}, "outputs video"},
		{Model{ID: "vendor/ear", Input: []string{"audio"}, Output: []string{"text"}}, "inputs audio"},
		{Model{ID: "vendor/watcher", Input: []string{"text", "video"}, Output: []string{"text"}}, "inputs video"},
		// Input before output, and both when both are true.
		{Model{ID: "vendor/omni", Input: []string{"text", "image"}, Output: []string{"image", "text"}}, "inputs image · outputs image"},
		// THE CATALOG'S OWN WORD, WHICH IS WHY THE FOLD IS GONE. Synthesized
		// sound is filed as `speech`, `audio` or `music` depending on the
		// family, and the old vocabulary called all three `speaks` — so a model
		// that writes songs and a model that reads a paragraph aloud came out
		// of this function identically. They do not now.
		{Model{ID: "vendor/song", Input: []string{"text"}, Output: []string{"music"}}, "outputs music"},
		{Model{ID: "vendor/audio", Input: []string{"text"}, Output: []string{"audio"}}, "outputs audio"},
		// THE ORDER IS OURS AND NOT THE PUBLISHED ORDER, because the catalog
		// has none: it spells the same set `text, image, file`, `file, image,
		// text` and `image, text, file` on neighbouring rows, and a column that
		// echoed that would put one fact in three places.
		{Model{ID: "vendor/jumbled", Input: []string{"file", "video", "text", "image", "audio"}}, "inputs image audio video file"},
		{Model{ID: "vendor/sorted", Input: []string{"text", "image", "audio", "video", "file"}}, "inputs image audio video file"},
		// A WORD THIS BUILD HAS NEVER HEARD OF IS STILL SAID, after the ones it
		// knows. `embeddings`, `transcription` and `rerank` are in today's
		// catalog and tomorrow's will carry something else; a surface that drew
		// only its own vocabulary would answer "text in, text out" for a whole
		// family it simply did not recognise.
		{Model{ID: "vendor/scribe", Input: []string{"audio"}, Output: []string{"transcription"}}, "inputs audio · outputs transcription"},
		{Model{ID: "vendor/odd", Input: []string{"text"}, Output: []string{"rerank", "image"}}, "outputs image rerank"},
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
	if note != "128k · inputs image" {
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
	if !strings.Contains(last, "cannot hold a conversation") || !strings.Contains(last, "it answers with speech") {
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
