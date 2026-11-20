package session

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// A mask rides by the road the model can take, and the result says which.

func newMaskPainter(t *testing.T, takesMask bool) (*Agent, *scriptedMedia, string) {
	t.Helper()
	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, 4, 3)),
		mediaType: "image/png",
	}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		if takesMask {
			config.MediaMasks = func(model string) bool { return model == "paint/model" }
		}
	})
	return agent, painter, workspace
}

// A model that publishes no mask knob gets the mask as the LAST reference with
// words naming it, never a `mask` field it might drop, and the result admits
// the change may reach past the white area.
func TestAMaskIsAGuidePictureForAModelThatTakesNoMask(t *testing.T) {
	agent, painter, workspace := newMaskPainter(t, false)
	source := writeReference(t, workspace, "art/room.png", pngOfSize(t, 4, 3))
	maskBytes := pngOfSize(t, 4, 3)
	mask := writeReference(t, workspace, "art/room-mask.png", maskBytes)

	result, isError := runTool(t, agent, "generate_image",
		`{"prompt":"a red sofa","reference_paths":["`+source+`"],"mask_path":"`+mask+`"}`)
	if isError {
		t.Fatalf("a guided masked edit failed: %s", result)
	}
	request := painter.request(0)
	if request.Mask != nil {
		t.Fatal("a model that publishes no mask knob was sent a mask field")
	}
	if len(request.InputReferences) != 2 ||
		!bytes.Equal(dataURLBytes(t, referenceURL(t, request.InputReferences[1])), maskBytes) {
		t.Fatalf("the mask is not the last of %d references", len(request.InputReferences))
	}
	if !strings.HasPrefix(request.Prompt, "a red sofa") || !strings.HasSuffix(request.Prompt, maskGuideWords) {
		t.Fatalf("the instruction does not say what the last picture is: %q", request.Prompt)
	}
	if !strings.Contains(result, maskGuidedNote) {
		t.Fatalf("the result does not say the mask was only a guide: %q", result)
	}
	// The path and the model stay on the first line, where the surfaces read them.
	first, _, _ := strings.Cut(result, "\n")
	if !strings.HasSuffix(first, ", generated on paint/model") {
		t.Fatalf("the first line no longer ends with the model: %q", first)
	}
}

// A model whose catalog row publishes a mask knob gets a real mask, the source
// alone as its reference, and the instruction exactly as written.
func TestAMaskRidesAsARealMaskForAModelThatTakesOne(t *testing.T) {
	agent, painter, workspace := newMaskPainter(t, true)
	source := writeReference(t, workspace, "art/room.png", pngOfSize(t, 4, 3))
	maskBytes := pngOfSize(t, 4, 3)
	mask := writeReference(t, workspace, "art/room-mask.png", maskBytes)

	result, isError := runTool(t, agent, "generate_image",
		`{"prompt":"a red sofa","reference_paths":["`+source+`"],"mask_path":"`+mask+`","n":2}`)
	if isError {
		t.Fatalf("a real masked edit failed: %s", result)
	}
	request := painter.request(0)
	if request.Mask == nil || !bytes.Equal(dataURLBytes(t, referenceURL(t, *request.Mask)), maskBytes) {
		t.Fatal("the mask did not ride as the request's mask")
	}
	if len(request.InputReferences) != 1 || request.Prompt != "a red sofa" || request.N != 2 {
		t.Fatalf("refs=%d prompt=%q n=%d, want the source alone, the words as written and n=2",
			len(request.InputReferences), request.Prompt, request.N)
	}
	if !strings.Contains(result, maskSentNote) || strings.Contains(result, maskGuidedNote) {
		t.Fatalf("the result does not say the mask was real: %q", result)
	}
}

// Every way a mask cannot work is refused before anything is paid for.
func TestAMaskThatCannotWorkIsRefusedBeforeAnythingIsPaid(t *testing.T) {
	agent, painter, workspace := newMaskPainter(t, true)
	source := writeReference(t, workspace, "art/room.png", pngOfSize(t, 4, 3))
	mask := writeReference(t, workspace, "art/room-mask.png", pngOfSize(t, 4, 3))
	wrong := writeReference(t, workspace, "art/other-mask.png", pngOfSize(t, 8, 8))

	for _, testCase := range []struct {
		name, args, want string
	}{
		{"no picture to mask", `{"prompt":"x","mask_path":"` + mask + `"}`, "mask_path needs the picture it masks"},
		{"a mask that is not there", `{"prompt":"x","reference_paths":["` + source + `"],"mask_path":"art/none.png"}`, "mask could not read art/none.png"},
		{"a mask of another size", `{"prompt":"x","reference_paths":["` + source + `"],"mask_path":"` + wrong + `"}`, "the mask is 8×8 and the picture it masks (art/room.png) is 4×3"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, isError := runTool(t, agent, "generate_image", testCase.args)
			if !isError || !strings.Contains(result, testCase.want) {
				t.Fatalf("result %q (error %v), want a refusal saying %q", result, isError, testCase.want)
			}
		})
	}
	if len(painter.seen) != 0 {
		t.Fatalf("a refused mask still cost %d generations", len(painter.seen))
	}
}
