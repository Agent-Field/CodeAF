package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// THE PICTURE TARIFF: a drawing model's only load-time price is the general
// listing's per-token `image_output`, beside prompt and completion zeros. On
// 2026-11-19 every drawing model on OpenRouter published it, and a catalog that
// dropped it refused every picture default on a fresh profile.

const tariffGeneral = `{"data":[
 {"id":"google/gemini-3.1-flash-image","architecture":{"input_modalities":["image","text"],"output_modalities":["image","text"]},"pricing":{"prompt":"0.0000005","completion":"0.000003","image_output":"0.00006"}},
 {"id":"bytedance-seed/seedream-5-0-pro","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0","image":"0.003","image_output":"0.0000107784431137725"}},
 {"id":"vendor/draws-unpriced","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0"}},
 {"id":"chat/only","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001","image_output":"0.5"}}]}`

func TestTheGeneralListingsImageOutputRateIsAPublishedPicturePrice(t *testing.T) {
	c := loadAt(t, newMediaRouter(t, map[string]mediaReply{generalPath: okReply(tariffGeneral)}), t.TempDir(), day, nil)
	for _, id := range []string{"google/gemini-3.1-flash-image", "bytedance-seed/seedream-5-0-pro"} {
		row, ok := c.Model(id)
		if !ok || !row.PublishedPrice("image") {
			t.Errorf("%s: no published picture price: %+v", id, row.MediaPrices)
		}
	}
	if prices := c.MediaPrices("google/gemini-3.1-flash-image"); len(prices) != 1 || prices[0].Unit != "token" || prices[0].Billable != "image_output" || prices[0].USD != 0.00006 {
		t.Errorf("gemini prices = %+v, want one per-token image_output rate", prices)
	}
	if row, _ := c.Model("vendor/draws-unpriced"); row.PublishedPrice("image") {
		t.Error("prompt and completion zeros were taken as a picture price")
	}
	// A rate on a row that does not draw is not a picture price of anything.
	if prices := c.MediaPrices("chat/only"); len(prices) != 0 {
		t.Errorf("a chat row carries a picture price: %+v", prices)
	}
}

func TestACacheWithoutImagePricesIsReadAgainOnceByTheLazyWarm(t *testing.T) {
	dir := t.TempDir()
	// What an older build wrote the day before: a fresh, marked cache whose
	// drawing rows lost their price on the way in.
	older := `{"fetched_at":"` + day.Format(time.RFC3339) + `","priced":true,"base":"` + DefaultBaseURL + `","models":[` +
		`{"id":"bytedance-seed/seedream-5-0-pro","input_modalities":["text","image"],"output_modalities":["image"]}]}`
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(older), 0o600); err != nil {
		t.Fatal(err)
	}
	replies := map[string]mediaReply{generalPath: okReply(tariffGeneral), imagesPath: okReply(fixtureBytes(t, "openrouter-images.json")), videosPath: okReply(fixtureBytes(t, "openrouter-videos.json"))}

	// The eager door keeps its promise: a fresh cache costs no request.
	quiet := newMediaRouter(t, replies)
	if c := Load(context.Background(), mediaOptions(quiet, dir, day.Add(time.Hour))); quiet.total() != 0 {
		t.Errorf("an eager load of a fresh cache made %d requests", quiet.total())
	} else if row, _ := c.Model("bytedance-seed/seedream-5-0-pro"); row.PublishedPrice("image") {
		t.Error("the older cache grew a price it never had")
	}

	// The lazy warm reads the general listing again, and its answer is priced.
	warm := newMediaRouter(t, replies)
	lazy := LoadLazy(context.Background(), mediaOptions(warm, dir, day.Add(time.Hour)))
	row, ok := lazy.Model("bytedance-seed/seedream-5-0-pro")
	lazy.Close()
	if !ok || !row.PublishedPrice("image") {
		t.Fatalf("the lazy answer over an untariffed cache is unpriced: %+v", row)
	}
	if warm.count(generalPath) != 1 {
		t.Errorf("the general listing was read %d times, want once", warm.count(generalPath))
	}

	// Once: the cache now carries the mark, and the next lazy warm asks nothing
	// of the general listing.
	again := newMediaRouter(t, replies)
	next := LoadLazy(context.Background(), mediaOptions(again, dir, day.Add(2*time.Hour)))
	next.Model("bytedance-seed/seedream-5-0-pro")
	next.Close()
	if again.count(generalPath) != 0 {
		t.Errorf("a marked cache was refetched %d times", again.count(generalPath))
	}
}
