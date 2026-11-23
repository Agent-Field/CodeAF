package config

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
)

// A fake OpenRouter: the general, image and video listings of the default base
// answered from strings, with every request counted. Nothing here is the
// network, and nothing is a per-model request.
type routeServer struct {
	mu                    sync.Mutex
	general, images, vids string
	failVideos            bool
	requests              []string
}

func (s *routeServer) client() *http.Client {
	return &http.Client{Transport: configRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		s.mu.Lock()
		s.requests = append(s.requests, request.Method+" "+request.URL.Path)
		s.mu.Unlock()
		body, status := "{}", http.StatusNotFound
		switch request.URL.Path {
		case "/api/v1/models":
			body, status = s.general, http.StatusOK
		case "/api/v1/images/models":
			body, status = s.images, http.StatusOK
		case "/api/v1/videos/models":
			body, status = s.vids, http.StatusOK
			if s.failVideos {
				body, status = "down", http.StatusServiceUnavailable
			}
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
}

func (s *routeServer) load(t *testing.T, dir string, at time.Time, refresh bool) *catalog.Catalog {
	t.Helper()
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL: catalog.DefaultBaseURL, Dir: dir, HTTPClient: s.client(), Refresh: refresh, Now: func() time.Time { return at },
	})
}

const routeGeneral = `{"data":[{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}]}`

func imageListing(ids ...string) string {
	rows := make([]string, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, `{"id":"`+id+`","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"supported_parameters":{}}`)
	}
	return `{"data":[` + strings.Join(rows, ",") + `]}`
}

func videoListing(rows ...string) string { return `{"data":[` + strings.Join(rows, ",") + `]}` }

func pricedVideo(id string) string {
	return `{"id":"` + id + `","supported_resolutions":["480p"],"pricing_skus":{"cents_per_video_output_second_480p":"2"}}`
}

func tokenOnlyVideo(id string) string {
	return `{"id":"` + id + `","supported_resolutions":["480p"],"pricing_skus":{"video_tokens":"0.00001"}}`
}

var routeDay = time.Now().UTC()

func TestAPricedAdvertisedCuratedVideoIsTheOnlyDefault(t *testing.T) {
	server := &routeServer{general: routeGeneral, images: imageListing("a/img"),
		vids: videoListing(tokenOnlyVideo("aaa/first-listed"), pricedVideo(preferredVideoModel), pricedVideo("zzz/newest"))}
	models := server.load(t, t.TempDir(), routeDay, false)
	route := (Config{}).ResolveVideoRoute(models)
	if route.Model != preferredVideoModel || route.Reason != RouteCurated || route.Err() != nil {
		t.Fatalf("route = %+v", route)
	}
	if (Config{}).ResolveVideoModel(models) != preferredVideoModel || CandidateMediaModel(models, "video") != preferredVideoModel {
		t.Error("the string resolvers disagree with the route")
	}
	if FallbackMediaModel("video") != "" || FallbackMediaModel("image") != "" {
		t.Error("the bare curated name for a spending modality bypasses the price gate")
	}
}

func TestReorderingOrAddingUnknownNewRowsNeverChangesTheDefault(t *testing.T) {
	rows := []string{tokenOnlyVideo("m/one"), pricedVideo("m/two"), pricedVideo(fallbackVideoModel), pricedVideo("m/three")}
	baseline := (&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(rows...)}).load(t, t.TempDir(), routeDay, false)
	want := (Config{}).ResolveVideoRoute(baseline)
	if want.Model != fallbackVideoModel {
		t.Fatalf("baseline = %+v, want the remembered older Seedance row", want)
	}
	reordered := []string{rows[3], rows[2], rows[1], rows[0]}
	withNew := append([]string{pricedVideo("brand/new-and-cheap"), tokenOnlyVideo("brand/unpriced")}, rows...)
	for name, listing := range map[string][]string{"reordered": reordered, "with new rows first": withNew} {
		got := (Config{}).ResolveVideoRoute((&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(listing...)}).load(t, t.TempDir(), routeDay, false))
		if got.Model != want.Model {
			t.Errorf("%s: default moved to %q", name, got.Model)
		}
	}
	// With no remembered row at all the answer is the same refusal however the
	// listing is ordered, with options sorted by name and not by anything else.
	noCurated := []string{pricedVideo("m/zz"), tokenOnlyVideo("m/aa"), pricedVideo("m/mm")}
	a := (Config{}).ResolveVideoRoute((&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(noCurated...)}).load(t, t.TempDir(), routeDay, false))
	b := (Config{}).ResolveVideoRoute((&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(noCurated[2], noCurated[1], noCurated[0])}).load(t, t.TempDir(), routeDay, false))
	if a.Model != "" || a.Reason != RouteModelRequired || strings.Join(a.Options, ",") != "m/aa,m/mm,m/zz" || strings.Join(a.Options, ",") != strings.Join(b.Options, ",") {
		t.Errorf("refusals = %+v / %+v", a, b)
	}
}

func TestAnAbsentOrUnpricedCuratedModelIsRefusedNotReplaced(t *testing.T) {
	absent := (&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(pricedVideo("other/priced"))}).load(t, t.TempDir(), routeDay, false)
	unpriced := (&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(tokenOnlyVideo(preferredVideoModel), tokenOnlyVideo(fallbackVideoModel), pricedVideo("other/priced"))}).load(t, t.TempDir(), routeDay, false)
	for name, models := range map[string]*catalog.Catalog{"absent": absent, "unpriced": unpriced} {
		route := (Config{}).ResolveVideoRoute(models)
		if route.Model != "" || route.Reason != RouteModelRequired || !errors.Is(route.Err(), ErrModelRequired) {
			t.Errorf("%s: route = %+v", name, route)
		}
		if errors.Is(route.Err(), ErrNoneAdvertised) {
			t.Errorf("%s: models exist and the error says none do", name)
		}
		if message := route.Err().Error(); !strings.Contains(message, "other/priced") || !strings.Contains(message, "Choose a video model") {
			t.Errorf("%s: message = %q", name, message)
		}
	}
}

func TestAnImageDefaultNeedsAGenuinelyReportedPrice(t *testing.T) {
	general := `{"data":[{"id":"` + preferredImageModel + `","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0"}}]}`
	zeroTokens := (&routeServer{general: general, images: imageListing(preferredImageModel, "x/other"), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	route := (Config{}).ResolveImageRoute(zeroTokens)
	if route.Model != "" || route.Reason != RouteModelRequired {
		t.Errorf("a per-token zero was taken as a price: %+v", route)
	}
	if !strings.Contains(route.Err().Error(), "x/other") {
		t.Errorf("the refusal does not name the options: %v", route.Err())
	}

	priced := `{"data":[{"id":"` + preferredImageModel + `","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"request":"0.04"}}]}`
	ok := (&routeServer{general: priced, images: imageListing(preferredImageModel), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	if got := (Config{}).ResolveImageRoute(ok); got.Model != preferredImageModel || got.Reason != RouteCurated {
		t.Errorf("a genuinely priced curated image row = %+v", got)
	}
	// A request price of zero is not a price either.
	free := `{"data":[{"id":"` + preferredImageModel + `","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"request":"0"}}]}`
	zero := (&routeServer{general: free, images: imageListing(preferredImageModel), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	if got := (Config{}).ResolveImageRoute(zero); got.Model != "" {
		t.Errorf("a zero request price chose a default: %+v", got)
	}
}

func TestAConfiguredRouteIsAlwaysTheHumansChoice(t *testing.T) {
	models := (&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(tokenOnlyVideo("m/one"))}).load(t, t.TempDir(), routeDay, false)
	configured := Config{ImageModel: " my/drawer ", VideoModel: "my/filmer"}
	if r := configured.ResolveImageRoute(models); r.Model != "my/drawer" || r.Reason != RouteConfigured {
		t.Errorf("image = %+v", r)
	}
	if r := configured.ResolveVideoRoute(models); r.Model != "my/filmer" || r.Reason != RouteConfigured {
		t.Errorf("video = %+v", r)
	}
	if configured.ResolveVideoModel(nil) != "my/filmer" {
		t.Error("a configured route needs no catalog")
	}
}

func TestAStaleOrUnreadListingNeverClaimsNoServiceOffersVideo(t *testing.T) {
	dir := t.TempDir()
	good := &routeServer{general: routeGeneral, images: imageListing("a/img"), vids: videoListing(pricedVideo(preferredVideoModel))}
	good.load(t, dir, routeDay, false)

	// The video listing fails on the next refresh: the old rows are kept, so the
	// remembered model is still ADVERTISED — but it is no longer a CURRENT
	// listing, so no default is chosen and nothing says "none".
	down := &routeServer{general: routeGeneral, images: imageListing("a/img"), vids: "", failVideos: true}
	stale := down.load(t, dir, routeDay.Add(time.Hour), true)
	route := (Config{}).ResolveVideoRoute(stale)
	if route.Model != "" || route.Reason != RouteUnconfirmed || !errors.Is(route.Err(), ErrUnconfirmed) {
		t.Fatalf("stale route = %+v", route)
	}
	if errors.Is(route.Err(), ErrNoneAdvertised) {
		t.Error("a stale listing claimed no service offers video")
	}
	if len(route.Options) != 1 {
		t.Errorf("the known models were hidden from the person: %v", route.Options)
	}

	// A listing that has NEVER been read (cold start, video endpoint down) is
	// unconfirmed too, with nothing to list.
	cold := (&routeServer{general: routeGeneral, images: imageListing("a/img"), vids: "", failVideos: true}).load(t, t.TempDir(), routeDay, false)
	if r := (Config{}).ResolveVideoRoute(cold); r.Reason != RouteUnconfirmed || r.Err() == nil {
		t.Errorf("cold route = %+v", r)
	}
	// And a current, authoritative, genuinely empty answer is the only none.
	// (An empty listing is itself refused as unreadable, so none is reached
	// through a service that does not read the dedicated listings at all.)
	other := runtimeCatalog(t, `{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	if r := (Config{}).ResolveVideoRoute(other); r.Reason != RouteNoneAdvertised || !errors.Is(r.Err(), ErrNoneAdvertised) {
		t.Errorf("other service route = %+v", r)
	}
}

func TestResolvingDefaultsFloodsNothingAndNeverAsksPerModelPrices(t *testing.T) {
	server := &routeServer{general: routeGeneral, images: imageListing("a/img", "b/img", "c/img"), vids: videoListing(pricedVideo("m/one"), pricedVideo("m/two"))}
	models := server.load(t, t.TempDir(), routeDay, false)
	before := len(server.requests)
	for i := 0; i < 50; i++ {
		(Config{}).ResolveImageModel(models)
		(Config{}).ResolveVideoModel(models)
		CandidateMediaModel(models, "video")
	}
	if len(server.requests) != before {
		t.Errorf("resolving made %d requests", len(server.requests)-before)
	}
	for _, request := range server.requests {
		if !strings.HasPrefix(request, "GET ") || strings.Contains(request, "/endpoints") {
			t.Errorf("unexpected request %q", request)
		}
	}
	if before != 3 {
		t.Errorf("a load made %d requests, want the three listings", before)
	}
}

func TestSpeechAndMusicDefaultsAreUnchanged(t *testing.T) {
	models := runtimeCatalog(t, `
		{"id":"first/speech","architecture":{"output_modalities":["speech"]}},
		{"id":"first/music","architecture":{"output_modalities":["music"]}}`)
	if got := (Config{}).ResolveSpeechModel(models); got != "first/speech" {
		t.Errorf("speech default = %q", got)
	}
	if got := (Config{}).ResolveMusicModel(models); got != "first/music" {
		t.Errorf("music default = %q", got)
	}
	if got := FallbackMediaModel("music"); got != preferredMusicModel {
		t.Errorf("music curated name = %q", got)
	}
}

func TestMediaRouteErrorsReadAsOneSentenceEach(t *testing.T) {
	cases := map[RouteReason]string{
		RouteModelRequired:  "there is no default image model with a published price. Choose an image model",
		RouteUnconfirmed:    "the image model list could not be confirmed",
		RouteNoneAdvertised: "no connected service advertises image generation",
	}
	for reason, want := range cases {
		err := MediaRoute{Modality: "image", Reason: reason, Options: []string{"a", "b"}, Total: 10}.Err()
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: %v", reason, err)
		}
	}
	if err := (MediaRoute{Modality: "image", Reason: RouteModelRequired, Options: []string{"a"}, Total: 10}).Err(); !strings.Contains(err.Error(), "(and 9 more)") {
		t.Errorf("the overflow is not counted: %v", err)
	}
}
