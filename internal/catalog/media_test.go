package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The dedicated media listings, tested against REAL responses. The testdata
// files openrouter-images.json and openrouter-videos.json are rows copied
// byte for byte (descriptions shortened) out of GET /images/models and
// GET /videos/models as served on 2026-10-15, and the two *-endpoints files are
// whole GET /images/models/{id}/endpoints responses. Nothing in them is
// hand-written; the failures below are fakes of the TRANSPORT only.

func fixtureBytes(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type mediaReply struct {
	status int
	body   string
	// wait, when set, holds the reply until the request is cancelled.
	wait bool
}

// mediaRouter answers each path of the default service with its own reply and
// records every request, so a test can assert what was asked and by which verb.
type mediaRouter struct {
	t        *testing.T
	mu       sync.Mutex
	replies  map[string]mediaReply
	asked    map[string]int
	methods  map[string]bool
	headers  []string
	inflight atomic.Int32
	peak     atomic.Int32
}

func newMediaRouter(t *testing.T, replies map[string]mediaReply) *mediaRouter {
	return &mediaRouter{t: t, replies: replies, asked: map[string]int{}, methods: map[string]bool{}}
}

func (r *mediaRouter) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		now := r.inflight.Add(1)
		defer r.inflight.Add(-1)
		for {
			peak := r.peak.Load()
			if now <= peak || r.peak.CompareAndSwap(peak, now) {
				break
			}
		}
		r.mu.Lock()
		r.asked[request.URL.Path]++
		r.methods[request.Method] = true
		r.headers = append(r.headers, request.Header.Get("Authorization"))
		reply, ok := r.replies[request.URL.Path]
		r.mu.Unlock()
		if !ok {
			reply = mediaReply{status: http.StatusNotFound, body: "{}"}
		}
		if reply.wait {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return &http.Response{
			StatusCode: reply.status, Status: http.StatusText(reply.status), Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(reply.body)), Request: request,
		}, nil
	})}
}

func (r *mediaRouter) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked[path]
}

func (r *mediaRouter) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.asked {
		n += c
	}
	return n
}

const (
	generalPath = "/api/v1/models"
	imagesPath  = "/api/v1/images/models"
	videosPath  = "/api/v1/videos/models"
)

func okReply(body string) mediaReply { return mediaReply{status: http.StatusOK, body: body} }

func realReplies(t *testing.T) map[string]mediaReply {
	return map[string]mediaReply{
		generalPath: okReply(fixtureBytes(t, "openrouter-models.json")),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  okReply(fixtureBytes(t, "openrouter-videos.json")),
	}
}

func loadAt(t *testing.T, router *mediaRouter, dir string, now time.Time, mutate func(*Options)) *Catalog {
	t.Helper()
	options := Options{BaseURL: DefaultBaseURL, APIKey: "k", Dir: dir, HTTPClient: router.client(), Now: func() time.Time { return now }}
	if mutate != nil {
		mutate(&options)
	}
	return Load(context.Background(), options)
}

var day = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func TestDedicatedListingsAddTheVideoModelsTheGeneralListingNeverHad(t *testing.T) {
	router := newMediaRouter(t, realReplies(t))
	c := loadAt(t, router, t.TempDir(), day, nil)

	if !c.MediaState().Authoritative("video") || !c.MediaState().Authoritative("image") {
		t.Fatalf("both listings were read and the state says %+v", c.MediaState())
	}
	// grok-imagine is video-only in the dedicated listing and absent from the
	// general one: it must exist, draw video, and NOT be given a text answer.
	grok, ok := c.Model("x-ai/grok-imagine-video-1.5")
	if !ok {
		t.Fatal("a model published only by /videos/models is missing")
	}
	if len(grok.OutputModalities) != 1 || grok.OutputModalities[0] != "video" {
		t.Errorf("a pure generator answers %v, want [video] and no invented text", grok.OutputModalities)
	}
	if !c.Supports(grok.ID, "input", "image") || !c.Supports(grok.ID, "input", "text") {
		t.Errorf("a model with first_frame images and a prompt takes %v", grok.InputModalities)
	}
	if !grok.PriceUnknown || grok.PromptPrice != 0 || grok.CompletionPrice != 0 {
		t.Errorf("a per-second model was given a token price: %+v", grok)
	}
	// The cents-per-second family is read, in dollars, with its unit.
	want := []MediaPrice{
		{Billable: "image_input", Unit: "image", USD: 0.01},
		{Billable: "video_output", Unit: "second", Variant: "1080p", USD: 0.25},
		{Billable: "video_output", Unit: "second", Variant: "480p", USD: 0.08},
		{Billable: "video_output", Unit: "second", Variant: "720p", USD: 0.14},
	}
	if got := c.MediaPrices(grok.ID); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("grok prices = %+v\nwant %+v", got, want)
	}
	if params, ok := c.MediaParams(grok.ID); !ok || len(params.Resolutions) != 3 || params.Durations[0] != 1 || params.FrameImages[0] != "first_frame" {
		t.Errorf("grok params = %+v", params)
	}

	// runway publishes the other spelling of the same rate.
	if got := c.MediaPrices("runway/gen-4.5"); len(got) != 1 || got[0].USD != 0.12 || got[0].Unit != "second" || got[0].Variant != "" {
		t.Errorf("runway prices = %+v", got)
	}
	// SKUs whose unit this package cannot state are LEFT UNKNOWN, not guessed:
	// wan prices by duration_seconds_480p and seedance by video tokens.
	for _, id := range []string{"alibaba/wan-3.0", "bytedance/seedance-2.5", "google/veo-3.1"} {
		if got := c.MediaPrices(id); len(got) != 0 {
			t.Errorf("%s: an uninterpreted sku became a price: %+v", id, got)
		}
	}
	// A model that publishes no frame images takes a prompt and nothing else.
	avatar, _ := c.Model("heygen/avatar-iv")
	if c.Supports(avatar.ID, "input", "image") {
		t.Errorf("a model with no frame images was given image input: %v", avatar.InputModalities)
	}
	if got := len(c.ModelsWithOutput("video")); got != 6 {
		t.Errorf("video models = %d, want the 5 dedicated-only plus the 1 seedance both listings share", got)
	}
}

func TestADedicatedRowKeepsTheGeneralRowsOwnFactsAndTextCapability(t *testing.T) {
	general := `{"data":[
	 {"id":"google/gemini-3-pro-image","name":"Gemini Pro Image","context_length":65536,
	  "architecture":{"input_modalities":["text","image"],"output_modalities":["text","image"]},
	  "pricing":{"prompt":"0.000002","completion":"0.000012"},"supported_parameters":["max_tokens","reasoning"]},
	 {"id":"bytedance/seedance-2.5","name":"Seedance","architecture":{"input_modalities":["text","image","video","audio"],"output_modalities":["video"]},"pricing":{"prompt":"0","completion":"0"}},
	 {"id":"plain/chat","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`
	router := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(general),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  okReply(fixtureBytes(t, "openrouter-videos.json")),
	})
	c := loadAt(t, router, t.TempDir(), day, nil)

	gemini, _ := c.Model("google/gemini-3-pro-image")
	if !c.Supports(gemini.ID, "output", "text") || !c.Supports(gemini.ID, "output", "image") {
		t.Errorf("a chat model that also draws lost a side: %v", gemini.OutputModalities)
	}
	if gemini.PromptPrice != 0.000002 || gemini.CompletionPrice != 0.000012 || gemini.PriceUnknown || gemini.ContextLength != 65536 {
		t.Errorf("the general row's token facts changed: %+v", gemini)
	}
	if ok, known := c.SupportsParameter(gemini.ID, "reasoning"); !ok || !known {
		t.Error("the general row's published chat parameters were lost")
	}
	if params, ok := c.MediaParams(gemini.ID); !ok || len(params.AspectRatios) == 0 {
		t.Errorf("the dedicated parameters did not arrive: %+v", params)
	}
	if len(gemini.Dedicated) != 1 || gemini.Dedicated[0] != "image" {
		t.Errorf("provenance = %v", gemini.Dedicated)
	}

	// A video row inferred from dedicated fields must not displace the inputs
	// the general row actually published.
	seed, _ := c.Model("bytedance/seedance-2.5")
	for _, input := range []string{"text", "image", "video", "audio"} {
		if !c.Supports(seed.ID, "input", input) {
			t.Errorf("seedance lost its published %s input: %v", input, seed.InputModalities)
		}
	}
	if c.Supports(seed.ID, "output", "text") {
		t.Error("seedance was given text output")
	}
	// A token-priced chat model that no dedicated listing mentions is untouched.
	chat, _ := c.Model("plain/chat")
	if len(chat.Dedicated) != 0 || chat.Media != nil || len(chat.MediaPrices) != 0 || chat.PromptPrice != 0.000001 {
		t.Errorf("an unrelated chat row was altered: %+v", chat)
	}
}

func TestADedicatedGeneratorOverTextOnlyGeneralRowDoesNotDemoteIt(t *testing.T) {
	// The general listing says a model answers in text only; the dedicated one
	// says it draws. Both are true of the id: text must survive.
	general := `{"data":[{"id":"recraft/recraft-v4.1-flash","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}]}`
	router := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(general),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  mediaReply{status: 500, body: "no"},
	})
	c := loadAt(t, router, t.TempDir(), day, nil)
	row, _ := c.Model("recraft/recraft-v4.1-flash")
	if !c.Supports(row.ID, "output", "text") || !c.Supports(row.ID, "output", "image") {
		t.Errorf("merged outputs = %v, want text and image", row.OutputModalities)
	}
}

func TestImageGeneratorsOutsideTheChatCatalogAppearWithoutTextOrAGuessedPrice(t *testing.T) {
	router := newMediaRouter(t, realReplies(t))
	c := loadAt(t, router, t.TempDir(), day, nil)
	for _, id := range []string{"krea/k3-edit", "black-forest-labs/flux.2-klein-4b", "bytedance-seed/seedream-5-0-lite"} {
		row, ok := c.Model(id)
		if !ok {
			t.Fatalf("%s is missing", id)
		}
		if len(row.OutputModalities) != 1 || row.OutputModalities[0] != "image" {
			t.Errorf("%s outputs = %v", id, row.OutputModalities)
		}
		if !row.PriceUnknown || len(row.MediaPrices) != 0 || row.PromptPrice != 0 || row.RequestPrice != 0 {
			t.Errorf("%s was given a price the listing never published: %+v", id, row)
		}
	}
	klein, _ := c.MediaParams("black-forest-labs/flux.2-klein-4b")
	if klein.MaxInputReferences != 4 || klein.MaxOutputs != 1 || len(klein.AspectRatios) == 0 {
		t.Errorf("klein params = %+v", klein)
	}
	if recraft, _ := c.Model("recraft/recraft-v4.1-flash"); c.Supports(recraft.ID, "input", "image") {
		t.Errorf("a text-only image generator was given image input: %v", recraft.InputModalities)
	}
	// An image generator is not a pair of eyes: its text output is absent.
	for _, model := range c.ModelsWithOutput("text") {
		if len(model.Dedicated) > 0 && model.ID != "google/gemini-3-pro-image" {
			t.Errorf("%s was given a text output", model.ID)
		}
	}
}

func TestDedicatedDiscoveryOnlyAsksGETsOfTheOpenRouterSourcesOwnPaths(t *testing.T) {
	router := newMediaRouter(t, realReplies(t))
	loadAt(t, router, t.TempDir(), day, nil)
	router.mu.Lock()
	defer router.mu.Unlock()
	if len(router.methods) != 1 || !router.methods[http.MethodGet] {
		t.Errorf("methods used = %v, want GET only", router.methods)
	}
	if len(router.asked) != 3 || router.asked[generalPath] != 1 || router.asked[imagesPath] != 1 || router.asked[videosPath] != 1 {
		t.Errorf("requests = %v, want one each of the three listings and no per-model detail", router.asked)
	}
	for _, header := range router.headers {
		if header != "Bearer k" {
			t.Errorf("a request carried the authorization %q, want the catalog's own key", header)
		}
	}
	if router.peak.Load() > 2 {
		t.Errorf("%d dedicated requests were in flight at once, want at most 2", router.peak.Load())
	}
}

func TestOnlyTheKnownOpenRouterSourceReadsDedicatedListings(t *testing.T) {
	cases := map[string]func(*Options){
		"a custom endpoint":        func(o *Options) { o.BaseURL = "https://models.example.test/api/v1" },
		"another source":           func(o *Options) { o.Source = "someone-else" },
		"a lookalike host":         func(o *Options) { o.BaseURL = "https://openrouter.ai.evil.test/api/v1" },
		"the opt-out":              func(o *Options) { o.NoDedicatedMedia = true },
		"a different path prefix":  func(o *Options) { o.BaseURL = "https://openrouter.ai/api/v2" },
		"codex's compartment name": func(o *Options) { o.Source = "codex" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var paths []string
			var mu sync.Mutex
			client := catalogClient(t, 200, fixtureBytes(t, "openrouter-models.json"), func(r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
			})
			options := Options{BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: client, Now: func() time.Time { return day }}
			mutate(&options)
			c := Load(context.Background(), options)
			if (c.MediaState() != MediaState{}) {
				t.Errorf("media state = %+v, want none", c.MediaState())
			}
			for _, path := range paths {
				if strings.Contains(path, "images") || strings.Contains(path, "videos") {
					t.Errorf("%s asked %s", name, path)
				}
			}
		})
	}
	t.Run("the openrouter source id on the default base is eligible", func(t *testing.T) {
		router := newMediaRouter(t, realReplies(t))
		c := loadAt(t, router, t.TempDir(), day, func(o *Options) { o.Source = "openrouter" })
		if !c.MediaState().Authoritative("video") {
			t.Errorf("state = %+v", c.MediaState())
		}
	})
}

func TestAFailedDedicatedListingNeverPublishesNoneAsProof(t *testing.T) {
	for name, reply := range map[string]mediaReply{
		"a server error": {status: 503, body: "down"},
		"not json":       okReply("<html>"),
		"an empty list":  okReply(`{"data":[]}`),
		"no list":        okReply(`{}`),
		"all rows bad":   okReply(`{"data":[1,"x",{"id":""},{"id":"nofields"}]}`),
		"a not-found":    {status: 404, body: "{}"},
	} {
		t.Run(name, func(t *testing.T) {
			replies := realReplies(t)
			replies[videosPath] = reply
			router := newMediaRouter(t, replies)
			c := loadAt(t, router, t.TempDir(), day, nil)

			state := c.MediaState()
			if state.Videos.Known || state.Authoritative("video") {
				t.Errorf("%s was read as a video catalog: %+v", name, state.Videos)
			}
			if !state.Authoritative("image") {
				t.Errorf("a failed video listing took the image listing with it: %+v", state)
			}
			// The general rows, including its own video model, are all still here.
			if _, ok := c.Model("bytedance/seedance-2.5"); !ok || len(c.ModelsWithOutput("text")) == 0 {
				t.Error("a failed dedicated listing cost the general catalog its rows")
			}
			if _, ok := c.Model("krea/k3-edit"); !ok {
				t.Error("the listing that landed was thrown away with the one that did not")
			}
		})
	}
}

func TestAFailedRefreshKeepsThePreviousDedicatedRowsAndSaysTheyAreStale(t *testing.T) {
	dir := t.TempDir()
	first := newMediaRouter(t, realReplies(t))
	loadAt(t, first, dir, day, nil)

	replies := realReplies(t)
	replies[videosPath] = mediaReply{status: 500, body: "x"}
	failing := newMediaRouter(t, replies)
	later := day.Add(2 * time.Hour)
	c := loadAt(t, failing, dir, later, func(o *Options) { o.Refresh = true })

	state := c.MediaState()
	if !state.Videos.Known || !state.Videos.Stale || state.Authoritative("video") {
		t.Errorf("video state = %+v, want known, stale, and not authoritative", state.Videos)
	}
	if !state.Videos.FetchedAt.Equal(day) {
		t.Errorf("the previous rows were re-dated %v, want %v", state.Videos.FetchedAt, day)
	}
	if _, ok := c.Model("x-ai/grok-imagine-video-1.5"); !ok {
		t.Error("a transient failure erased the video models already known")
	}
	if got := c.MediaPrices("x-ai/grok-imagine-video-1.5"); len(got) != 4 {
		t.Errorf("the previous prices were lost: %+v", got)
	}
	if !state.Authoritative("image") {
		t.Errorf("the image listing that did land should be current: %+v", state.Images)
	}

	// And the next plain load serves the same stale standing from disk.
	again := loadAt(t, newMediaRouter(t, nil), dir, later.Add(time.Minute), nil)
	if s := again.MediaState(); !s.Videos.Known || !s.Videos.Stale {
		t.Errorf("the persisted standing = %+v", s.Videos)
	}
}

func TestADedicatedListingOlderThanTheTTLIsStaleEvenWhenNothingFailed(t *testing.T) {
	dir := t.TempDir()
	loadAt(t, newMediaRouter(t, realReplies(t)), dir, day, nil)
	c := loadAt(t, newMediaRouter(t, nil), dir, day.Add(TTL-time.Hour), nil)
	if !c.MediaState().Authoritative("video") {
		t.Errorf("an hour inside the TTL is stale: %+v", c.MediaState())
	}
	// Past the TTL with the network gone the whole cache degrades together.
	c = loadAt(t, newMediaRouter(t, nil), dir, day.Add(TTL+time.Hour), nil)
	if s := c.MediaState(); s.Authoritative("video") || !s.Videos.Known || !s.Videos.Stale {
		t.Errorf("a day-old dedicated listing = %+v, want known and stale", s.Videos)
	}
}

func TestADedicatedListingThatTimesOutIsAFailureNotAnAbsence(t *testing.T) {
	replies := realReplies(t)
	replies[videosPath] = mediaReply{wait: true}
	router := newMediaRouter(t, replies)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	c := Load(ctx, Options{BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: router.client(), Now: func() time.Time { return day }})
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("the load waited %v on a listing that never answered", took)
	}
	if c.MediaState().Videos.Known || c.MediaState().Authoritative("video") {
		t.Errorf("a timeout counted as a video catalog: %+v", c.MediaState())
	}
	if router.inflight.Load() != 0 {
		t.Error("a media request was left running after the load returned")
	}
}

func TestOversizeAndOverlongDedicatedListingsAreRefusedWholeAndBeforeDecoding(t *testing.T) {
	huge := `{"data":[{"id":"a/b","supported_resolutions":["480p"],"description":"` + strings.Repeat("x", MaxMediaCatalogBytes) + `"}]}`
	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i <= MaxMediaRows; i++ {
		if i > 0 {
			rows.WriteString(",")
		}
		fmt.Fprintf(&rows, `{"id":"vendor/video-%d","supported_resolutions":["480p"]}`, i)
	}
	rows.WriteString(`]}`)
	for name, body := range map[string]string{"oversize": huge, "overlong": rows.String()} {
		t.Run(name, func(t *testing.T) {
			replies := realReplies(t)
			replies[videosPath] = okReply(body)
			c := loadAt(t, newMediaRouter(t, replies), t.TempDir(), day, nil)
			if c.MediaState().Videos.Known {
				t.Error("a refused listing was accepted")
			}
			if _, ok := c.Model("vendor/video-0"); ok {
				t.Error("a listing past its bound was truncated and kept instead of refused")
			}
		})
	}
	t.Run("just inside the row bound is accepted", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i := 0; i < MaxMediaRows; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"id":"vendor/video-%d","supported_resolutions":["480p"]}`, i)
		}
		b.WriteString(`]}`)
		replies := realReplies(t)
		replies[videosPath] = okReply(b.String())
		c := loadAt(t, newMediaRouter(t, replies), t.TempDir(), day, nil)
		if !c.MediaState().Videos.Known {
			t.Error("a listing at the bound was refused")
		}
	})
}

func TestDuplicateAndMalformedDedicatedRowsCostOnlyThemselves(t *testing.T) {
	body := `{"data":[
	 {"id":"a/first","name":"First","supported_resolutions":["480p"],"pricing_skus":{"cents_per_video_output_second_480p":"2"}},
	 {"id":"a/first","name":"Second copy","supported_resolutions":["720p"],"pricing_skus":{"cents_per_video_output_second_480p":"9"}},
	 {"id":"a/drift","supported_resolutions":"480p","supported_durations":{"x":1},"pricing_skus":"free","supported_frame_images":["first_frame"]},
	 {"id":"a/numbers","supported_durations":[2.5,4,0,-1,5],"pricing_skus":{"cents_per_image_input":2,"cents_per_video_output_second_480p":"-3","cents_per_video_output_second_weird":"1","cents_per_video_output_second_continuation_2k":"1","cents_per_video_output_second_NaN":"NaN"}},
	 "not an object",
	 {"name":"no id","supported_resolutions":["480p"]},
	 {"id":"a/last","supported_aspect_ratios":["16:9"]}]}`
	replies := realReplies(t)
	replies[videosPath] = okReply(body)
	c := loadAt(t, newMediaRouter(t, replies), t.TempDir(), day, nil)

	first, _ := c.Model("a/first")
	if first.Name != "First" || len(c.MediaPrices("a/first")) != 1 || c.MediaPrices("a/first")[0].USD != 0.02 {
		t.Errorf("a duplicate id replaced the first row: %+v", first)
	}
	if _, ok := c.Model("a/drift"); !ok {
		t.Error("a row whose fields drifted in shape was dropped instead of kept without them")
	}
	if params, _ := c.MediaParams("a/drift"); len(params.FrameImages) != 1 || len(params.Resolutions) != 0 || len(params.Durations) != 0 {
		t.Errorf("drifted fields were not isolated: %+v", params)
	}
	numbers := c.MediaPrices("a/numbers")
	if len(numbers) != 1 || numbers[0].Billable != "image_input" || numbers[0].USD != 0.02 {
		t.Errorf("only the well-formed numeric sku should be priced, got %+v", numbers)
	}
	if params, _ := c.MediaParams("a/numbers"); fmt.Sprint(params.Durations) != "[4 5]" {
		t.Errorf("durations = %v, want only whole positive seconds", params.Durations)
	}
	if _, ok := c.Model("a/last"); !ok {
		t.Error("a row after the malformed ones was lost")
	}
}

func TestAPureVideoListingIsDiscoveredOverATinyGeneralCatalog(t *testing.T) {
	tiny := `{"data":[{"id":"only/chat","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}]}`
	replies := realReplies(t)
	replies[generalPath] = okReply(tiny)
	c := loadAt(t, newMediaRouter(t, replies), t.TempDir(), day, nil)
	if len(c.ModelsWithOutput("video")) != 6 {
		t.Errorf("video models over a one-row general catalog = %d, want the 6 dedicated rows", len(c.ModelsWithOutput("video")))
	}
	if _, ok := c.Model("only/chat"); !ok {
		t.Error("the general row vanished")
	}
}

func TestTheDedicatedCacheRoundTripsAndServesWithoutAnyRequest(t *testing.T) {
	dir := t.TempDir()
	loadAt(t, newMediaRouter(t, realReplies(t)), dir, day, nil)

	quiet := newMediaRouter(t, nil)
	c := loadAt(t, quiet, dir, day.Add(time.Hour), nil)
	if quiet.total() != 0 {
		t.Errorf("a fresh cache made %d requests", quiet.total())
	}
	if !c.MediaState().Authoritative("video") || len(c.MediaPrices("x-ai/grok-imagine-video-1.5")) != 4 {
		t.Errorf("the cache lost the dedicated facts: %+v", c.MediaState())
	}
	if row, _ := c.Model("anthropic/claude-opus-5"); row.PromptPrice != 0.000005 {
		t.Errorf("the general facts changed across the cache: %+v", row)
	}
	if recalled := Recall(Options{BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day }}); len(recalled.ModelsWithOutput("video")) == 0 {
		t.Error("Recall does not see the dedicated rows")
	}
}

func TestACacheWrittenBeforeDedicatedListingsStillLoadsAndIsToppedUpAlone(t *testing.T) {
	dir := t.TempDir()
	// An older build's cache: general rows only, none of the new fields.
	legacy := `{"fetched_at":"` + day.Format(time.RFC3339) + `","priced":true,"models":[{"id":"only/chat","input_modalities":["text"],"output_modalities":["text"],"prompt_price":0.000001}]}`
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	options := func(router *mediaRouter, at time.Time) Options {
		return Options{BaseURL: DefaultBaseURL, Dir: dir, HTTPClient: router.client(), Now: func() time.Time { return at }}
	}

	// An eager load keeps the old promise: a fresh cache costs no request.
	quiet := newMediaRouter(t, realReplies(t))
	c := Load(context.Background(), options(quiet, day.Add(time.Hour)))
	if quiet.total() != 0 {
		t.Errorf("a fresh legacy cache made %d requests", quiet.total())
	}
	if c.MediaState().Videos.Known || c.MediaState().Authoritative("video") {
		t.Errorf("a cache that never read the video listing claims it did: %+v", c.MediaState())
	}
	if !c.MediaState().Videos.due {
		t.Error("an unread listing is not marked as worth a top-up")
	}

	// The lazy warm tops up the two dedicated listings and NOT the general one.
	topUp := newMediaRouter(t, realReplies(t))
	lazy := LoadLazy(context.Background(), options(topUp, day.Add(time.Hour)))
	deadline := time.After(5 * time.Second)
	// Wait for the top-up to be PERSISTED, not merely asked: closing the catalog
	// between the two cancels the write, which is the law, and not the test.
	for {
		if cached, ok := readCache(cachePath(dir, "", DefaultBaseURL), "", DefaultBaseURL); ok && cached.Media != nil && !cached.Media.Videos.FetchedAt.IsZero() {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("the lazy warm never topped up: %v", topUp.asked)
		case <-time.After(10 * time.Millisecond):
		}
	}
	lazy.Close()
	if topUp.count(generalPath) != 0 {
		t.Errorf("a media top-up refetched the general listing %d times", topUp.count(generalPath))
	}
	after := Load(context.Background(), options(newMediaRouter(t, nil), day.Add(2*time.Hour)))
	if !after.MediaState().Authoritative("video") {
		t.Errorf("the top-up was not persisted: %+v", after.MediaState())
	}
	if _, ok := after.Model("only/chat"); !ok {
		t.Error("the top-up lost the general rows")
	}
	// The file is still readable by the old reader: only additive fields.
	raw, _ := os.ReadFile(filepath.Join(dir, cacheName))
	var old struct {
		FetchedAt time.Time `json:"fetched_at"`
		Models    []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || len(old.Models) < 2 {
		t.Errorf("the cache no longer reads as the old shape: %v %d", err, len(old.Models))
	}
}

func TestAFailedTopUpIsNotRetriedUntilTheIntervalPasses(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"fetched_at":"` + day.Format(time.RFC3339) + `","priced":true,"models":[{"id":"only/chat","input_modalities":["text"],"output_modalities":["text"]}]}`
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	down := newMediaRouter(t, map[string]mediaReply{imagesPath: {status: 500}, videosPath: {status: 500}})
	run := func(router *mediaRouter, at time.Time) *rows {
		resolved, _ := load(context.Background(), Options{
			BaseURL: DefaultBaseURL, Dir: dir, HTTPClient: router.client(), Now: func() time.Time { return at }, mediaOnly: true,
		})
		return resolved
	}
	first := run(down, day.Add(time.Minute))
	if down.total() != 2 || first.media.Videos.Known {
		t.Fatalf("first attempt: %d requests, state %+v", down.total(), first.media)
	}
	if first.media.due() {
		t.Error("a listing that just failed is already due again")
	}
	// Persisted: a later reader inside the interval is not due, one after is.
	soon := cachedRows(mustCache(t, dir), Options{BaseURL: DefaultBaseURL}, day.Add(5*time.Minute))
	if soon.media.due() {
		t.Error("the retry throttle was not persisted")
	}
	late := cachedRows(mustCache(t, dir), Options{BaseURL: DefaultBaseURL}, day.Add(time.Minute+mediaRetry))
	if !late.media.due() {
		t.Error("a failed listing is never retried")
	}
	if got := mustCache(t, dir).Models; len(got) != 1 {
		t.Errorf("a failed top-up changed the general rows: %d", len(got))
	}
}

func mustCache(t *testing.T, dir string) cache {
	t.Helper()
	cached, ok := readCache(cachePath(dir, "", DefaultBaseURL), "", DefaultBaseURL)
	if !ok {
		t.Fatal("no cache")
	}
	return cached
}

func TestRememberKeepsWhatTheDedicatedListingsAlreadyProved(t *testing.T) {
	dir := t.TempDir()
	loadAt(t, newMediaRouter(t, realReplies(t)), dir, day, nil)
	err := Remember(Options{BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) }}, []Model{
		{ID: "probe/one", InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := Recall(Options{BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) }})
	if _, ok := c.Model("x-ai/grok-imagine-video-1.5"); !ok {
		t.Error("a probe write erased the dedicated rows")
	}
	if !c.MediaState().Authoritative("video") {
		t.Errorf("a probe write erased the dedicated standing: %+v", c.MediaState())
	}
}

func TestImageEndpointPricesAreTypedPerUnitAndPerProvider(t *testing.T) {
	klein := "/api/v1/images/models/black-forest-labs/flux.2-klein-4b/endpoints"
	seed := "/api/v1/images/models/bytedance-seed/seedream-5-0-lite/endpoints"
	router := newMediaRouter(t, map[string]mediaReply{
		klein: okReply(fixtureBytes(t, "openrouter-image-klein-endpoints.json")),
		seed:  okReply(fixtureBytes(t, "openrouter-image-seedream-endpoints.json")),
	})
	options := Options{BaseURL: DefaultBaseURL, APIKey: "k", HTTPClient: router.client()}

	prices, err := ImageEndpointPrices(context.Background(), options, "black-forest-labs/flux.2-klein-4b")
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 1 || prices[0] != (MediaPrice{Billable: "image_output", Unit: "megapixel", Provider: "black-forest-labs", USD: 0.014}) {
		t.Errorf("klein = %+v", prices)
	}
	prices, err = ImageEndpointPrices(context.Background(), options, "bytedance-seed/seedream-5-0-lite")
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 {
		t.Fatalf("seedream = %+v", prices)
	}
	// A published zero is a known zero, kept; and it is a different unit from
	// the per-megapixel rate above, so the two are never comparable by number.
	if prices[0].Billable != "image_input" || prices[0].USD != 0 || prices[1] != (MediaPrice{Billable: "image_output", Unit: "image", Provider: "seed", USD: 0.035}) {
		t.Errorf("seedream = %+v", prices)
	}
	if router.total() != 2 || len(router.methods) != 1 || !router.methods[http.MethodGet] {
		t.Errorf("each lookup should be one GET, saw %v", router.asked)
	}
}

func TestImageEndpointPricesRefuseWhatTheyCannotVouchFor(t *testing.T) {
	router := newMediaRouter(t, map[string]mediaReply{
		"/api/v1/images/models/a/none/endpoints":  okReply(`{"endpoints":[{"provider_slug":"p","pricing":[{"billable":"output_image","unit":"token","cost_usd":1},{"billable":"output_image","unit":"image","cost_usd":-1}]}]}`),
		"/api/v1/images/models/a/empty/endpoints": okReply(`{"endpoints":[]}`),
		"/api/v1/images/models/a/big/endpoints":   okReply(`{"endpoints":[],"pad":"` + strings.Repeat("x", MaxEndpointBytes) + `"}`),
		"/api/v1/images/models/a/down/endpoints":  {status: 502, body: "x"},
		"/api/v1/images/models/a/many/endpoints":  okReply(`{"endpoints":[` + strings.Repeat(`{},`, MaxEndpointRows) + `{}]}`),
	})
	options := Options{BaseURL: DefaultBaseURL, HTTPClient: router.client()}
	for _, id := range []string{"a/none", "a/empty", "a/big", "a/down", "a/many"} {
		if prices, err := ImageEndpointPrices(context.Background(), options, id); err == nil || prices != nil {
			t.Errorf("%s answered %+v, %v; an unreadable price must be an error", id, prices, err)
		}
	}
	before := router.total()
	for _, id := range []string{"", "../etc", "a/../b", "a//b", "a/b?x=1", "a/b#c", strings.Repeat("a", 200)} {
		if _, err := ImageEndpointPrices(context.Background(), options, id); err == nil {
			t.Errorf("%q was accepted as a model id", id)
		}
	}
	custom := Options{BaseURL: "https://models.example.test/api/v1", HTTPClient: router.client()}
	if _, err := ImageEndpointPrices(context.Background(), custom, "a/none"); err == nil {
		t.Error("a custom endpoint was asked for image prices")
	}
	if router.total() != before {
		t.Error("a refused lookup still made a request")
	}
}

func TestExistingGeneralBehaviorIsUnchangedByTheDedicatedListings(t *testing.T) {
	withMedia := loadAt(t, newMediaRouter(t, realReplies(t)), t.TempDir(), day, nil)
	without := loadAt(t, newMediaRouter(t, realReplies(t)), t.TempDir(), day, func(o *Options) { o.NoDedicatedMedia = true })
	for _, model := range without.ModelsNow() {
		merged, ok := withMedia.Model(model.ID)
		if !ok {
			t.Fatalf("%s vanished", model.ID)
		}
		if merged.PromptPrice != model.PromptPrice || merged.CompletionPrice != model.CompletionPrice || merged.PriceUnknown != model.PriceUnknown ||
			merged.ContextLength != model.ContextLength || merged.RequestPrice != model.RequestPrice || merged.CacheReadPrice != model.CacheReadPrice {
			t.Errorf("%s: token economics changed: %+v vs %+v", model.ID, merged, model)
		}
		if fmt.Sprint(merged.Parameters) != fmt.Sprint(model.Parameters) || merged.Reasoning.Known != model.Reasoning.Known {
			t.Errorf("%s: chat parameters changed", model.ID)
		}
		if hasModality(model.OutputModalities, "text") && !hasModality(merged.OutputModalities, "text") {
			t.Errorf("%s lost its text output", model.ID)
		}
		for _, input := range model.InputModalities {
			if !hasModality(merged.InputModalities, input) {
				t.Errorf("%s lost its %s input", model.ID, input)
			}
		}
	}
}

// ---- Refresh must REPLACE a dedicated compartment, never accumulate into it. ----

func videoListing(rows ...string) mediaReply {
	return okReply(`{"data":[` + strings.Join(rows, ",") + `]}`)
}

func videoRowJSON(id string, centsPerSecond string) string {
	return fmt.Sprintf(`{"id":%q,"supported_resolutions":["480p"],"pricing_skus":{"cents_per_video_output_second_480p":%q}}`, id, centsPerSecond)
}

const hybridGeneral = `{"data":[
 {"id":"hybrid/draws-and-talks","context_length":1000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["max_tokens"]},
 {"id":"other/chat","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}]}`

func mediaOptions(router *mediaRouter, dir string, at time.Time) Options {
	return Options{BaseURL: DefaultBaseURL, Dir: dir, HTTPClient: router.client(), Now: func() time.Time { return at }}
}

func TestAFreshDedicatedRefreshRetiresRemovedModelsAndReplacesPrices(t *testing.T) {
	dir := t.TempDir()
	first := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(hybridGeneral),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  videoListing(videoRowJSON("vendor/retired", "2"), videoRowJSON("vendor/repriced", "2")),
	})
	loadAt(t, first, dir, day, nil)

	second := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(hybridGeneral),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  videoListing(videoRowJSON("vendor/repriced", "3"), videoRowJSON("vendor/new", "5")),
	})
	c := loadAt(t, second, dir, day.Add(time.Hour), func(o *Options) { o.Refresh = true })
	assertRetiredAndRepriced(t, c)

	// And the same truth survives a trip through the disk.
	assertRetiredAndRepriced(t, loadAt(t, newMediaRouter(t, nil), dir, day.Add(2*time.Hour), nil))
}

func assertRetiredAndRepriced(t *testing.T, c *Catalog) {
	t.Helper()
	if _, ok := c.Model("vendor/retired"); ok {
		t.Error("a model the fresh listing no longer publishes is still offered")
	}
	prices := c.MediaPrices("vendor/repriced")
	if len(prices) != 1 || prices[0].USD != 0.03 {
		t.Errorf("a repriced model has %+v, want exactly the new 3 cents", prices)
	}
	if _, ok := c.Model("vendor/new"); !ok {
		t.Error("a newly published model is missing")
	}
}

func TestALazyTopUpOfAnOlderCacheRetiresAndRepricesThroughTheDisk(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"fetched_at":"` + day.Format(time.RFC3339) + `","priced":true,"models":[` +
		`{"id":"hybrid/draws-and-talks","input_modalities":["text"],"output_modalities":["text"],"prompt_price":0.000001,"completion_price":0.000002}]}`
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	waitPersisted := func(want time.Time) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			if cached, ok := readCache(cachePath(dir, "", DefaultBaseURL), "", DefaultBaseURL); ok && cached.Media != nil && cached.Media.Videos.FetchedAt.Equal(want) {
				return
			}
			select {
			case <-deadline:
				t.Fatalf("the top-up for %v was never persisted", want)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	lazyAt := func(router *mediaRouter, at time.Time, persisted time.Time) {
		lazy := LoadLazy(context.Background(), mediaOptions(router, dir, at))
		waitPersisted(persisted)
		lazy.Close()
		if router.count(generalPath) != 0 {
			t.Errorf("a media top-up refetched the general listing")
		}
	}
	t1, t2, t3 := day.Add(time.Hour), day.Add(2*time.Hour), day.Add(3*time.Hour)

	lazyAt(newMediaRouter(t, map[string]mediaReply{
		imagesPath: okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath: videoListing(videoRowJSON("vendor/retired", "2"), videoRowJSON("vendor/repriced", "2")),
	}), t1, t1)

	// A failed attempt in between: the old rows stay, and say they are stale.
	down := newMediaRouter(t, map[string]mediaReply{imagesPath: {status: 500}, videosPath: {status: 500}})
	if _, err := load(context.Background(), func() Options { o := mediaOptions(down, dir, t2); o.mediaOnly = true; return o }()); err != nil {
		t.Fatal(err)
	}
	between := Load(context.Background(), mediaOptions(newMediaRouter(t, nil), dir, t2.Add(time.Minute)))
	if s := between.MediaState(); !s.Videos.Known || !s.Videos.Stale || !s.Videos.FetchedAt.Equal(t1) {
		t.Fatalf("after a failed top-up the standing is %+v", s.Videos)
	}
	if _, ok := between.Model("vendor/retired"); !ok {
		t.Error("a transient failure dropped a model it had no evidence against")
	}

	// The retry interval passes and the listing answers differently.
	lazyAt(newMediaRouter(t, map[string]mediaReply{
		imagesPath: okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath: videoListing(videoRowJSON("vendor/repriced", "3"), videoRowJSON("vendor/new", "5")),
	}), t3, t3)
	assertRetiredAndRepriced(t, Load(context.Background(), mediaOptions(newMediaRouter(t, nil), dir, t3.Add(time.Minute))))
}

func TestARetiredGeneratorHandsAHybridBackExactlyItsChatCapabilities(t *testing.T) {
	dir := t.TempDir()
	hybridImage := `{"id":"hybrid/draws-and-talks","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},"supported_parameters":{"aspect_ratio":{"type":"enum","values":["1:1"]}}}`
	first := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(hybridGeneral),
		imagesPath:  okReply(`{"data":[` + hybridImage + `]}`),
		videosPath:  videoListing(videoRowJSON("vendor/v", "2")),
	})
	c := loadAt(t, first, dir, day, nil)
	if !c.Supports("hybrid/draws-and-talks", "output", "image") || !c.Supports("hybrid/draws-and-talks", "output", "text") {
		t.Fatalf("the hybrid was not merged: %v", mustModel(t, c, "hybrid/draws-and-talks").OutputModalities)
	}

	// The image listing now publishes a different model entirely.
	second := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(hybridGeneral),
		imagesPath:  okReply(`{"data":[{"id":"vendor/other-painter","architecture":{"input_modalities":["text"],"output_modalities":["image"]}}]}`),
		videosPath:  videoListing(videoRowJSON("vendor/v", "2")),
	})
	for name, c := range map[string]*Catalog{
		"in the refresh":  loadAt(t, second, dir, day.Add(time.Hour), func(o *Options) { o.Refresh = true }),
		"after reopening": loadAt(t, newMediaRouter(t, nil), dir, day.Add(2*time.Hour), nil),
	} {
		h := mustModel(t, c, "hybrid/draws-and-talks")
		if fmt.Sprint(h.OutputModalities) != "[text]" || fmt.Sprint(h.InputModalities) != "[text]" {
			t.Errorf("%s: modalities in/out = %v/%v, want the chat row's own", name, h.InputModalities, h.OutputModalities)
		}
		if h.Media != nil || len(h.Dedicated) != 0 || len(h.MediaPrices) != 0 {
			t.Errorf("%s: a retired generator left media facts behind: %+v", name, h)
		}
		if h.PromptPrice != 0.000001 || h.CompletionPrice != 0.000002 || h.ContextLength != 1000 || len(h.Parameters) != 1 {
			t.Errorf("%s: the chat facts changed: %+v", name, h)
		}
		if _, ok := c.Model("vendor/other-painter"); !ok {
			t.Errorf("%s: the new generator is missing", name)
		}
	}
}

func mustModel(t *testing.T, c *Catalog, id string) Model {
	t.Helper()
	model, ok := c.Model(id)
	if !ok {
		t.Fatalf("%s is missing", id)
	}
	return model
}

func TestACacheThatMergedWithoutKeepingTheGeneralRowsRefetchesThemBoundedly(t *testing.T) {
	dir := t.TempDir()
	// What the first version of this feature wrote: merged Models, dedicated
	// rows, no general snapshot. The painter below is a merged-in generator
	// that must not survive, and the chat row's text cannot be proven from it.
	merged := `{"fetched_at":"` + day.Format(time.RFC3339) + `","priced":true,"models":[` +
		`{"id":"hybrid/draws-and-talks","input_modalities":["text","image"],"output_modalities":["text","image"],"dedicated":["image"]},` +
		`{"id":"vendor/retired","input_modalities":["text"],"output_modalities":["video"],"dedicated":["video"]}],` +
		`"media":{"images":{"fetched_at":"` + day.Format(time.RFC3339) + `","tried_at":"` + day.Format(time.RFC3339) + `"},"videos":{"fetched_at":"` + day.Format(time.RFC3339) + `","tried_at":"` + day.Format(time.RFC3339) + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(merged), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := generalOf(mustCache(t, dir)); ok {
		t.Fatal("a snapshot-less merged cache claims to know its general rows")
	}
	router := newMediaRouter(t, map[string]mediaReply{
		generalPath: okReply(hybridGeneral),
		imagesPath:  okReply(fixtureBytes(t, "openrouter-images.json")),
		videosPath:  videoListing(videoRowJSON("vendor/v", "2")),
	})
	options := mediaOptions(router, dir, day.Add(30*time.Minute))
	options.mediaOnly = true
	if _, err := load(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if router.count(generalPath) != 1 || router.count(imagesPath) != 1 || router.count(videosPath) != 1 {
		t.Errorf("recovery asked %v, want one of each listing", router.asked)
	}
	c := Recall(Options{BaseURL: DefaultBaseURL, Dir: dir, Now: func() time.Time { return day.Add(time.Hour) }})
	if _, ok := c.Model("vendor/retired"); ok {
		t.Error("a retired generator from the unrecoverable cache survived the refetch")
	}
	if h := mustModel(t, c, "hybrid/draws-and-talks"); len(h.Dedicated) != 0 || fmt.Sprint(h.OutputModalities) != "[text]" {
		t.Errorf("the hybrid was not rebuilt from the fresh general row: %+v", h)
	}
	if _, ok := generalOf(mustCache(t, dir)); !ok {
		t.Error("the recovered cache still does not keep its general rows")
	}
}

func TestAnUnreadableOrUnsupportedSkuIsUnknownNotAFreeModel(t *testing.T) {
	body := videoListing(
		`{"id":"vendor/blank","supported_resolutions":["480p"],"pricing_skus":{"cents_per_video_output_second_480p":"","cents_per_image_input":"abc"}}`,
		`{"id":"vendor/tokens","supported_resolutions":["480p"],"pricing_skus":{"video_tokens":"0.00001","duration_seconds":"0.05"}}`,
		`{"id":"vendor/null","supported_resolutions":["480p"],"pricing_skus":null}`,
		`{"id":"vendor/free","supported_resolutions":["480p"],"pricing_skus":{"cents_per_image_input":"0"}}`)
	replies := realReplies(t)
	replies[videosPath] = body
	c := loadAt(t, newMediaRouter(t, replies), t.TempDir(), day, nil)
	for _, id := range []string{"vendor/blank", "vendor/tokens", "vendor/null"} {
		row := mustModel(t, c, id)
		if len(row.MediaPrices) != 0 || !row.PriceUnknown {
			t.Errorf("%s: an unreadable price became %+v (unknown=%v)", id, row.MediaPrices, row.PriceUnknown)
		}
	}
	// Only an explicit published zero is a zero.
	if free := c.MediaPrices("vendor/free"); len(free) != 1 || free[0].USD != 0 || free[0].Billable != "image_input" {
		t.Errorf("a published zero = %+v", free)
	}
}

func TestPublishedPriceNeedsAFiniteUnitPriceAndNeverATokenZero(t *testing.T) {
	cases := []struct {
		name   string
		model  Model
		output string
		want   bool
	}{
		{"video per second", Model{MediaPrices: []MediaPrice{{Billable: "video_output", Unit: "second", USD: 0.02}}}, "video", true},
		{"video published zero is a published price", Model{MediaPrices: []MediaPrice{{Billable: "video_output", Unit: "second", USD: 0}}}, "video", true},
		{"video token zero", Model{PromptPrice: 0, CompletionPrice: 0}, "video", false},
		{"video request price is not a per-second price", Model{RequestPrice: 0.5}, "video", false},
		{"video input still price only", Model{MediaPrices: []MediaPrice{{Billable: "image_input", Unit: "image", USD: 0.01}}}, "video", false},
		{"image per megapixel", Model{MediaPrices: []MediaPrice{{Billable: "image_output", Unit: "megapixel", USD: 0.014}}}, "image", true},
		{"image request price", Model{RequestPrice: 0.04}, "image", true},
		{"image zero request", Model{RequestPrice: 0}, "image", false},
		{"image negative", Model{MediaPrices: []MediaPrice{{Billable: "image_output", Unit: "image", USD: -1}}}, "image", false},
		{"unknown modality", Model{RequestPrice: 1}, "music", false},
	}
	for _, c := range cases {
		if got := c.model.PublishedPrice(c.output); got != c.want {
			t.Errorf("%s: PublishedPrice = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMediaStateSaysWhetherTheDedicatedListingsApplyAtAll(t *testing.T) {
	custom := Load(context.Background(), Options{BaseURL: "https://x.example/v1", Dir: t.TempDir(),
		HTTPClient: catalogClient(t, 200, `{"data":[{"id":"a/b"}]}`, nil)})
	if custom.MediaState().Applicable {
		t.Error("a custom endpoint claims dedicated listings")
	}
	offline := Load(context.Background(), Options{BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: catalogClient(t, 503, "x", nil)})
	if s := offline.MediaState(); !s.Applicable || s.Authoritative("video") || s.Videos.Known {
		t.Errorf("an unread OpenRouter catalog = %+v, want applicable and unconfirmed", s)
	}
}
