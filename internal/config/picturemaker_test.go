package config

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
)

// THE PICTURE MAKER, against listings shaped like OpenRouter's: a drawing
// model's price on the general listing (per output-image token), its knobs on
// the dedicated image listing.

// wordsOnlyRow is a drawing model that takes words and no picture.
func wordsOnlyRow(id, imageOutput string) string {
	return `{"id":"` + id + `","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0","image_output":"` + imageOutput + `"}}`
}

// knobbedImages is a dedicated image listing whose first row publishes a
// shape list, a count, a mask and a transparent background.
func knobbedImages(first string, rest ...string) string {
	rows := []string{`{"id":"` + first + `","name":"Vendor: Knobbed One","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},` +
		`"supported_parameters":{"aspect_ratio":{"type":"enum","values":["1:1","16:9"]},"n":{"type":"range","min":1,"max":4},` +
		`"input_references":{"type":"range","min":1,"max":3},"mask":{"type":"boolean"},"background":{"type":"enum","values":["auto","transparent"]}}}`}
	for _, id := range rest {
		rows = append(rows, `{"id":"`+id+`","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"supported_parameters":{}}`)
	}
	return `{"data":[` + strings.Join(rows, ",") + `]}`
}

func pictureCatalog(t *testing.T, general, images string) *catalog.Catalog {
	t.Helper()
	return (&routeServer{general: general, images: images, vids: videoListing(pricedVideo("v/v"))}).load(t, t.TempDir(), routeDay, false)
}

func makerByID(t *testing.T, makers []PictureMaker, id string) PictureMaker {
	t.Helper()
	for _, maker := range makers {
		if maker.ID == id {
			return maker
		}
	}
	t.Fatalf("%s is not on the list %+v", id, makers)
	return PictureMaker{}
}

func TestThePictureMakerListCarriesPriceAndCapabilities(t *testing.T) {
	models := pictureCatalog(t,
		drawingListing(drawingRow("vendor/knobbed", "0.00006", 1200), wordsOnlyRow("vendor/words", "0.00001")),
		knobbedImages("vendor/knobbed", "vendor/words"))
	makers := PictureMakers(models)
	knobbed := makerByID(t, makers, "vendor/knobbed")
	if knobbed.Name != "Knobbed One" {
		t.Errorf("name = %q, want the listing's name without its vendor", knobbed.Name)
	}
	// $0.00006 an output token at a 1K picture's 1120 tokens.
	if !knobbed.Priced || knobbed.PriceUnit != "token" || !knobbed.Estimated || math.Abs(knobbed.PricePerPicture-0.0672) > 1e-9 {
		t.Errorf("price = %+v", knobbed)
	}
	if !knobbed.ReadsPhotos || !knobbed.MasksEdits || knobbed.MaxPictures != 4 || knobbed.MaxPhotos != 3 || !knobbed.Transparent ||
		strings.Join(knobbed.Shapes, ",") != "1:1,16:9" || !knobbed.Automatic || knobbed.Unavailable != "" {
		t.Errorf("capabilities = %+v", knobbed)
	}
	words := makerByID(t, makers, "vendor/words")
	if words.ReadsPhotos || words.Transparent || words.MaxPictures != 0 || len(words.Shapes) != 0 {
		t.Errorf("an unpublished capability must read as absent: %+v", words)
	}
	if got := PictureMakerOf(models, "vendor/gone"); got.Unavailable != "not_listed" || got.Name != "gone" {
		t.Errorf("a chosen model nobody lists = %+v", got)
	}
}

func TestAPictureIsPricedInTheUnitTheListingPublished(t *testing.T) {
	cases := []struct {
		name  string
		row   catalog.Model
		usd   float64
		unit  string
		known bool
	}{
		{"per image", catalog.Model{MediaPrices: []catalog.MediaPrice{{Billable: "image_output", Unit: "image", USD: 0.045}}}, 0.045, "image", true},
		{"per megapixel at 1024 square", catalog.Model{MediaPrices: []catalog.MediaPrice{{Billable: "image_output", Unit: "megapixel", USD: 0.03}}}, 0.03 * 1.048576, "megapixel", true},
		{"the model's own rate over a cheaper endpoint", catalog.Model{MediaPrices: []catalog.MediaPrice{
			{Billable: "image_output", Unit: "image", USD: 0.01, Provider: "cheap"}, {Billable: "image_output", Unit: "image", USD: 0.04}}}, 0.04, "image", true},
		{"the lowest of several endpoints", catalog.Model{MediaPrices: []catalog.MediaPrice{
			{Billable: "image_output", Unit: "image", USD: 0.05, Provider: "a"}, {Billable: "image_output", Unit: "image", USD: 0.02, Provider: "b"}}}, 0.02, "image", true},
		{"per request", catalog.Model{RequestPrice: 0.08}, 0.08, "request", true},
		{"an input price is not a picture's", catalog.Model{MediaPrices: []catalog.MediaPrice{{Billable: "image_input", Unit: "image", USD: 0.01}}}, 0, "", false},
	}
	for _, c := range cases {
		usd, unit, known := PicturePrice(c.row)
		if known != c.known || unit != c.unit || math.Abs(usd-c.usd) > 1e-12 {
			t.Errorf("%s: got %v %q %v, want %v %q %v", c.name, usd, unit, known, c.usd, c.unit, c.known)
		}
	}
}

func TestAutomaticsLadderStartsWhereTheDefaultRouteDoes(t *testing.T) {
	models := pictureCatalog(t,
		drawingListing(drawingRow("aaa/better-scored", "0.00001", 1400), drawingRow(priorImageModel, "0.0000108", 0),
			drawingRow(fallbackImageModel, "0.0000036", 0), wordsOnlyRow("zzz/words", "0.00001")),
		`{"data":[{"id":"`+priorImageModel+`","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},"supported_parameters":{}},`+
			`{"id":"`+fallbackImageModel+`","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"supported_parameters":{}}]}`)
	ladder := ImageLadder(models, false)
	if route := DefaultMediaRoute(models, "image"); len(ladder) == 0 || ladder[0] != route.Model {
		t.Fatalf("ladder %v does not start at the default %q", ladder, route.Model)
	}
	if strings.Join(ladder, ",") != priorImageModel+","+fallbackImageModel+",aaa/better-scored,zzz/words" {
		t.Errorf("ladder = %v, want the remembered names, then the listed ones by score", ladder)
	}
	// Changing a photo takes the models that read one, and only those.
	if photos := ImageLadder(models, true); strings.Join(photos, ",") != priorImageModel+",aaa/better-scored" {
		t.Errorf("photo ladder = %v", photos)
	}
}

func TestAPictureFollowsTheCanvasThenEverywhereThenAutomatic(t *testing.T) {
	t.Setenv("CODEAF_IMAGE_MODEL", "")
	t.Setenv("AFORGE_IMAGE_MODEL", "") // legacy-name
	profile := t.TempDir()
	canvas := filepath.Join(t.TempDir(), "spring", "work")
	models := pictureCatalog(t,
		drawingListing(drawingRow(priorImageModel, "0.0000108", 0), drawingRow("vendor/other", "0.00002", 900)),
		imageListing(priorImageModel, "vendor/other"))

	route := ResolvePicture(models, profile, PictureAsk{Workspace: canvas})
	if route.Model != priorImageModel || route.Scope != PictureScopeAutomatic || route.Reason != string(RouteCurated) || route.Chosen() {
		t.Fatalf("nothing chosen: %+v", route)
	}
	if strings.Join(route.Then, ",") != "vendor/other" {
		t.Errorf("Automatic's next rungs = %v", route.Then)
	}

	if err := SetEverywherePictureMaker(profile, "vendor/other"); err != nil {
		t.Fatal(err)
	}
	route = ResolvePicture(models, profile, PictureAsk{Workspace: canvas})
	if route.Model != "vendor/other" || route.Scope != PictureScopeEverywhere || !route.Chosen() || len(route.Then) != 0 || route.Alternative != priorImageModel {
		t.Fatalf("Everywhere chosen: %+v", route)
	}

	if err := SetCanvasPictureMakerAt(profile, canvas, priorImageModel); err != nil {
		t.Fatal(err)
	}
	// A folder inside the canvas's folder is the same canvas.
	route = ResolvePicture(models, profile, PictureAsk{Workspace: filepath.Join(canvas, "drafts")})
	if route.Model != priorImageModel || route.Scope != PictureScopeCanvas || route.Alternative != "vendor/other" {
		t.Fatalf("canvas chosen: %+v", route)
	}
	if other := ResolvePicture(models, profile, PictureAsk{Workspace: t.TempDir()}); other.Scope != PictureScopeEverywhere {
		t.Errorf("another canvas follows Everywhere: %+v", other)
	}

	// Empty gives the canvas back to Everywhere, and Everywhere back to Automatic.
	if err := SetCanvasPictureMakerAt(profile, canvas, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetEverywherePictureMaker(profile, ""); err != nil {
		t.Fatal(err)
	}
	if route = ResolvePicture(models, profile, PictureAsk{Workspace: canvas}); route.Scope != PictureScopeAutomatic {
		t.Fatalf("both cleared: %+v", route)
	}
	if model, _ := EverywherePictureMaker(profile); model != "" {
		t.Errorf("Everywhere still reads %q after it was cleared", model)
	}
	if err := SetCanvasPictureMakerAt(profile, "relative/work", "x"); err == nil {
		t.Error("a relative canvas folder was accepted")
	}
}

func TestAnEverywhereVariableOutranksTheRowAndSaysSo(t *testing.T) {
	profile := t.TempDir()
	if err := SetEverywherePictureMaker(profile, "vendor/row"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_IMAGE_MODEL", "vendor/variable")
	if model, variable := EverywherePictureMaker(profile); model != "vendor/variable" || variable != "CODEAF_IMAGE_MODEL" {
		t.Fatalf("got %q from %q", model, variable)
	}
}

func TestChangingAPhotoWithAModelThatCannotReadOneIsRefusedInWords(t *testing.T) {
	t.Setenv("CODEAF_IMAGE_MODEL", "")
	profile := t.TempDir()
	models := pictureCatalog(t,
		drawingListing(wordsOnlyRow("vendor/words", "0.00001"), drawingRow(priorImageModel, "0.0000108", 0)),
		knobbedImages("vendor/unused", "vendor/words"))
	if err := SetEverywherePictureMaker(profile, "vendor/words"); err != nil {
		t.Fatal(err)
	}
	route := ResolvePicture(models, profile, PictureAsk{NeedsPhotos: true})
	if route.Model != "" || route.Reason != PictureCannotReadPhotos {
		t.Fatalf("route = %+v, want a refusal", route)
	}
	if !strings.Contains(route.Message, "can't read a photo, and you're changing one") || !strings.Contains(route.Message, route.Name(priorImageModel)+" can") {
		t.Errorf("message = %q", route.Message)
	}
	// Words alone are still the chosen model's to draw.
	if words := ResolvePicture(models, profile, PictureAsk{}); words.Model != "vendor/words" {
		t.Errorf("from words: %+v", words)
	}
	// Automatic simply takes a model that reads photos.
	_ = SetEverywherePictureMaker(profile, "")
	if auto := ResolvePicture(models, profile, PictureAsk{NeedsPhotos: true}); auto.Model != priorImageModel {
		t.Errorf("Automatic with a photo: %+v", auto)
	}
}

func TestWithNothingThatDrawsTheRouteSaysSoPlainly(t *testing.T) {
	t.Setenv("CODEAF_IMAGE_MODEL", "")
	// An empty dedicated listing is refused as unreadable, so "nothing draws" is
	// reached through a service that reads only its general listing.
	models := runtimeCatalog(t, `{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`)
	route := ResolvePicture(models, t.TempDir(), PictureAsk{})
	if route.Model != "" || route.Reason != PictureNoneAdvertised || !strings.HasPrefix(route.Message, "No connected provider makes pictures.") {
		t.Fatalf("route = %+v", route)
	}
	if len(PictureMakers(models)) != 0 {
		t.Error("a listing with no drawing model listed one")
	}
}
