package config

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// THE PICTURE DEFAULT, against listings shaped like OpenRouter's own on
// 2026-11-19: a drawing model's only load-time price is the general listing's
// per-token `image_output`, beside prompt and completion zeros.

// drawingRow is one general-listing row of a drawing model. An empty price
// leaves `image_output` out, which is a model that published nothing.
func drawingRow(id, imageOutput string, elo int) string {
	pricing := `"prompt":"0","completion":"0"`
	if imageOutput != "" {
		pricing += `,"image_output":"` + imageOutput + `"`
	}
	benchmarks := ""
	if elo > 0 {
		benchmarks = `,"benchmarks":{"design_arena":[{"arena":"models","category":"image","elo":` + strconv.Itoa(elo) + `}]}`
	}
	return `{"id":"` + id + `","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},"pricing":{` + pricing + `}` + benchmarks + `}`
}

func drawingListing(rows ...string) string {
	return `{"data":[` + strings.Join(append([]string{`{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}`}, rows...), ",") + `]}`
}

func TestThePictureDefaultIsTheFirstCuratedModelTheListingPrices(t *testing.T) {
	general := drawingListing(
		drawingRow("aaa/better-scored", "0.00001", 1400),
		drawingRow(fallbackImageModel, "0.0000036", 0),
		drawingRow(preferredImageModel, "0.00006", 1251),
		drawingRow(priorImageModel, "0.0000108", 0),
	)
	models := (&routeServer{general: general, images: imageListing(preferredImageModel, priorImageModel, fallbackImageModel), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	route := (Config{}).ResolveImageRoute(models)
	if route.Model != preferredImageModel || route.Reason != RouteCurated || route.Err() != nil {
		t.Fatalf("route = %+v, want the curated leader %s", route, preferredImageModel)
	}

	// The leader gone, the next remembered name is taken before any listed one.
	withoutLeader := drawingListing(drawingRow("aaa/better-scored", "0.00001", 1400), drawingRow(fallbackImageModel, "0.0000036", 0), drawingRow(priorImageModel, "0.0000108", 0))
	models = (&routeServer{general: withoutLeader, images: imageListing(priorImageModel), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	if route := (Config{}).ResolveImageRoute(models); route.Model != priorImageModel || route.Reason != RouteCurated {
		t.Fatalf("route without the leader = %+v, want %s", route, priorImageModel)
	}
}

func TestWithNoCuratedModelThePictureDefaultIsTheBestPricedListedOne(t *testing.T) {
	general := drawingListing(
		drawingRow("zzz/unscored", "0.000002", 0),
		drawingRow("mmm/scored", "0.00003", 1175),
		drawingRow("bbb/top-but-unpriced", "", 1500),
		drawingRow("ccc/scored-tie", "0.00001", 1175),
		`{"id":"openrouter/auto","architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},"pricing":{"prompt":"-1","completion":"-1"}}`,
		`{"id":"edits/only","architecture":{"input_modalities":["image"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0","image_output":"0.00001"},"benchmarks":{"design_arena":[{"elo":1600}]}}`,
	)
	models := (&routeServer{general: general, images: imageListing("zzz/unscored"), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	route := (Config{}).ResolveImageRoute(models)
	// The unpriced row, the router and the model that takes no prompt are never
	// defaults whatever their score; of the two equal scores the lower id wins.
	if route.Model != "ccc/scored-tie" || route.Reason != RouteListed || route.Err() != nil {
		t.Fatalf("route = %+v, want the best-scored priced listed model ccc/scored-tie", route)
	}
	if CandidateMediaModel(models, "image") != "ccc/scored-tie" || (Config{}).ResolveImageModel(models) != "ccc/scored-tie" {
		t.Error("the string resolvers disagree with the route")
	}
}

func TestAPictureListingWithNothingPricedOrNothingAtAllStillRefuses(t *testing.T) {
	unpriced := drawingListing(drawingRow(preferredImageModel, "", 0), drawingRow("x/other", "", 1300))
	models := (&routeServer{general: unpriced, images: imageListing(preferredImageModel, "x/other"), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	route := (Config{}).ResolveImageRoute(models)
	if route.Model != "" || route.Reason != RouteModelRequired || !errors.Is(route.Err(), ErrModelRequired) {
		t.Fatalf("unpriced listing: route = %+v", route)
	}
	if message := route.Err().Error(); !strings.Contains(message, "there is no default image model with a published price") || !strings.Contains(message, "x/other") {
		t.Errorf("refusal = %q", message)
	}

	// An empty dedicated listing is itself refused as unreadable, so a listing
	// with no drawing model at all is reached the way the video twin reaches it:
	// through a service that reads only its general listing.
	empty := runtimeCatalog(t, `{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	route = (Config{}).ResolveImageRoute(empty)
	if route.Model != "" || route.Reason != RouteNoneAdvertised || !errors.Is(route.Err(), ErrNoneAdvertised) {
		t.Fatalf("empty listing: route = %+v, want none advertised", route)
	}
}

func TestAnExplicitPictureModelWinsOverEveryDefault(t *testing.T) {
	general := drawingListing(drawingRow(preferredImageModel, "0.00006", 1251), drawingRow("best/listed", "0.00001", 1600))
	models := (&routeServer{general: general, images: imageListing(preferredImageModel), vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
	chosen := Config{ImageModel: "someone/their-own-unlisted-pick"}
	route := chosen.ResolveImageRoute(models)
	if route.Model != "someone/their-own-unlisted-pick" || route.Reason != RouteConfigured {
		t.Fatalf("route = %+v, want the person's own row untouched", route)
	}
	// And with nothing listed at all, the person's row still stands.
	if route := chosen.ResolveImageRoute(nil); route.Model != "someone/their-own-unlisted-pick" {
		t.Fatalf("route over no catalog = %+v", route)
	}
}
