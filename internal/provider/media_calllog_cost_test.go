package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
)

// Every wire request has a pair, and the job cost belongs to its completed
// poll rather than being lost or charged again when its bytes are downloaded.
func assertMediaPairs(t *testing.T, rows []calllog.Record, pairs int, cost float64) {
	t.Helper()
	starts := map[string]bool{}
	ends := map[string]bool{}
	total := 0.0
	for _, row := range rows {
		if row.ID == "" || row.Tag != "media" {
			t.Fatalf("unpaired media row: %+v", row)
		}
		if row.Phase == "start" {
			if starts[row.ID] {
				t.Fatal("duplicate start")
			}
			starts[row.ID] = true
		} else {
			if ends[row.ID] {
				t.Fatal("duplicate end")
			}
			ends[row.ID] = true
			total += row.Cost
		}
	}
	if len(rows) != pairs*2 || len(starts) != pairs || len(ends) != pairs || total != cost {
		t.Fatalf("media pairs=%d/%d rows=%d cost=%v, want %d pairs and %v", len(starts), len(ends), len(rows), total, pairs, cost)
	}
	for id := range starts {
		if !ends[id] {
			t.Fatalf("start %s has no end", id)
		}
	}
}

func TestHeaderOnlyImageCostReachesItsCallLogPair(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OpenRouter-Cost", "0.07")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
	}))
	t.Cleanup(server.Close)
	media, err := NewMediaClient(Config{APIKey: "fixture", BaseURL: server.URL + "/api/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	response, err := media.GenerateImage(context.Background(), ImageRequest{Model: "paint/model", Prompt: "harbor"})
	if err != nil || response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 0.07 {
		t.Fatalf("image response=%+v err=%v", response, err)
	}
	assertMediaPairs(t, read(), 1, 0.07)
}

func TestCompletedVideoPollKeepsItsCostInOneCallLogPair(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			_, _ = io.WriteString(w, `{"id":"job-1","status":"pending"}`)
		case r.URL.Path == "/api/v1/videos/job-1":
			_, _ = io.WriteString(w, `{"id":"job-1","status":"completed","unsigned_urls":["/artifact/video.mp4"],"usage":{"cost":1.25}}`)
		case r.URL.Path == "/artifact/video.mp4":
			_, _ = io.WriteString(w, "mp4 bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	media, err := NewMediaClient(Config{APIKey: "fixture", BaseURL: server.URL + "/api/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	media.videoWait = func(context.Context, time.Duration) error { return nil }
	response, err := media.GenerateVideo(context.Background(), VideoRequest{Model: "motion/model", Prompt: "harbor"})
	if err != nil || response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 1.25 {
		t.Fatalf("video response=%+v err=%v", response, err)
	}
	assertMediaPairs(t, read(), 3, 1.25)
	done := ended(read())
	if done[1].Cost != 1.25 || done[2].Cost != 0 {
		t.Fatalf("poll/download costs=%+v", done)
	}
}

// A token-only usage object still has a header-only cost; the two sources
// describe different fields and must not erase one another.
func TestImageHeaderCostFillsTokenUsageWithoutReplacingJSONCost(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		cost        float64
	}{
		{"tokens-only", `{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}`, .07},
		{"json-cost", `{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"cost":0.11}`, .11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := loggingTo(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-OpenRouter-Cost", "0.07")
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}],"usage":`+tc.usage+`}`)
			}))
			t.Cleanup(server.Close)
			media, err := NewMediaClient(Config{APIKey: "fixture", BaseURL: server.URL + "/api/v1", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			response, err := media.GenerateImage(context.Background(), ImageRequest{Model: "paint/model", Prompt: "harbor"})
			if err != nil || response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != tc.cost || response.Usage.PromptTokens != 7 || response.Usage.CompletionTokens != 3 || response.Usage.TotalTokens != 10 {
				t.Fatalf("merged image usage=%+v, err=%v", response, err)
			}
			assertMediaPairs(t, read(), 1, tc.cost)
		})
	}
}
