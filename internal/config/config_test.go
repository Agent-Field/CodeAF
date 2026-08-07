package config

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

func settings(t *testing.T) Config {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	// Nothing here may reach the network. A refused connection is the offline
	// case, which the catalog is required to tolerate.
	t.Setenv("AFORGE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	t.Setenv("AFORGE_DAILY_BUDGET", "")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestMediaSlotsUseEnvironmentAndRuntimeCatalogOrder(t *testing.T) {
	t.Setenv("AFORGE_IMAGE_MODEL", "user/image")
	t.Setenv("AFORGE_SPEECH_MODEL", "user/speech")
	t.Setenv("AFORGE_MUSIC_MODEL", "user/music")
	t.Setenv("AFORGE_VIDEO_MODEL", "user/video")
	configured := settings(t)
	if configured.ImageModel != "user/image" || configured.SpeechModel != "user/speech" ||
		configured.MusicModel != "user/music" || configured.VideoModel != "user/video" {
		t.Fatalf("media slots = %q %q %q %q", configured.ImageModel, configured.SpeechModel,
			configured.MusicModel, configured.VideoModel)
	}
	if configured.ResolveImageModel(nil) != "user/image" || configured.ResolveSpeechModel(nil) != "user/speech" ||
		configured.ResolveMusicModel(nil) != "user/music" || configured.ResolveVideoModel(nil) != "user/video" {
		t.Fatal("explicit media slots were made catalog-dependent")
	}

	t.Setenv("AFORGE_IMAGE_MODEL", "")
	t.Setenv("AFORGE_SPEECH_MODEL", "")
	t.Setenv("AFORGE_MUSIC_MODEL", "")
	t.Setenv("AFORGE_VIDEO_MODEL", "")
	resolved := settings(t)
	// Use the package's offline defaults to exercise the verified preference
	// slugs without exposing catalog construction internals.
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "://offline", Dir: t.TempDir(),
	})
	if got := resolved.ResolveImageModel(models); got != preferredImageModel {
		t.Fatalf("resolved image = %q", got)
	}
	if got := resolved.ResolveSpeechModel(models); got != preferredSpeechModel {
		t.Fatalf("resolved speech = %q", got)
	}
	if got := resolved.ResolveMusicModel(models); got != preferredMusicModel {
		t.Fatalf("resolved music = %q", got)
	}
	if got := resolved.ResolveVideoModel(models); got != preferredVideoModel {
		t.Fatalf("resolved video = %q", got)
	}
}

type configRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn configRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func runtimeCatalog(t *testing.T, rows string) *catalog.Catalog {
	t.Helper()
	client := &http.Client{Transport: configRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":[` + rows + `]}`)), Request: request}, nil
	})}
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
}

func TestImageAndSpeechPreferenceOrders(t *testing.T) {
	models := runtimeCatalog(t, `
		{"id":"first/image","architecture":{"output_modalities":["image"]}},
		{"id":"krea/krea-2-medium-turbo","architecture":{"output_modalities":["image"]}},
		{"id":"first/speech","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["audio"]}},
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}}`)
	configured := Config{}
	if got := configured.ResolveImageModel(models); got != preferredImageModel {
		t.Fatalf("image preference = %q", got)
	}
	if got := configured.ResolveSpeechModel(models); got != preferredSpeechModel {
		t.Fatalf("speech preference = %q", got)
	}

	withoutPrimary := runtimeCatalog(t, `
		{"id":"first/image","architecture":{"output_modalities":["image"]}},
		{"id":"first/speech","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["audio"]}}`)
	if got := configured.ResolveImageModel(withoutPrimary); got != "first/image" {
		t.Fatalf("image catalog fallback = %q", got)
	}
	if got := configured.ResolveSpeechModel(withoutPrimary); got != fallbackSpeechModel {
		t.Fatalf("speech secondary preference = %q", got)
	}

	firstOnly := runtimeCatalog(t, `{"id":"first/speech","architecture":{"output_modalities":["speech"]}}`)
	if got := configured.ResolveSpeechModel(firstOnly); got != "first/speech" {
		t.Fatalf("speech catalog fallback = %q", got)
	}
}

func TestMusicAndVideoPreferenceOrders(t *testing.T) {
	models := runtimeCatalog(t, `
		{"id":"voice/tts","name":"Voice TTS","architecture":{"output_modalities":["audio"]}},
		{"id":"first/music","architecture":{"output_modalities":["music"]}},
		{"id":"google/lyria-3-clip-preview","architecture":{"output_modalities":["audio"]}},
		{"id":"first/video","architecture":{"output_modalities":["video"]}},
		{"id":"bytedance/seedance-1-5-pro","architecture":{"output_modalities":["video"]}}`)
	configured := Config{}
	if got := configured.ResolveMusicModel(models); got != preferredMusicModel {
		t.Fatalf("music preference = %q", got)
	}
	if got := configured.ResolveVideoModel(models); got != preferredVideoModel {
		t.Fatalf("video preference = %q", got)
	}

	withoutPreferred := runtimeCatalog(t, `
		{"id":"voice/tts","architecture":{"output_modalities":["speech"]}},
		{"id":"first/music","architecture":{"output_modalities":["music"]}},
		{"id":"first/video","architecture":{"output_modalities":["video"]}}`)
	if got := configured.ResolveMusicModel(withoutPreferred); got != "first/music" {
		t.Fatalf("music catalog fallback = %q", got)
	}
	if got := configured.ResolveVideoModel(withoutPreferred); got != "first/video" {
		t.Fatalf("video catalog fallback = %q", got)
	}
	if got := configured.ResolveMusicModel(runtimeCatalog(t, ``)); got != preferredMusicModel {
		t.Fatalf("music built-in fallback = %q", got)
	}
}

// TestNoPanelIsTheKillSwitch is the promise the whole feature is gated on. With
// AFORGE_MODELS unset the harness must build the adapter it has always built and
// run the path it has always run — not a router with one rung, which would be a
// different code path wearing the same behaviour.
func TestNoPanelIsTheKillSwitch(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "")
	config := settings(t)
	if len(config.Panel.Models) != 0 {
		t.Fatalf("panel = %+v, want none", config.Panel)
	}
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, plain := client.(*provider.Client); !plain {
		t.Fatalf("client = %T, want the single-model adapter", client)
	}
	if client.Model() != DefaultModel {
		t.Fatalf("model = %q, want the configured default", client.Model())
	}
	// And the run context is stamped exactly as before: one cache key for the
	// run, the operator's reasoning level, and no call class until a call site
	// opens one.
	ctx := config.Context(context.Background(), "a goal")
	if provider.CacheKeyFrom(ctx) != provider.RunCacheKey("a goal", DefaultModel) {
		t.Fatal("the run cache key changed")
	}
	if provider.CallClassFrom(ctx) != "" {
		t.Fatal("a call class was stamped where no call site opened one")
	}
	picked, err := config.ClientFor("user/explicit-pick")
	if err != nil {
		t.Fatal(err)
	}
	if _, plain := picked.(*provider.Client); !plain || picked.Model() != "user/explicit-pick" {
		t.Fatalf("explicit no-panel client = %T model %q, want the unchanged bare adapter", picked, picked.Model())
	}
}

func TestVoiceModelHasIndependentDefaultAndEnvironmentOverride(t *testing.T) {
	t.Setenv("AFORGE_VOICE_MODEL", "")
	if got := settings(t).VoiceModel; got != DefaultVoiceModel {
		t.Fatalf("voice model = %q, want %q", got, DefaultVoiceModel)
	}
	t.Setenv("AFORGE_VOICE_MODEL", "acme/transcriber")
	if got := settings(t).VoiceModel; got != "acme/transcriber" {
		t.Fatalf("voice model override = %q", got)
	}
}

// TestAPanelSwitchesInTheRouter is the other side of the switch, and it is one
// line of environment away.
func TestAPanelSwitchesInTheRouter(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "google/gemma-3-12b-it,~deepseek/deepseek-v4-flash-latest,moonshotai/kimi-k2.6")
	config := settings(t)
	if len(config.Panel.Models) != 3 {
		t.Fatalf("panel = %+v, want three models", config.Panel.Models)
	}
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	panel, routed := client.(*router.Router)
	if !routed {
		t.Fatalf("client = %T, want a router", client)
	}
	defer panel.Close()
	if panel.Rungs() != 3 {
		t.Fatalf("rungs = %d, want three", panel.Rungs())
	}
	if client.Model() != "google/gemma-3-12b-it" {
		t.Fatalf("model = %q, want the first panel entry", client.Model())
	}
	picked, err := config.ClientFor("moonshotai/kimi-k2.6")
	if err != nil {
		t.Fatal(err)
	}
	pickedPanel, routed := picked.(*router.Router)
	if !routed {
		t.Fatalf("explicit panel client = %T, want a router", picked)
	}
	defer pickedPanel.Close()
	if picked.Model() != "moonshotai/kimi-k2.6" {
		t.Fatalf("explicit panel model = %q, want the picker choice", picked.Model())
	}
	outside, err := config.ClientFor("user/model-outside-panel")
	if err != nil {
		t.Fatal(err)
	}
	outsidePanel := outside.(*router.Router)
	defer outsidePanel.Close()
	if outside.Model() != "user/model-outside-panel" || outsidePanel.Rungs() != 4 {
		t.Fatalf("outside picker = model %q over %d rungs, want the explicit opener plus the panel",
			outside.Model(), outsidePanel.Rungs())
	}
}

// TestPanelStateLivesWhereTheProfileDoes keeps a run's memory in one place. An
// operator who redirected AFORGE_PROFILE_DIR — a test, a sandbox, a second
// account — must not find half of it in their home directory anyway.
func TestPanelStateLivesWhereTheProfileDoes(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "a/one,b/two")
	config := settings(t)
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	panel := client.(*router.Router)
	panel.Ledger().Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	if err := panel.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"router-ledger.json", "router-events.jsonl"} {
		if _, err := os.Stat(filepath.Join(config.ProfileDir, name)); err != nil {
			t.Fatalf("%s was not written to the configured profile directory: %v", name, err)
		}
	}
}

// TestAnUnreadablePanelIsAStartupError rather than a silent fallback. A typo in
// AFORGE_MODELS that quietly ran everything on one model would be discovered
// only in the bill, or never.
func TestAnUnreadablePanelIsAStartupError(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_MODELS", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := Load(); err == nil {
		t.Fatal("a panel file that does not exist was accepted")
	}
}

func TestDailyBudgetUSDDefaultOverrideAndUnlimited(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	for _, test := range []struct {
		raw  string
		want float64
	}{
		{"", DefaultDailyBudgetUSD},
		{"37.25", 37.25},
		{"0", 0},
	} {
		t.Run(test.raw, func(t *testing.T) {
			t.Setenv("AFORGE_DAILY_BUDGET", test.raw)
			got, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if got.DailyBudgetUSD != test.want {
				t.Fatalf("daily budget = %v, want %v", got.DailyBudgetUSD, test.want)
			}
		})
	}

	for _, raw := range []string{"-1", "NaN", "Inf", "twenty"} {
		t.Run("invalid-"+raw, func(t *testing.T) {
			t.Setenv("AFORGE_DAILY_BUDGET", raw)
			if _, err := DailyBudgetUSD(); err == nil {
				t.Fatalf("invalid daily budget %q was accepted", raw)
			}
		})
	}
}

func TestDailyBudgetUSDPersistedConfigAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := BudgetConfigPath(dir)
	if err := os.WriteFile(path, []byte(`{"future_setting":"preserved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(dir, 35); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AFORGE_DAILY_BUDGET", "")
	got, err := DailyBudgetUSDAt(dir)
	if err != nil || got != 35 {
		t.Fatalf("persisted daily budget = %v err=%v", got, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	if values["future_setting"] != "preserved" || values["daily_budget_usd"] != float64(35) {
		t.Fatalf("persisted config = %#v", values)
	}

	t.Setenv("AFORGE_DAILY_BUDGET", "42")
	got, err = DailyBudgetUSDAt(dir)
	if err != nil || got != 42 {
		t.Fatalf("environment override = %v err=%v", got, err)
	}
}
