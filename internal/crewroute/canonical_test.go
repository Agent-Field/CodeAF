package crewroute

import (
	"strings"
	"testing"
)

// ONE MODEL, EVERY SPELLING: OpenRouter, Fireworks, a Hugging Face mirror,
// Together, a free pool, a dated snapshot and a local quantised pull of the
// same weights all read as the model the evidence was measured under — the
// quantised pull as a distinct variant of it.
func TestCanonicalReadsEveryProvidersSpellingOfOneModel(t *testing.T) {
	cases := []struct {
		id, want, variant string
	}{
		{"z-ai/glm-5.3-flash", "z-ai/glm-5.3-flash", ""},
		{"openrouter/z-ai/glm-5.3-flash", "z-ai/glm-5.3-flash", ""},
		{"accounts/fireworks/models/glm-5p3-flash", "z-ai/glm-5.3-flash", ""},
		{"zai-org/GLM-5.3-Flash", "z-ai/glm-5.3-flash", ""},
		{"z-ai/glm_5.3_flash", "z-ai/glm-5.3-flash", ""},
		{"z-ai/glm-5.3-flash:free", "z-ai/glm-5.3-flash", ""},
		{"z-ai/glm-5.3-flash:nitro", "z-ai/glm-5.3-flash", ""},
		{"z-ai/glm-5.3-flash-20260301", "z-ai/glm-5.3-flash", ""},
		{"~z-ai/glm-5.3-flash-latest", "z-ai/glm-5.3-flash", ""},
		{"z-ai/glm-5.3-flash:high", "z-ai/glm-5.3-flash", ""},
		{"glm-5.3-flash:q4_k_m", "z-ai/glm-5.3-flash", "q4_k_m"},
		{"deepseek-ai/DeepSeek-V4-Flash", "deepseek/deepseek-v4-flash", ""},
		{"accounts/fireworks/models/kimi-k3", "moonshotai/kimi-k3", ""},
		{"meta-llama/Llama-3.3-70B-Instruct-Turbo", "meta-llama/llama-3.3-70b-instruct", ""},
		{"moonshotai/kimi-k3-fp8", "moonshotai/kimi-k3", "fp8"},
	}
	for _, tc := range cases {
		got := CanonicalOf(tc.id)
		if got.ID != tc.want || got.Variant != tc.variant {
			t.Errorf("CanonicalOf(%q) = %+v, want %q variant %q", tc.id, got, tc.want, tc.variant)
		}
	}
}

// AN UNKNOWN SPELLING STAYS ITS OWN MODEL: nothing is merged on a likeness.
func TestCanonicalNeverMergesOnALikeness(t *testing.T) {
	for _, pair := range [][2]string{
		{"z-ai/glm-5.3-flash", "z-ai/glm-5.3"},
		{"somelab/coder-7b", "otherlab/coder-7b"},
		{"thinkingmachines/inkling-small", "thinkingmachines/inkling"},
	} {
		if Canonical(pair[0]) == Canonical(pair[1]) {
			t.Errorf("%q and %q were read as one model", pair[0], pair[1])
		}
	}
	if got := Canonical("somelab/mystery-model"); got != "somelab/mystery-model" {
		t.Errorf("an unknown id was rewritten to %q", got)
	}
}

// A QUANTISED COPY IS NOT ITS MODEL, but borrows its evidence at a discount
// and reads as unmeasured.
func TestAQuantisedCopyInheritsEvidenceAtADiscount(t *testing.T) {
	tab := prior()
	base, _ := Snapshot("z-ai/glm-5.3-flash")
	local := base
	local.ID = "glm-5.3-flash:q4_k_m"
	if Lineage(local.ID) == Lineage(base.ID) {
		t.Fatalf("the quantised copy shares the model's identity: %q", Lineage(local.ID))
	}
	full, measured := tab.quality(Bugfix, Worker, base)
	got, localMeasured := tab.quality(Bugfix, Worker, local)
	if !measured || localMeasured {
		t.Fatalf("measured flags: model %v, copy %v", measured, localMeasured)
	}
	if want := full * quantDiscount; got != want || !strings.Contains(Lineage(local.ID), "@q4_k_m") {
		t.Fatalf("the copy reads %v (lineage %q), want %v", got, Lineage(local.ID), want)
	}
}

// A MODEL'S NAME SAYS WHEN IT WAS TUNED FOR ONE DOMAIN, and a general or a
// code model's does not.
func TestDomainTunedModelsAreToldByTheirNames(t *testing.T) {
	for _, id := range []string{"inclusionai/ling-3.0-flash-fin:free", "vendor/med-llama-70b", "acme/legal-7b", "x/qwen-math-72b", "y/roleplay-13b:free"} {
		if !DomainTuned(id) {
			t.Errorf("%s reads as general", id)
		}
	}
	for _, id := range []string{"poolside/laguna-s-2.1:free", "z-ai/glm-5.3-flash", "qwen/qwen3-coder:free", "google/gemma-4-31b-it:free", "cohere/north-mini-code:free"} {
		if DomainTuned(id) {
			t.Errorf("%s reads as domain-tuned", id)
		}
	}
}
