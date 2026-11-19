package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// THE PICTURE MAKER DOOR: what the desk reads to draw "Automatic · Seedream 5
// Pro", a canvas's own choice, and the one sentence for an app that cannot
// make pictures at all.

const pictureRows = `{"id":"seed/pro","name":"ByteDance Seed: Seedream 5 Pro","architecture":{"input_modalities":["text","image"],"output_modalities":["image"]},"pricing":{"prompt":"0","completion":"0","image_output":"0.00004"}},` +
	`{"id":"chat/model","architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000001"}}`

func TestPictureMakersWithNoProviderSayWhereToConnectOne(t *testing.T) {
	got := pictureMakersWire(nil, t.TempDir(), "", false, false, "")
	if got.Resolved.Model != "" || got.Resolved.Reason != "no_provider" || !strings.Contains(got.Resolved.Message, "Settings → Models") || got.Makers == nil || len(got.Makers) != 0 {
		t.Fatalf("no provider = %+v", got)
	}
	none := pictureMakersWire(nil, t.TempDir(), "", true, false, "")
	if none.Resolved.Reason != config.PictureNoneAdvertised || !strings.HasPrefix(none.Resolved.Message, "No connected provider makes pictures.") {
		t.Fatalf("a provider that makes no pictures = %+v", none.Resolved)
	}
}

func TestPictureMakersNameTheAutomaticModelAndACanvasChoice(t *testing.T) {
	t.Setenv("CODEAF_IMAGE_MODEL", "")
	t.Setenv("AFORGE_IMAGE_MODEL", "") // legacy-name
	models := mediaCatalogFor(t, pictureRows)
	profile := t.TempDir()
	canvas := filepath.Join(t.TempDir(), "work")

	got := pictureMakersWire(models, profile, canvas, true, true, "")
	if got.Resolved.Model != "seed/pro" || got.Resolved.Name != "Seedream 5 Pro" || got.Resolved.Scope != "automatic" {
		t.Fatalf("Automatic = %+v", got.Resolved)
	}
	if got.Canvas == nil || got.Canvas.Model != "" || got.Everywhere.Model != "" {
		t.Fatalf("nothing chosen yet: canvas %+v everywhere %+v", got.Canvas, got.Everywhere)
	}
	if len(got.Makers) != 1 || got.Makers[0].PricePerPictureUSD == nil || !got.Makers[0].ReadsPhotos || got.Makers[0].Shapes == nil {
		t.Fatalf("makers = %+v", got.Makers)
	}

	if err := config.SetCanvasPictureMakerAt(profile, canvas, "vendor/unlisted"); err != nil {
		t.Fatal(err)
	}
	got = pictureMakersWire(models, profile, canvas, true, true, "")
	if got.Canvas.Model != "vendor/unlisted" || got.Canvas.Folder != canvas || got.Resolved.Scope != "canvas" || got.Resolved.Model != "vendor/unlisted" {
		t.Fatalf("canvas chosen = %+v / %+v", got.Canvas, got.Resolved)
	}
	last := got.Makers[len(got.Makers)-1]
	if last.ID != "vendor/unlisted" || last.Unavailable != "not_listed" || !strings.Contains(last.UnavailableReason, "isn't offered") {
		t.Fatalf("a chosen model nobody lists is not drawn as one: %+v", last)
	}
	// Without the canvas, the answer is Everywhere's alone.
	if plain := pictureMakersWire(models, profile, "", true, true, ""); plain.Canvas != nil || plain.Resolved.Scope != "automatic" {
		t.Fatalf("no workspace = %+v", plain)
	}
}
