package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestOneModelVerificationProfileHonorsExactCapableVisionPin(t *testing.T) {
	mediaEnvOff(t)
	const selected = "deepseek/deepseek-v4.1-flash"
	// Mirrors the model's published input/output modalities, not a guessed
	// text-only capability from an older Flash generation.
	models := mediaCatalogFor(t, `{"id":"other/vision-model","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},{"id":"deepseek/deepseek-v4.1-flash","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}`)
	profile := oneModelProfile(t)
	if err := writeVisionRow(profile, selected); err != nil {
		t.Fatal(err)
	}
	cfg, err := applyV3Governance(session.Config{Model: selected}, profile, false, true)
	if err != nil {
		t.Fatal(err)
	}
	resolve := v3MediaModel(models, profile, cfg.RolesSource, cfg.OneModel)
	if got := resolve("vision"); got != selected {
		t.Fatalf("explicit vision pin changed to %q despite advertised image input", got)
	}
}

func TestOneModelExplicitUnsupportedMediaPinsAreUnavailable(t *testing.T) {
	mediaEnvOff(t)
	const selected = "deepseek/deepseek-v4.1-flash"
	models := mediaCatalogFor(t, mediaFixture+`,{"id":"deepseek/deepseek-v4.1-flash","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}`)
	profile := oneModelProfile(t)
	for _, slot := range []string{"image", "speech", "music", "video", "voice"} {
		writeMediaSlot(t, profile, slot, selected)
	}
	if err := writeVisionRow(profile, selected); err != nil {
		t.Fatal(err)
	}
	resolve := v3MediaModel(models, profile, nil, true)
	if got := resolve("vision"); got != selected {
		t.Fatalf("capable vision pin unavailable: %q", got)
	}
	for _, modality := range []string{"image", "speech", "music", "video", "transcribe"} {
		if got := resolve(modality); got != "" {
			t.Errorf("unsupported explicit %s pin silently replaced by %q", modality, got)
		}
	}
	names := session.HarnessBeltNames(t.TempDir(), session.HarnessBeltSeams{Media: &provider.MediaClient{}, MediaModel: resolve, Seer: &provider.Client{}})
	for _, tool := range []string{"generate_image", "speak", "generate_music", "generate_video"} {
		if names[tool] {
			t.Errorf("unsupported pinned operation still advertises %s", tool)
		}
	}
	if !names["view_image"] {
		t.Fatal("image-capable exact pin removed view_image")
	}
	// An unpinned perception modality preserves the normal capability ladder.
	if got := resolve("listen"); got == "" {
		t.Fatal("unconfigured media was disabled by the narrow pin rule")
	}
	// Without --one-model an incapable image pin keeps its previous fallback.
	legacy := v3MediaModel(models, profile, nil)
	if got := legacy("image"); got == "" || got == selected {
		t.Fatalf("ordinary media fallback changed: %q", got)
	}
}
