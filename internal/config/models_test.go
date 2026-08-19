package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
)

// wordCandidateLimit mirrors the head's ambiguity cap. Config must not import
// the conversational layer to know how many options a question may carry.
const wordCandidateLimit = 4

func wordCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	return runtimeCatalog(t, `
		{"id":"google/gemini-3-pro","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"google/gemini-3-flash","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"anthropic/claude-opus-5","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"~deepseek/deepseek-v4-flash-latest","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"krea/krea-2-medium-turbo","architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
		{"id":"hexgrad/kokoro-82m","architecture":{"input_modalities":["text"],"output_modalities":["speech"]}}`)
}

func TestModelMatchesResolvesExactPrefixAndAmbiguousWords(t *testing.T) {
	models := wordCatalog(t)
	for _, test := range []struct {
		word string
		want []string
	}{
		{word: "google/gemini-3-pro", want: []string{"google/gemini-3-pro"}},
		{word: "gemini-3-pro", want: []string{"google/gemini-3-pro"}},
		{word: "gemini", want: []string{"google/gemini-3-pro", "google/gemini-3-flash"}},
		{word: "opus", want: []string{"anthropic/claude-opus-5"}},
		{word: "deepseek-v4-flash-latest", want: []string{"~deepseek/deepseek-v4-flash-latest"}},
		{word: "krea", want: nil},
		{word: "nothing-like-this", want: nil},
	} {
		t.Run(test.word, func(t *testing.T) {
			got := ModelMatches(models, "work", test.word, wordCandidateLimit)
			if len(got) == 0 && len(test.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("matches = %v, want %v", got, test.want)
			}
		})
	}
	if got := ModelMatches(nil, "work", "gemini", 4); got != nil {
		t.Fatalf("nil catalog matched %v", got)
	}
}

func TestBestMediaModelFollowsThePreferenceOrderThenPrice(t *testing.T) {
	preferred := runtimeCatalog(t, `
		{"id":"cheap/image","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.01"}},
		{"id":"krea/krea-2-medium-turbo","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.02"}},
		{"id":"openai/gpt-image-1.5","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.09"}},
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["speech"]}},
		{"id":"google/lyria-3-clip-preview","architecture":{"output_modalities":["music"]},"pricing":{"request":"0.04"}},
		{"id":"bytedance/seedance-1-5-pro","architecture":{"output_modalities":["video"]},"pricing":{"request":"0.5"}}`)
	for modality, want := range map[string]string{
		// gpt-image-1.5 outranks krea in the documented image order. None of
		// the three leaders is advertised here, so each modality falls to the
		// rung under it: the paid TTS ahead of the cheap Kokoro, and the older
		// Lyria and Seedance rows.
		"image":  "openai/gpt-image-1.5",
		"speech": "openai/gpt-4o-mini-tts",
		"music":  "google/lyria-3-clip-preview",
		"video":  "bytedance/seedance-1-5-pro",
	} {
		if got := BestMediaModel(preferred, modality); got != want {
			t.Fatalf("best %s = %q, want %q", modality, got, want)
		}
	}

	// The three leaders, advertised alongside everything above and each one
	// cheaper than the row it must beat, so only the preference order can
	// explain the answer.
	leaders := runtimeCatalog(t, `
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["speech"]}},
		{"id":"fish-audio/s1","architecture":{"output_modalities":["speech"]}},
		{"id":"google/lyria-3-clip-preview","architecture":{"output_modalities":["music"]},"pricing":{"request":"0.04"}},
		{"id":"google/lyria-3-pro-preview","architecture":{"output_modalities":["music"]},"pricing":{"request":"0.01"}},
		{"id":"bytedance/seedance-1-5-pro","architecture":{"output_modalities":["video"]},"pricing":{"request":"0.5"}},
		{"id":"bytedance/seedance-2.0-mini","architecture":{"output_modalities":["video"]},"pricing":{"request":"0.05"}}`)
	for modality, want := range map[string]string{
		"speech": "fish-audio/s1",
		"music":  "google/lyria-3-pro-preview",
		"video":  "bytedance/seedance-2.0-mini",
	} {
		if got := BestMediaModel(leaders, modality); got != want {
			t.Fatalf("best %s = %q, want %q", modality, got, want)
		}
	}

	// Nothing from the preference list advertised: price is the only quality
	// signal a catalog row carries, so the dearest wins.
	unknown := runtimeCatalog(t, `
		{"id":"studio/plain","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.02"}},
		{"id":"studio/grand","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.30"}}`)
	if got := BestMediaModel(unknown, "image"); got != "studio/grand" {
		t.Fatalf("price fallback = %q, want studio/grand", got)
	}
	if got := BestMediaModel(unknown, "video"); got != "" {
		t.Fatalf("unadvertised modality invented %q", got)
	}
}

func TestResolveMediaModelReadsAllThreeSpellingsAndRefusesWrongModality(t *testing.T) {
	models := runtimeCatalog(t, `
		{"id":"krea/krea-2-medium-turbo","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.02"}},
		{"id":"openai/gpt-image-1.5","architecture":{"output_modalities":["image"]},"pricing":{"request":"0.09"}},
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}}`)

	if model, err := ResolveMediaModel(models, "image", ""); model != "" || err != nil {
		t.Fatalf("absent = %q err=%v, want the caller's slot default", model, err)
	}
	if model, err := ResolveMediaModel(models, "image", "best"); model != "openai/gpt-image-1.5" || err != nil {
		t.Fatalf("best = %q err=%v", model, err)
	}
	if model, err := ResolveMediaModel(models, "image", "krea"); model != "krea/krea-2-medium-turbo" || err != nil {
		t.Fatalf("fuzzy name = %q err=%v", model, err)
	}
	_, err := ResolveMediaModel(models, "image", "kokoro")
	if err == nil || !strings.Contains(err.Error(), "speech") || !strings.Contains(err.Error(), "image") {
		t.Fatalf("wrong-modality refusal = %v", err)
	}
	_, err = ResolveMediaModel(models, "image", "nothing-like-this")
	if err == nil || !strings.Contains(err.Error(), "no image model matches") {
		t.Fatalf("unknown name refusal = %v", err)
	}
	_, err = ResolveMediaModel(models, "video", "best")
	if err == nil || !strings.Contains(err.Error(), "no video model is advertised") {
		t.Fatalf("unadvertised best refusal = %v", err)
	}
}

// "use the net column" once produced a four-way Claude menu ("net" inside
// "sonnet"), and the official GAIA template's "comma separated list" produced
// a four-way Cohere one ("comma" leading "command"). Letters are not names:
// a person's word matches whole tokens or not at all, at both ends.
func TestAModelWordMatchesWholeTokensOrNotAtAll(t *testing.T) {
	for word, want := range map[string]bool{
		"net":       false, // son|net — the f4 failure
		"comma":     false, // comma|nd — the GAIA failure
		"kimi":      true,  // moonshotai/kimi-k2
		"sonnet":    true,
		"command":   true,
		"command-r": true,
		"onnet":     false,
		"comm":      false,
	} {
		got := tokensAlignInside("moonshotai/kimi-k2", word) ||
			tokensAlignInside("anthropic/claude-sonnet-4", word) ||
			tokensAlignInside("cohere/command-r-08-2024", word)
		if got != want {
			t.Errorf("token alignment for %q = %v, want %v", word, got, want)
		}
	}
	if tokensLeadBase("command-a", "comma") {
		t.Fatal("comma still leads command-a — the GAIA template dies at compile again")
	}
	if !tokensLeadBase("kimi-k2", "kimi") {
		t.Fatal("kimi no longer leads kimi-k2")
	}
}
