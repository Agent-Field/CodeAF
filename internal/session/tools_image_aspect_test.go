package session

import (
	"bytes"
	"encoding/base64"
	stdimage "image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A frame shape the model does not draw is drawn at its nearest one and cropped
// back, and the result says so (tools_image_aspect.go).

// geminiShapes is what google/gemini-3.1-flash-image published on 2026-11-19:
// fourteen shapes, and not 8:5.
var geminiShapes = []string{"1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5", "5:4", "8:1", "9:16", "16:9", "21:9"}

func newShapePainter(t *testing.T, drawnW, drawnH int, shapes []string) (*Agent, *scriptedMedia, string) {
	t.Helper()
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, drawnW, drawnH)),
		mediaType: "image/png",
	}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "google/gemini-3.1-flash-image"})
		config.MediaAspects = func(model string) []string {
			if model == "google/gemini-3.1-flash-image" {
				return shapes
			}
			return nil
		}
	})
	return agent, painter, workspace
}

func savedSize(t *testing.T, workspace, result string) (int, int) {
	t.Helper()
	path, _, found := strings.Cut(result, " — ")
	if !found {
		t.Fatalf("the result names no file: %q", result)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the saved picture cannot be read: %v", err)
	}
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the saved picture is not a picture: %v", err)
	}
	return config.Width, config.Height
}

// The pxinplace case: "make the photo brighter" on an 8:5 picture, with a
// default model that does not draw 8:5. It is drawn at 3:2, the nearest shape
// Gemini takes, and comes back 8:5 at the photo's own size.
func TestAnEightByFiveEditIsDrawnAtTheNearestShapeAndComesBackItsOwnSize(t *testing.T) {
	agent, painter, workspace := newShapePainter(t, 48, 32, geminiShapes)
	photo := writeReference(t, workspace, "art/photo.png", pngOfSize(t, 40, 25))

	result, isError := runTool(t, agent, "generate_image",
		`{"prompt":"make the photo brighter","reference_paths":["`+photo+`"],"aspect_ratio":"8:5"}`)
	if isError {
		t.Fatalf("an 8:5 edit was refused: %s", result)
	}
	if got := painter.request(0).AspectRatio; got != "3:2" {
		t.Fatalf("the model was asked for %q, want its nearest published shape 3:2", got)
	}
	if w, h := savedSize(t, workspace, result); w != 40 || h != 25 {
		t.Fatalf("the edit came back %d×%d, want the photo's own 40×25", w, h)
	}
	if !strings.Contains(result, "does not draw 8:5, so it drew at 3:2 and the result was cropped to 8:5 and scaled to 40×25") {
		t.Fatalf("the result does not say the shape was fitted: %q", result)
	}
	if first, _, _ := strings.Cut(result, "\n"); !strings.HasSuffix(first, ", generated on google/gemini-3.1-flash-image") {
		t.Fatalf("the first line no longer ends with the model: %q", first)
	}
}

// With no picture to match, the result is cropped to the asked shape at the
// size the model drew it.
func TestAShapeTheModelDoesNotDrawIsCroppedToTheAskedShape(t *testing.T) {
	agent, painter, workspace := newShapePainter(t, 48, 32, geminiShapes)
	result, isError := runTool(t, agent, "generate_image", `{"prompt":"a wide harbour at dawn","aspect_ratio":"8:5"}`)
	if isError {
		t.Fatalf("an 8:5 picture was refused: %s", result)
	}
	if got := painter.request(0).AspectRatio; got != "3:2" {
		t.Fatalf("asked for %q, want 3:2", got)
	}
	// 48×32 is 3:2; cropped to 8:5 it keeps the width and 30 of the 32 rows.
	if w, h := savedSize(t, workspace, result); w != 48 || h != 30 {
		t.Fatalf("saved %d×%d, want 48×30", w, h)
	}
}

// A shape the model draws, and one whose shapes nobody published, are sent as
// they were asked and never cropped.
func TestAShapeTheModelDrawsOrCannotBeCheckedIsSentUntouched(t *testing.T) {
	for name, shapes := range map[string][]string{"published": geminiShapes, "unpublished": nil} {
		agent, painter, workspace := newShapePainter(t, 32, 18, shapes)
		asked := "16:9"
		if shapes == nil {
			asked = "8:5"
		}
		result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour","aspect_ratio":"`+asked+`"}`)
		if isError {
			t.Fatalf("%s: %s", name, result)
		}
		if got := painter.request(0).AspectRatio; got != asked {
			t.Errorf("%s: sent %q, want %q untouched", name, got, asked)
		}
		if w, h := savedSize(t, workspace, result); w != 32 || h != 18 {
			t.Errorf("%s: the picture was altered to %d×%d", name, w, h)
		}
		if strings.Contains(result, "does not draw") {
			t.Errorf("%s: a shape that was sent as asked is reported as fitted: %q", name, result)
		}
	}
}

func TestFitAspectMeasuresShapesNotSpellings(t *testing.T) {
	cases := []struct {
		asked     string
		published []string
		sent      string
		mapped    bool
	}{
		{"8:5", geminiShapes, "3:2", true},
		{"16:10", []string{"8:5", "1:1"}, "8:5", false},
		{"5:8", geminiShapes, "2:3", true},
		{"2.35:1", geminiShapes, "21:9", true},
		{"auto", geminiShapes, "auto", false},
		{"", geminiShapes, "", false},
		{"8:5", []string{"auto"}, "8:5", false},
	}
	for _, c := range cases {
		fit := fitAspect(c.asked, c.published)
		if fit.sent != c.sent || fit.mapped() != c.mapped {
			t.Errorf("fitAspect(%q) = %q mapped=%v, want %q mapped=%v", c.asked, fit.sent, fit.mapped(), c.sent, c.mapped)
		}
	}
}
