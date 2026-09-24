package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/router"
)

func settings(t *testing.T) Config {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	// Nothing here may reach the network. A refused connection is the offline
	// case, which the catalog is required to tolerate.
	t.Setenv("CODEAF_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	t.Setenv("CODEAF_BRIEF_AFTER", "")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestMediaSlotsUseEnvironmentAndRuntimeCatalogOrder(t *testing.T) {
	t.Setenv("CODEAF_IMAGE_MODEL", "user/image")
	t.Setenv("CODEAF_SPEECH_MODEL", "user/speech")
	t.Setenv("CODEAF_MUSIC_MODEL", "user/music")
	t.Setenv("CODEAF_VIDEO_MODEL", "user/video")
	t.Setenv("CODEAF_VISION_MODEL", "user/vision")
	configured := settings(t)
	if configured.ImageModel != "user/image" || configured.SpeechModel != "user/speech" ||
		configured.MusicModel != "user/music" || configured.VideoModel != "user/video" || configured.VisionModel != "user/vision" {
		t.Fatalf("media slots = %q %q %q %q %q", configured.ImageModel, configured.SpeechModel,
			configured.MusicModel, configured.VideoModel, configured.VisionModel)
	}
	if configured.ResolveImageModel(nil) != "user/image" || configured.ResolveSpeechModel(nil) != "user/speech" ||
		configured.ResolveMusicModel(nil) != "user/music" || configured.ResolveVideoModel(nil) != "user/video" ||
		configured.ResolveVisionModel(nil, "talk/model", "work/model") != "user/vision" {
		t.Fatal("explicit media slots were made catalog-dependent")
	}

	t.Setenv("CODEAF_IMAGE_MODEL", "")
	t.Setenv("CODEAF_SPEECH_MODEL", "")
	t.Setenv("CODEAF_MUSIC_MODEL", "")
	t.Setenv("CODEAF_VIDEO_MODEL", "")
	t.Setenv("CODEAF_VISION_MODEL", "")
	resolved := settings(t)
	// Use the package's offline defaults to exercise the verified preference
	// slugs without exposing catalog construction internals.
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: catalog.DefaultBaseURL, Dir: t.TempDir(),
		HTTPClient: &http.Client{Transport: configRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, http.ErrServerClosed
		})},
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

func TestVisionModelResolutionOrder(t *testing.T) {
	t.Setenv("CODEAF_VISION_MODEL", "operator/vision")
	explicit := settings(t)
	if got := explicit.ResolveVisionModel(nil, "talk/vision", "work/vision"); got != "operator/vision" {
		t.Fatalf("explicit vision = %q", got)
	}

	t.Setenv("CODEAF_VISION_MODEL", "")
	configured := settings(t)
	models := runtimeCatalog(t, `
		{"id":"first/vision","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
		{"id":"talk/vision","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
		{"id":"work/vision","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
		{"id":"google/gemini-3.7-flash","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
		{"id":"text/only","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	if got := configured.ResolveVisionModel(models, "talk/vision", "work/vision"); got != "talk/vision" {
		t.Fatalf("vision talk preference = %q", got)
	}
	if got := configured.ResolveVisionModel(models, "text/only", "work/vision"); got != "work/vision" {
		t.Fatalf("vision work preference = %q", got)
	}
	if got := configured.ResolveVisionModel(models, "text/only", "missing/work"); got != preferredVisionModel {
		t.Fatalf("vision advertised preference = %q", got)
	}

	firstOnly := runtimeCatalog(t, `
		{"id":"first/vision","architecture":{"input_modalities":["image"],"output_modalities":["text"]}},
		{"id":"second/vision","architecture":{"input_modalities":["image"],"output_modalities":["text"]}}`)
	if got := configured.ResolveVisionModel(firstOnly, "text/only", "missing/work"); got != "first/vision" {
		t.Fatalf("vision catalog fallback = %q", got)
	}
	noVision := runtimeCatalog(t, `{"id":"text/only","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	if got := configured.ResolveVisionModel(noVision, "text/only", "missing/work"); got != "" {
		t.Fatalf("vision unavailable catalog = %q, want no proxy", got)
	}
	if got := configured.ResolveVisionModel(nil, "talk/vision", "work/vision"); got != "" {
		t.Fatalf("vision absent catalog = %q, want no proxy", got)
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
		{"id":"bytedance-seed/seedream-5-0-pro","architecture":{"output_modalities":["image"]}},
		{"id":"krea/krea-2-medium-turbo","architecture":{"output_modalities":["image"]}},
		{"id":"first/speech","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["audio"]}},
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}},
		{"id":"fish-audio/s2.1-pro","architecture":{"output_modalities":["speech"]}},
		{"id":"fish-audio/s1","architecture":{"output_modalities":["speech"]}}`)
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
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["audio"]}},
		{"id":"hexgrad/kokoro-82m","architecture":{"output_modalities":["speech"]}}`)
	if got := configured.ResolveImageModel(withoutPrimary); got != "first/image" {
		t.Fatalf("image catalog fallback = %q", got)
	}
	if got := configured.ResolveSpeechModel(withoutPrimary); got != kokoroSpeechModel {
		t.Fatalf("speech secondary preference = %q", got)
	}

	// The last remembered voice, for a catalog that advertises neither of the
	// two above it.
	hostedOnly := runtimeCatalog(t, `
		{"id":"first/speech","architecture":{"output_modalities":["speech"]}},
		{"id":"openai/gpt-4o-mini-tts","architecture":{"output_modalities":["audio"]}}`)
	if got := configured.ResolveSpeechModel(hostedOnly); got != hostedSpeechModel {
		t.Fatalf("speech third preference = %q", got)
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
		{"id":"google/lyria-3-pro-preview","architecture":{"output_modalities":["audio"]}},
		{"id":"first/video","architecture":{"output_modalities":["video"]}},
		{"id":"bytedance/seedance-2.5","architecture":{"output_modalities":["video"]}},
		{"id":"bytedance/seedance-2.0-mini","architecture":{"output_modalities":["video"]}}`)
	configured := Config{}
	if got := configured.ResolveMusicModel(models); got != preferredMusicModel {
		t.Fatalf("music preference = %q", got)
	}
	if got := configured.ResolveVideoModel(models); got != preferredVideoModel {
		t.Fatalf("video preference = %q", got)
	}

	// The older pair is still reached for when the preferred row is missing,
	// rather than the catalog's first arbitrary name.
	olderPair := runtimeCatalog(t, `
		{"id":"first/music","architecture":{"output_modalities":["music"]}},
		{"id":"google/lyria-3-pro-preview","architecture":{"output_modalities":["audio"]}},
		{"id":"first/video","architecture":{"output_modalities":["video"]}},
		{"id":"bytedance/seedance-2.0-mini","architecture":{"output_modalities":["video"]}}`)
	if got := configured.ResolveMusicModel(olderPair); got != fallbackMusicModel {
		t.Fatalf("music secondary preference = %q", got)
	}
	if got := configured.ResolveVideoModel(olderPair); got != fallbackVideoModel {
		t.Fatalf("video secondary preference = %q", got)
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
	if got := configured.ResolveMusicModel(runtimeCatalog(t, ``)); got != "" {
		t.Fatalf("empty music catalog resolved %q", got)
	}
}

func TestPracticeBudgetAndIdleDefaultsAndOverrides(t *testing.T) {
	t.Setenv("CODEAF_PRACTICE_BUDGET", "")
	t.Setenv("CODEAF_PRACTICE_IDLE", "")
	got := settings(t)
	if got.PracticeBudgetUSD != DefaultPracticeBudgetUSD || got.PracticeIdle != DefaultPracticeIdle {
		t.Fatalf("practice defaults = $%v/%s", got.PracticeBudgetUSD, got.PracticeIdle)
	}

	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_PRACTICE_BUDGET", "3.5")
	t.Setenv("CODEAF_PRACTICE_IDLE", "45m")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.PracticeBudgetUSD != 3.5 || got.PracticeIdle != 45*time.Minute {
		t.Fatalf("practice overrides = $%v/%s", got.PracticeBudgetUSD, got.PracticeIdle)
	}

	for _, test := range []struct{ budget, idle string }{
		{budget: "-1"}, {budget: "NaN"}, {idle: "-1m"}, {idle: "later"},
	} {
		t.Setenv("CODEAF_PRACTICE_BUDGET", test.budget)
		t.Setenv("CODEAF_PRACTICE_IDLE", test.idle)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid practice settings budget=%q idle=%q were accepted", test.budget, test.idle)
		}
	}
}

func TestBriefAfterConfiguration(t *testing.T) {
	config := settings(t)
	if config.BriefAfter != 4*time.Hour {
		t.Fatalf("default brief threshold = %s, want 4h", config.BriefAfter)
	}

	t.Setenv("CODEAF_BRIEF_AFTER", "90m")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.BriefAfter != 90*time.Minute {
		t.Fatalf("configured brief threshold = %s, want 90m", config.BriefAfter)
	}

	t.Setenv("CODEAF_BRIEF_AFTER", "-1h")
	if _, err := Load(); err == nil {
		t.Fatal("negative CODEAF_BRIEF_AFTER was accepted")
	}
}

// TestNoPanelIsTheKillSwitch is the promise the whole feature is gated on. With
// CODEAF_MODELS unset the harness must build the adapter it has always built and
// run the path it has always run — not a router with one rung, which would be a
// different code path wearing the same behaviour.
func TestNoPanelIsTheKillSwitch(t *testing.T) {
	t.Setenv("CODEAF_MODELS", "")
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
	t.Setenv("CODEAF_VOICE_MODEL", "")
	if got := settings(t).VoiceModel; got != DefaultVoiceModel {
		t.Fatalf("voice model = %q, want %q", got, DefaultVoiceModel)
	}
	t.Setenv("CODEAF_VOICE_MODEL", "acme/transcriber")
	if got := settings(t).VoiceModel; got != "acme/transcriber" {
		t.Fatalf("voice model override = %q", got)
	}
}

// TestAPanelSwitchesInTheRouter is the other side of the switch, and it is one
// line of environment away.
func TestAPanelSwitchesInTheRouter(t *testing.T) {
	t.Setenv("CODEAF_MODELS", "google/gemma-3-12b-it,~deepseek/deepseek-v4-flash-latest,moonshotai/kimi-k2.6")
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
// operator who redirected CODEAF_PROFILE_DIR — a test, a sandbox, a second
// account — must not find half of it in their home directory anyway.
func TestPanelStateLivesWhereTheProfileDoes(t *testing.T) {
	t.Setenv("CODEAF_MODELS", "a/one,b/two")
	config := settings(t)
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	panel := client.(*router.Router)
	panel.Ledger().Observe("a/one", provider.ClassPlanSpine, 0, provider.ReadingVerifiedSuccess)
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
// CODEAF_MODELS that quietly ran everything on one model would be discovered
// only in the bill, or never.
func TestAnUnreadablePanelIsAStartupError(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_MODELS", filepath.Join(t.TempDir(), "missing.json"))
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
			t.Setenv("CODEAF_DAILY_BUDGET", test.raw)
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
			t.Setenv("CODEAF_DAILY_BUDGET", raw)
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
	t.Setenv("CODEAF_DAILY_BUDGET", "")
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

	t.Setenv("CODEAF_DAILY_BUDGET", "42")
	got, err = DailyBudgetUSDAt(dir)
	if err != nil || got != 42 {
		t.Fatalf("environment override = %v err=%v", got, err)
	}
}

// Pinning a rung is a cost decision, so an unreadable value must stop the
// process rather than quietly walk the whole ladder the user opted out of.
func TestDocumentEngineDefaultsToAutoAndRefusesUnknownRungs(t *testing.T) {
	if DefaultDocumentEngine != "auto" {
		t.Fatalf("default rung = %q, want the full ladder", DefaultDocumentEngine)
	}
	if got := settings(t).DocumentEngine; got != DefaultDocumentEngine {
		t.Fatalf("default document engine = %q", got)
	}
	for raw, want := range map[string]string{"local": "local", " Free ": "free", "OCR": "ocr", "auto": "auto"} {
		t.Setenv("CODEAF_DOC_ENGINE", raw)
		if got := settings(t).DocumentEngine; got != want {
			t.Fatalf("CODEAF_DOC_ENGINE=%q resolved to %q, want %q", raw, got, want)
		}
	}

	t.Setenv("CODEAF_DOC_ENGINE", "tesseract")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CODEAF_DOC_ENGINE") {
		t.Fatalf("unknown engine error = %v", err)
	}
}

func TestDocumentClientIsDirectLikeTheVisionProxy(t *testing.T) {
	configured := settings(t)
	client, err := configured.DocumentClient()
	if err != nil || client == nil {
		t.Fatalf("document client = %v err=%v", client, err)
	}
}

func TestLoadWarnsOnceForEveryUnreadTopLevelProfileKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ProfileDirEnv, dir)
	if err := os.WriteFile(BudgetConfigPath(dir), []byte(`{"models":{"tiers":{"reflex":"nested/model"}},"typo.key":true,"model.talk":"flat/model","response.attempts":3,"response.lift_after":2,"response.lift_cap_usd":0.5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	warnedProfileConfigs = sync.Map{}
	var output bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	first, err := LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	if first.Model != DefaultModel {
		t.Fatalf("nested model changed resolution: got %q, want %q", first.Model, DefaultModel)
	}
	if got, want := first.UnreadProfileKeys, []string{"models", "typo.key"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unread profile keys = %v, want %v", got, want)
	}
	if got, ok := persistedString(dir, KeyChatModel); !ok || got != "flat/model" {
		t.Fatalf("consumed flat key resolved as %q, %v", got, ok)
	}
	second, err := LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.UnreadProfileKeys, first.UnreadProfileKeys) {
		t.Fatalf("second unread profile keys = %v, want %v", second.UnreadProfileKeys, first.UnreadProfileKeys)
	}
	got := output.String()
	if strings.Count(got, "unread top-level config key(s)") != 1 {
		t.Fatalf("load diagnostic count = %d, want 1: %q", strings.Count(got, "unread top-level config key(s)"), got)
	}
	for _, key := range []string{"models", "typo.key"} {
		if !strings.Contains(got, key) {
			t.Errorf("diagnostic does not name %q: %q", key, got)
		}
	}
	for _, key := range []string{KeyChatModel, KeyResponseAttempts, KeyResponseLiftAfter, KeyResponseLiftCap} {
		if strings.Contains(got, key) {
			t.Errorf("diagnostic called consumed flat key %q unread: %q", key, got)
		}
	}
	if value := ResponseAttemptsAt(dir); value != 3 {
		t.Errorf("response attempts = %v, want 3", value)
	}
	if value := ResponseLiftAfterAt(dir); value != 2 {
		t.Errorf("response lift after = %v, want 2", value)
	}
	if value := ResponseLiftCapAt(dir); value != 0.5 {
		t.Errorf("response lift cap = %v, want 0.5", value)
	}
}

// TestNoShippedWriterKeyIsReportedUnread guards the property that a key the
// product itself writes is never named as unread: the notice must not tell a
// person their config carries an ignored key they never typed. Every settings
// registry row and every non-setting field the loader writes is a shipped
// writer; a profile made of all of them yields an empty unread list.
func TestNoShippedWriterKeyIsReportedUnread(t *testing.T) {
	dir := t.TempDir()
	values := map[string]json.RawMessage{}
	for _, row := range NewSettings(SettingsOptions{ProfileDir: dir}).Rows() {
		if row.Key == "" {
			continue
		}
		values[row.Key] = json.RawMessage(`"x"`)
	}
	for _, key := range []string{
		KeySetupSeen, KeySplitPct, KeyStandingBackground,
		KeyResponseAttempts, KeyResponseLiftAfter, KeyResponseLiftCap,
		keyModelSources,
	} {
		values[key] = json.RawMessage(`"x"`)
	}
	if unread := warnUnreadProfileKeys(dir, values); len(unread) != 0 {
		t.Fatalf("shipped-writer keys reported unread: %v", unread)
	}
}

// loadProfileKeyLedger reads testdata/profile-keys.ledger into a set, skipping
// comment (#) and blank lines.
func loadProfileKeyLedger(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "profile-keys.ledger"))
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = true
	}
	return set
}

// TestProfileKeyLedgerLaw makes it impossible to add or remove a profile writer
// without accounting for it. Half (a): every key a current writer can produce is
// in the ledger. Half (b): every ledger key is either consumed at head or in
// retiredProfileKeys. Adding a settings row forces a ledger line; removing one
// leaves its line behind and turns this red until the key is retired on purpose.
func TestProfileKeyLedgerLaw(t *testing.T) {
	ledger := loadProfileKeyLedger(t)
	consumed := consumedProfileKeys(t.TempDir())

	for key := range consumed {
		if key == "" {
			continue
		}
		if !ledger[key] {
			t.Errorf("writer key %q is not in testdata/profile-keys.ledger; add it", key)
		}
	}
	for key := range ledger {
		if !consumed[key] && !retiredProfileKeys[key] {
			t.Errorf("ledger key %q is neither consumed at head nor in retiredProfileKeys; if its writer was removed, retire the key on purpose", key)
		}
	}
	for key := range retiredProfileKeys {
		if consumed[key] {
			t.Errorf("retired key %q is still consumed at head; remove it from retiredProfileKeys", key)
		}
		if !ledger[key] {
			t.Errorf("retired key %q is not in the ledger", key)
		}
	}
}

// A profile the lane page wrote carries the talk lane's borrow row, which a
// reader consumes, so it is never reported unread.
func TestTheLaneBorrowRowIsNotReportedUnread(t *testing.T) {
	values := map[string]json.RawMessage{
		LaneSettingKey(LaneSlotTalk): json.RawMessage(`"openrouter"`),
		LaneBorrowKey(LaneSlotTalk):  json.RawMessage(`false`),
	}
	if unread := warnUnreadProfileKeys(t.TempDir(), values); len(unread) != 0 {
		t.Fatalf("the lane rows were reported unread: %v", unread)
	}
}

// TestRetiredProfileKeysAreNotReportedUnread pins that a profile carrying a
// retired key is silent, and that a genuinely unknown key is still named.
func TestRetiredProfileKeysAreNotReportedUnread(t *testing.T) {
	dir := t.TempDir()
	for key := range retiredProfileKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			values := map[string]json.RawMessage{key: json.RawMessage(`"x"`)}
			if unread := warnUnreadProfileKeys(dir, values); len(unread) != 0 {
				t.Fatalf("retired key %q reported unread: %v", key, unread)
			}
		})
	}
	values := map[string]json.RawMessage{}
	for key := range retiredProfileKeys {
		values[key] = json.RawMessage(`"x"`)
	}
	values["totally_unknown_key"] = json.RawMessage(`"x"`)
	unread := warnUnreadProfileKeys(dir, values)
	if len(unread) != 1 || unread[0] != "totally_unknown_key" {
		t.Fatalf("with retired keys plus one unknown, expected only totally_unknown_key unread, got %v", unread)
	}
}
