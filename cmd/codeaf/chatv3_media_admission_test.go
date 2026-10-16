package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestV3MediaAdmissionNeverSubstitutesForAnInvalidExplicitChoice(t *testing.T) {
	mediaEnvOff(t)
	profile := t.TempDir()
	models := mediaCatalogFor(t, mediaFixture)
	source := func(string) (string, bool) { return "", false }
	resolve := v3MediaAdmission(models, profile, source)
	writeMediaSlot(t, profile, "image", "vendor/first-voice")
	got := resolve("image", "")
	if got.Model != "" || got.Reason != "configured_model_invalid" || !strings.Contains(got.Message, "vendor/first-voice") {
		t.Fatalf("silently substituted a painter: %+v", got)
	}
	writeMediaSlot(t, profile, "image", "operator/explicit-not-listed")
	if got = resolve("image", ""); got.Model != "operator/explicit-not-listed" {
		t.Fatalf("listing overruled explicit route: %+v", got)
	}
}

func TestV3MediaAdmissionNoDefaultExplainsChoicesWithoutPickingTheFirst(t *testing.T) {
	mediaEnvOff(t)
	models := mediaCatalogFor(t, `{"id":"new/painter","architecture":{"input_modalities":["text"],"output_modalities":["image"]}}`)
	got := v3MediaAdmission(models, t.TempDir(), func(string) (string, bool) { return "", false })("image", "")
	if got.Model != "" || got.Reason == "" || got.Message == "" {
		t.Fatalf("picked an arbitrary route or lost refusal: %+v", got)
	}
}

func TestV3MediaAdmissionKeepsExplicitRoleChoiceAndActualCallChoice(t *testing.T) {
	mediaEnvOff(t)
	models := mediaCatalogFor(t, mediaFixture)
	source := func(key string) (string, bool) {
		return "vendor/first-painter", key == roles.PinKey(roles.RoleImageGen)
	}
	resolve := v3MediaAdmission(models, t.TempDir(), source)
	if got := resolve("image", ""); got.Model != "vendor/first-painter" {
		t.Fatalf("lost explicit pin: %+v", got)
	}
	if got := resolve("image", "vendor/second-painter"); got.Model != "vendor/second-painter" {
		t.Fatalf("lost actual invocation choice: %+v", got)
	}
}
