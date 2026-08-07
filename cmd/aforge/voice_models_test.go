package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

type voiceRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn voiceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestVoiceModelDiscoveryFiltersTranscriptionOutput(t *testing.T) {
	previous := modelCatalogHTTPClient
	t.Cleanup(func() { modelCatalogHTTPClient = previous })
	modelCatalogHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"text/model","name":"Text","architecture":{"output_modalities":["text"]}},
			{"id":"audio/asr","name":"ASR","architecture":{"output_modalities":["text","transcription"]}},
			{"id":"audio/caps","name":"Caps","architecture":{"output_modalities":["Transcription"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	models, err := fetchVoiceModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Slug != "audio/asr" || models[1].Slug != "audio/caps" {
		t.Fatalf("voice models = %+v", models)
	}
}

func TestVoiceModelCatalogUsesStaleCacheOfflineAndHasDefaultFallback(t *testing.T) {
	previous := modelCatalogHTTPClient
	t.Cleanup(func() { modelCatalogHTTPClient = previous })
	modelCatalogHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	directory := t.TempDir()
	cached := modelCatalogCache{
		FetchedAt: time.Now().Add(-48 * time.Hour),
		Models:    []tui.ModelChoice{{Slug: "cached/asr"}},
	}
	if err := saveVoiceModelCatalog(directory, cached); err != nil {
		t.Fatal(err)
	}
	commander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefsDir: directory,
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
	}
	if got := commander.CatalogFor("voice"); len(got) != 1 || got[0].Slug != "cached/asr" {
		t.Fatalf("offline cached catalog = %+v", got)
	}

	emptyCommander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefsDir: t.TempDir(),
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
	}
	if got := emptyCommander.CatalogFor("voice"); len(got) != 1 || got[0].Slug != config.DefaultVoiceModel {
		t.Fatalf("offline default catalog = %+v", got)
	}
}

func TestVoiceModelChoicePersistsInChatConfig(t *testing.T) {
	directory := t.TempDir()
	commander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefsDir: directory,
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
	}
	if err := commander.SetModel("voice", "acme/asr"); err != nil {
		t.Fatal(err)
	}
	if got := loadChatPrefs(directory).VoiceModel; got != "acme/asr" {
		t.Fatalf("persisted voice model = %q", got)
	}
}

func TestVoiceSpendUsesTheDurableUsageLedger(t *testing.T) {
	graph, err := store.Open(t.TempDir() + "/graph.db")
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	commander := &chatCommander{store: graph}
	commander.RecordVoiceUsage(0.0125)
	usage, err := graph.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if usage.Cost != 0.0125 || usage.Nodes != 1 {
		t.Fatalf("voice usage = %+v", usage)
	}
}
