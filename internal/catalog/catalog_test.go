package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func catalogClient(t *testing.T, status int, body string, inspect func(*http.Request)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

const catalogPayload = `{"data":[
  {"id":"vision/model","name":"Vision","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}},
  {"id":"paint/model","architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
  {"id":"voice/model","architecture":{"input_modalities":["text"],"output_modalities":["audio"]}},
  {"id":"stt/model","architecture":{"input_modalities":["audio"],"output_modalities":["transcription"]}}
]}`

func TestLoadFetchesParsesCachesAndQueriesModalities(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", APIKey: "secret", Dir: dir,
		HTTPClient: catalogClient(t, http.StatusOK, catalogPayload, func(request *http.Request) {
			calls.Add(1)
			if request.URL.Path != "/api/v1/models" || request.Header.Get("Authorization") != "Bearer secret" {
				t.Errorf("request = %s auth %q", request.URL.Path, request.Header.Get("Authorization"))
			}
		}),
	})
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d, want one", calls.Load())
	}
	if got := c.ModelsWithOutput("image"); len(got) != 1 || got[0].ID != "paint/model" {
		t.Fatalf("image models = %+v", got)
	}
	if !c.Supports("~vision/model", "input", "image") || c.Supports("paint/model", "input", "image") {
		t.Fatal("input-image support query was wrong")
	}
	if got := c.ModelsWithOutput("speech"); len(got) != 1 || got[0].ID != "voice/model" {
		t.Fatalf("speech models = %+v", got)
	}
	if got := c.ModelsWithOutput("transcription"); len(got) != 1 || got[0].ID != "stt/model" {
		t.Fatalf("transcription models = %+v", got)
	}

	// A fresh cache must avoid the transport entirely.
	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("fresh cache performed a fetch")
		return nil, errors.New("offline")
	})}
	fromCache := Load(context.Background(), Options{BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: offline})
	if !fromCache.Supports("vision/model", "input", "image") {
		t.Fatal("fresh cache lost modalities")
	}
}

func TestStaleCacheWinsOverOfflineAndEmptyUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := writeCache(cachePath(dir), cache{FetchedAt: old, Models: []Model{{
		ID: "stale/image", OutputModalities: []string{"image"},
	}}}); err != nil {
		t.Fatal(err)
	}
	offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	c := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: offline,
		Now: func() time.Time { return old.Add(48 * time.Hour) },
	})
	if got := c.ModelsWithOutput("image"); len(got) != 1 || got[0].ID != "stale/image" {
		t.Fatalf("stale fallback = %+v", got)
	}

	empty := Load(context.Background(), Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: t.TempDir(), HTTPClient: offline,
	})
	if len(empty.ModelsWithOutput("image")) == 0 || len(empty.ModelsWithOutput("speech")) == 0 {
		t.Fatal("hardcoded offline defaults were not available")
	}
}
