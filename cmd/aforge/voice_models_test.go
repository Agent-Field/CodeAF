package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

type voiceRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn voiceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// The voice dropdown lists transcription-capable models straight from the
// shared catalog seam — no separate voice catalog fetch or cache exists.
func TestVoiceModelDiscoveryFiltersTranscriptionOutput(t *testing.T) {
	client := &http.Client{Transport: voiceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"text/model","name":"Text","architecture":{"output_modalities":["text"]}},
			{"id":"audio/asr","name":"ASR","architecture":{"output_modalities":["text","transcription"]}},
			{"id":"audio/caps","name":"Caps","architecture":{"output_modalities":["Transcription"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
	commander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
		models:   models,
	}
	got := commander.CatalogFor("voice")
	if len(got) != 2 || got[0].Slug != "audio/asr" || got[1].Slug != "audio/caps" {
		t.Fatalf("voice models = %+v", got)
	}
}

func TestVoiceModelCatalogUsesStaleCacheOfflineAndHasDefaultFallback(t *testing.T) {
	directory := t.TempDir()
	online := &http.Client{Transport: voiceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload := `{"data":[{"id":"cached/asr","name":"Cached","architecture":{"output_modalities":["transcription"]}}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	// Seed a cache that is already stale by the time the offline load happens.
	_ = catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: directory, HTTPClient: online,
		Now: func() time.Time { return time.Now().Add(-48 * time.Hour) },
	})

	offline := &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: directory, HTTPClient: offline,
	})
	commander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefsDir: directory,
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
		models:   models,
	}
	if got := commander.CatalogFor("voice"); len(got) != 1 || got[0].Slug != "cached/asr" {
		t.Fatalf("offline cached catalog = %+v", got)
	}

	// No cache, no network: the dropdown still offers the configured voice
	// model rather than going empty.
	emptyModels := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: offline,
	})
	emptyCommander := &chatCommander{
		settings: config.Config{VoiceModel: config.DefaultVoiceModel},
		prefsDir: t.TempDir(),
		prefs:    chatPrefs{VoiceModel: config.DefaultVoiceModel},
		models:   emptyModels,
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
