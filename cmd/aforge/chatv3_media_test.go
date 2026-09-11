package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// docs/MULTIMODAL.md's v3 revision, Decision 5: ONE KNOB PER MODALITY, resolved
// at use time down one documented ladder, with EVERY RUNG CAPABILITY-CHECKED.

type mediaRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn mediaRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// mediaCatalogFor is a catalog with no network and no cache file behind it: the
// rows are the fixture, in the fixture's order.
func mediaCatalogFor(t *testing.T, rows string) *catalog.Catalog {
	t.Helper()
	client := &http.Client{Transport: mediaRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":[` + rows + `]}`)), Request: request}, nil
	})}
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
}

// mediaFixture is one row of every family, published the way OpenRouter
// publishes them.
const mediaFixture = `
	{"id":"anthropic/claude-sonnet-4.5","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
	{"id":"vendor/first-painter","architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
	{"id":"vendor/second-painter","architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
	{"id":"krea/krea-2-medium-turbo","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]}},
	{"id":"vendor/first-voice","architecture":{"input_modalities":["text"],"output_modalities":["speech"]}},
	{"id":"vendor/first-film","architecture":{"input_modalities":["text"],"output_modalities":["video"]}},
	{"id":"vendor/ear","architecture":{"input_modalities":["audio"],"output_modalities":["text"]}},
	{"id":"google/gemini-2.5-flash","architecture":{"input_modalities":["text","image","audio","video"],"output_modalities":["text"]}},
	{"id":"moonshotai/kimi-k3","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`

// writeMediaSlot writes one capability slot the way the settings sheet does —
// through the registry's own row, never by hand — so the test cannot pass on a
// key the sheet does not actually write.
func writeMediaSlot(t *testing.T, profileDir, slot, slug string) {
	t.Helper()
	row, found := config.NewSettings(config.SettingsOptions{ProfileDir: profileDir}).
		Row(config.ModelSettingKey(slot))
	if !found {
		t.Fatalf("there is no %q settings row", slot)
	}
	if err := row.Apply(slug); err != nil {
		t.Fatalf("the %q row refused %q: %v", slot, slug, err)
	}
}

func mediaEnvOff(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AFORGE_VISION_MODEL", "AFORGE_IMAGE_MODEL", "AFORGE_SPEECH_MODEL",
		"AFORGE_MUSIC_MODEL", "AFORGE_VIDEO_MODEL", "AFORGE_VOICE_MODEL",
	} {
		t.Setenv(name, "")
	}
}

// THE LADDER, top to bottom: the slot beats the pin beats the catalog beats the
// curated name, and each rung is only taken when the catalog says the model it
// names can actually do the job.
func TestTheMediaResolverWalksItsLadder(t *testing.T) {
	mediaEnvOff(t)
	profile := t.TempDir()
	models := mediaCatalogFor(t, mediaFixture)
	pins := map[string]string{}
	resolve := v3MediaModel(models, profile, func(key string) (string, bool) {
		value, ok := pins[key]
		return value, ok
	})

	// RUNG 3, the catalog: nothing is set, and the ladder still answers with a
	// model that publishes the capability. This is what makes a machine that
	// has never opened settings draw, speak and film out of the box.
	//
	// Note which image model wins: the documented preference order beats the
	// catalog's own order, so krea leads two painters that were listed first.
	for _, test := range []struct{ modality, want string }{
		{"image", "krea/krea-2-medium-turbo"},
		{"speech", "vendor/first-voice"},
		{"video", "vendor/first-film"},
		{"vision", "anthropic/claude-sonnet-4.5"},
		{"transcribe", "vendor/ear"},
		{"listen", "google/gemini-2.5-flash"},
		{"watch", "google/gemini-2.5-flash"},
	} {
		if got := resolve(test.modality); got != test.want {
			t.Fatalf("%s resolved to %q with nothing set, want the catalog's %q",
				test.modality, got, test.want)
		}
	}

	// RUNG 2, the pin: an operator's roles.imagegen beats the catalog.
	pins[roles.PinKey(roles.RoleImageGen)] = "vendor/first-painter"
	if got := resolve("image"); got != "vendor/first-painter" {
		t.Fatalf("the imagegen pin resolved to %q", got)
	}

	// RUNG 1, the slot: the settings row beats the pin, and it is read AT CALL
	// TIME — this write happens after the closure was built.
	writeMediaSlot(t, profile, "image", "vendor/second-painter")
	if got := resolve("image"); got != "vendor/second-painter" {
		t.Fatalf("the drawing slot resolved to %q — the slot must beat the pin", got)
	}

	// And the LOOKING row is the vision slot under its own name, which is how
	// the double knob dies: one row, one reader.
	if err := writeVisionRow(profile, "google/gemini-2.5-flash"); err != nil {
		t.Fatal(err)
	}
	if got := resolve("vision"); got != "google/gemini-2.5-flash" {
		t.Fatalf("the looking row resolved to %q", got)
	}

	// A word the resolver does not answer to is "" and not a guess.
	if got := resolve("interpretive dance"); got != "" {
		t.Fatalf("an unknown modality resolved to %q", got)
	}
}

func writeVisionRow(profileDir, slug string) error {
	row, found := config.NewSettings(config.SettingsOptions{ProfileDir: profileDir}).
		Row(config.KeyVisionModel)
	if !found {
		return errNoLookingRow
	}
	return row.Apply(slug)
}

var errNoLookingRow = errors.New("there is no looking row")

// AN INCAPABLE RUNG IS PASSED OVER rather than sent to a provider to fail. A
// slug that was renamed, or a pin somebody copied out of the wrong list,
// degrades to the best available model instead of a 404.
func TestTheMediaResolverPassesOverAnIncapableRung(t *testing.T) {
	mediaEnvOff(t)
	profile := t.TempDir()
	models := mediaCatalogFor(t, mediaFixture)

	// The slot names a chat model, which cannot draw; the pin names a speech
	// model, which cannot draw either. Both are passed over.
	writeMediaSlot(t, profile, "image", "moonshotai/kimi-k3")
	pins := map[string]string{roles.PinKey(roles.RoleImageGen): "vendor/first-voice"}
	resolve := v3MediaModel(models, profile, func(key string) (string, bool) {
		value, ok := pins[key]
		return value, ok
	})
	if got := resolve("image"); got != "krea/krea-2-medium-turbo" {
		t.Fatalf("an incapable slot and pin resolved to %q, want the catalog's answer", got)
	}

	// A slug the catalog has never carried is incapable on the same terms:
	// nobody can vouch for it, and this door only says yes to published facts.
	pins[roles.PinKey(roles.RoleVideo)] = "vendor/renamed-last-year"
	if got := resolve("video"); got != "vendor/first-film" {
		t.Fatalf("a pin naming an unknown slug resolved to %q", got)
	}

	// And the perception words refuse a model that answers in pictures rather
	// than in words: eyes that reply with a painting cannot say what they saw.
	pins[roles.PinKey(roles.RoleVision)] = "krea/krea-2-medium-turbo"
	if got := resolve("vision"); got != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("a painter was accepted as a pair of eyes: %q", got)
	}
}

// A CATALOG THAT REACHED NOTHING still draws, speaks and films. Its offline
// rows ARE the curated names, so the capability check the last rung is held to
// passes on facts rather than on a build's memory of them.
func TestTheMediaResolverStillAnswersOnAnOfflineCatalog(t *testing.T) {
	mediaEnvOff(t)
	offline := &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, http.ErrServerClosed
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: catalog.DefaultBaseURL, Dir: t.TempDir(), HTTPClient: offline,
	})
	resolve := v3MediaModel(models, t.TempDir(), nil)

	for _, test := range []struct{ modality, want string }{
		{"image", "bytedance-seed/seedream-5-0-pro"},
		{"speech", "fish-audio/s2.1-pro"},
		// COMPOSING IS ITS OWN WORD and answers on a cold machine like the
		// other three: the offline catalog publishes Lyria with a "music"
		// output, so the curated rung is capability-checked and taken rather
		// than passed over.
		{"music", "google/lyria-3-clip-preview"},
		{"video", "bytedance/seedance-2.5"},
	} {
		if got := resolve(test.modality); got != test.want {
			t.Fatalf("%s on an offline catalog resolved to %q, want %q", test.modality, got, test.want)
		}
	}
	// And the perception words answer NOTHING rather than a name nobody can
	// vouch for — no offline row publishes image, audio or video input with
	// words back. Nothing is what keeps the verb off the belt entirely.
	for _, modality := range []string{"vision", "listen", "watch"} {
		if got := resolve(modality); got != "" {
			t.Fatalf("%s on an offline catalog invented %q", modality, got)
		}
	}
}

// AND A CATALOG THAT ANSWERED, WITHOUT A CAPABLE ROW IN IT, INVENTS NOTHING.
// The curated last rung is capability-checked like every other, so a build's
// remembered name is not smuggled past a catalog that has heard of no such
// model — the modality's tools are simply absent (session.Config's own law).
func TestTheMediaResolverInventsNothingOnACatalogWithNoCapableRow(t *testing.T) {
	mediaEnvOff(t)
	models := mediaCatalogFor(t, `
		{"id":"moonshotai/kimi-k3","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	resolve := v3MediaModel(models, t.TempDir(), nil)
	for _, modality := range []string{"image", "speech", "music", "video", "vision", "transcribe", "listen", "watch"} {
		if got := resolve(modality); got != "" {
			t.Fatalf("%s resolved to %q on a catalog with no capable row", modality, got)
		}
	}
}

// SPEECH AND MUSIC ARE TWO QUESTIONS, and a catalog that can answer one must not
// be read as answering the other.
//
// They ride one endpoint's worth of vocabulary — internal/catalog aliases the
// provider's broad "audio" onto both words — so the tempting shortcut is to let
// a TTS row serve the composing slot. It cannot: a text-to-speech model asked
// for a piece of music pronounces the brief. A row that publishes only "speech"
// therefore answers the speaking slot and leaves the composing verb OFF the
// belt, which is the honest absence.
func TestASpeechModelDoesNotAnswerTheComposingSlot(t *testing.T) {
	mediaEnvOff(t)
	models := mediaCatalogFor(t, `
		{"id":"vendor/only-a-voice","architecture":{"input_modalities":["text"],"output_modalities":["speech"]}}`)
	resolve := v3MediaModel(models, t.TempDir(), nil)
	if got := resolve("speech"); got != "vendor/only-a-voice" {
		t.Fatalf("the speaking slot resolved to %q, want the TTS row", got)
	}
	if got := resolve("music"); got != "" {
		t.Fatalf("the composing slot resolved to %q on a catalog with only a TTS row", got)
	}
}

// THE ENVIRONMENT STILL WINS over the persisted row, because that is the order
// every other knob in this product resolves in.
func TestTheMediaSlotReadsTheEnvironmentFirst(t *testing.T) {
	mediaEnvOff(t)
	profile := t.TempDir()
	models := mediaCatalogFor(t, mediaFixture)
	writeMediaSlot(t, profile, "image", "vendor/first-painter")
	t.Setenv("AFORGE_IMAGE_MODEL", "krea/krea-2-medium-turbo")
	if got := v3MediaModel(models, profile, nil)("image"); got != "krea/krea-2-medium-turbo" {
		t.Fatalf("the drawing slot resolved to %q with the environment set", got)
	}
}

// THE MEDIA SLOT PICKER IS FED END TO END: the catalog goes through the door's
// own list-builder, and what comes out the far side is a list the drawing slot's
// question can actually be answered from. The door used to drop every one of
// these rows.
func TestTheDoorCarriesTheRowsTheMediaSlotsNeed(t *testing.T) {
	models := mediaCatalogFor(t, mediaFixture)
	rows := v3Models(models)
	if len(rows) == 0 {
		t.Fatal("the door handed the surface nothing")
	}
	for _, test := range []struct {
		slot string
		want string
	}{
		{"image", "vendor/first-painter"},
		{"speech", "vendor/first-voice"},
		{"video", "vendor/first-film"},
		{"voice", "vendor/ear"},
	} {
		found := false
		for _, row := range tui3.KeepForSlot(rows, config.ModelSettingKey(test.slot)) {
			if row.ID == test.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("the %q slot's list, drawn from the door's own rows, has no %q", test.slot, test.want)
		}
	}
	// And the chat law still answers the chat question over the same rows.
	for _, row := range tui3.ChatModels(rows) {
		if row.ID == "vendor/first-painter" {
			t.Fatal("a drawing model reached the conversation list")
		}
	}
}
