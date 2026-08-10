package main

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

func TestModelPaletteCatalogFiltersEveryCapabilitySlot(t *testing.T) {
	client := &http.Client{Transport: voiceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"chat/text","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"chat/text-two","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"chat/text-three","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"chat/text-four","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"audio/asr","architecture":{"input_modalities":["audio"],"output_modalities":["transcription"]}},
			{"id":"paint/image","architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
			{"id":"voice/tts","name":"Recognizable TTS","architecture":{"input_modalities":["text"],"output_modalities":["speech"]}},
			{"id":"sound/general","name":"General audio","architecture":{"input_modalities":["text"],"output_modalities":["audio"]}},
			{"id":"song/music","architecture":{"input_modalities":["text"],"output_modalities":["music"]}},
			{"id":"motion/video","architecture":{"input_modalities":["text"],"output_modalities":["video"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
	commander := command.New(command.Options{Models: models, Settings: config.Config{VoiceModel: config.DefaultVoiceModel}})
	want := map[string][]string{
		"talk":   {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
		"work":   {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
		"plan":   {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
		"boost":  {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
		"voice":  {"audio/asr"},
		"image":  {"paint/image"},
		"speech": {"voice/tts", "sound/general"},
		"music":  {"sound/general", "song/music"},
		"video":  {"motion/video"},
	}
	for slot, expected := range want {
		choices := commander.CatalogFor(slot)
		got := make([]string, 0, len(choices))
		for _, choice := range choices {
			got = append(got, choice.Slug)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s catalog = %v, want %v", slot, got, expected)
		}
	}
}

// paletteTestSettings is enough configuration for a slot to build a client
// when a test asks it to switch models. Nothing here reaches the network: the
// palette tests only ever read back which slug a slot now names.
var paletteTestSettings = config.Config{APIKey: "test-key", BaseURL: "https://example.invalid/api/v1"}

func TestBoostPreferenceFollowsWorkPersistsOverrideAndCanBeCleared(t *testing.T) {
	directory := t.TempDir()
	work := adoptLiveClient(paletteTestSettings, "work/one", nil)
	commander := command.New(command.Options{PrefsDir: directory, TaskClient: work})
	if !commander.ModelFollows("boost") || commander.CurrentModel("boost") != "work/one" {
		t.Fatalf("default boost follows=%t model=%q", commander.ModelFollows("boost"), commander.CurrentModel("boost"))
	}
	if err := commander.SetModel("boost", "anthropic/claude-opus-5"); err != nil {
		t.Fatal(err)
	}
	if commander.ModelFollows("boost") || commander.CurrentModel("boost") != "anthropic/claude-opus-5" ||
		loadChatPrefs(directory).BoostModel != "anthropic/claude-opus-5" {
		t.Fatalf("explicit boost prefs = %+v model=%q", loadChatPrefs(directory), commander.CurrentModel("boost"))
	}
	if err := commander.SetModel("boost", ""); err != nil {
		t.Fatal(err)
	}
	if err := work.SetModel("work/two"); err != nil {
		t.Fatal(err)
	}
	if !commander.ModelFollows("boost") || commander.CurrentModel("boost") != "work/two" || loadChatPrefs(directory).BoostModel != "" {
		t.Fatalf("cleared boost prefs = %+v model=%q", loadChatPrefs(directory), commander.CurrentModel("boost"))
	}
}

func TestPlanPreferenceFollowsWorkPersistsOverrideAndCanBeCleared(t *testing.T) {
	directory := t.TempDir()
	testSettings := config.Config{APIKey: "test-key", BaseURL: "https://example.invalid/api/v1"}
	work := adoptLiveClient(testSettings, "work/one", nil)
	planner := adoptLiveClient(testSettings, "work/one", nil)
	commander := command.New(command.Options{PrefsDir: directory, TaskClient: work, PlanClient: planner, Settings: testSettings})
	if !commander.ModelFollows("plan") || commander.CurrentModel("plan") != "work/one" {
		t.Fatalf("default plan follows=%t model=%q", commander.ModelFollows("plan"), commander.CurrentModel("plan"))
	}
	if err := commander.SetModel("plan", "anthropic/claude-opus-5"); err != nil {
		t.Fatal(err)
	}
	if commander.ModelFollows("plan") || commander.CurrentModel("plan") != "anthropic/claude-opus-5" ||
		loadChatPrefs(directory).PlanModel != "anthropic/claude-opus-5" {
		t.Fatalf("explicit plan prefs = %+v model=%q", loadChatPrefs(directory), commander.CurrentModel("plan"))
	}
	// A work switch must not move an explicitly chosen plan model.
	if err := commander.SetModel("work", "work/two"); err != nil {
		t.Fatal(err)
	}
	if commander.CurrentModel("plan") != "anthropic/claude-opus-5" {
		t.Fatalf("work switch moved explicit plan model to %q", commander.CurrentModel("plan"))
	}
	if err := commander.SetModel("plan", ""); err != nil {
		t.Fatal(err)
	}
	if !commander.ModelFollows("plan") || commander.CurrentModel("plan") != "work/two" || loadChatPrefs(directory).PlanModel != "" {
		t.Fatalf("cleared plan prefs = %+v model=%q", loadChatPrefs(directory), commander.CurrentModel("plan"))
	}
	// Following again: a work switch carries the plan slot with it, live.
	if err := commander.SetModel("work", "work/three"); err != nil {
		t.Fatal(err)
	}
	if commander.CurrentModel("plan") != "work/three" {
		t.Fatalf("following plan slot stayed on %q after work moved", commander.CurrentModel("plan"))
	}
}

func TestMediaModelPreferencesPersistAndUpdateFutureLeafSnapshot(t *testing.T) {
	directory := t.TempDir()
	media := command.NewMediaModels(exec.MediaTools{
		ImageModel: "old/image", SpeechModel: "old/speech", MusicModel: "old/music", VideoModel: "old/video",
	}, nil)
	commander := command.New(command.Options{PrefsDir: directory, MediaModels: media})
	picks := map[string]string{
		"image": "new/image", "speech": "new/speech", "music": "new/music", "video": "new/video",
	}
	for role, slug := range picks {
		if err := commander.SetModel(role, slug); err != nil {
			t.Fatalf("set %s: %v", role, err)
		}
	}
	persisted := loadChatPrefs(directory)
	if persisted.ImageModel != picks["image"] || persisted.SpeechModel != picks["speech"] ||
		persisted.MusicModel != picks["music"] || persisted.VideoModel != picks["video"] {
		t.Fatalf("persisted media preferences = %+v", persisted)
	}
	snapshot := media.Snapshot()
	if snapshot.ImageModel != picks["image"] || snapshot.SpeechModel != picks["speech"] ||
		snapshot.MusicModel != picks["music"] || snapshot.VideoModel != picks["video"] {
		t.Fatalf("future leaf media snapshot = %+v", snapshot)
	}
	reloaded := command.New(command.Options{Prefs: loadChatPrefs(directory)})
	for role, slug := range picks {
		if got := reloaded.CurrentModel(role); got != slug {
			t.Fatalf("reloaded %s model = %q, want %q", role, got, slug)
		}
	}
}
