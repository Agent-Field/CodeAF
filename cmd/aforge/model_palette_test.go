package main

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
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
	commander := &chatCommander{models: models, settings: config.Config{VoiceModel: config.DefaultVoiceModel}}
	want := map[string][]string{
		"talk":   {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
		"work":   {"chat/text", "chat/text-two", "chat/text-three", "chat/text-four"},
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

func TestBoostPreferenceFollowsWorkPersistsOverrideAndCanBeCleared(t *testing.T) {
	directory := t.TempDir()
	work := &liveClient{model: "work/one"}
	commander := &chatCommander{prefsDir: directory, taskClient: work}
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
	work.mu.Lock()
	work.model = "work/two"
	work.mu.Unlock()
	if !commander.ModelFollows("boost") || commander.CurrentModel("boost") != "work/two" || loadChatPrefs(directory).BoostModel != "" {
		t.Fatalf("cleared boost prefs = %+v model=%q", loadChatPrefs(directory), commander.CurrentModel("boost"))
	}
}

func TestMediaModelPreferencesPersistAndUpdateFutureLeafSnapshot(t *testing.T) {
	directory := t.TempDir()
	media := &chatMediaModels{tools: exec.MediaTools{
		ImageModel: "old/image", SpeechModel: "old/speech", MusicModel: "old/music", VideoModel: "old/video",
	}}
	commander := &chatCommander{prefsDir: directory, prefs: chatPrefs{}, mediaModels: media}
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
	reloaded := &chatCommander{prefs: loadChatPrefs(directory)}
	for role, slug := range picks {
		if got := reloaded.CurrentModel(role); got != slug {
			t.Fatalf("reloaded %s model = %q, want %q", role, got, slug)
		}
	}
}
