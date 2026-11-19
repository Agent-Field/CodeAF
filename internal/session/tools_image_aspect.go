package session

import (
	"bytes"
	"encoding/base64"
	"fmt"
	stdimage "image"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/provider"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // a provider may answer in webp; it must still be cropped
)

// A frame shape the drawing model does not take: generate_image's aspect_ratio.
//
// Every drawing model publishes the shapes it draws (the catalog's
// [catalog.MediaParams.AspectRatios]), and the lists differ: Gemini 3.1 Flash
// Image takes fourteen and not 8:5, FLUX.2 takes nine, Seedream takes eighteen.
// A shape outside a model's list is not a small matter for the model — the
// whole call is refused — and it is the ordinary case, not a rare one: a
// picture being edited has the shape it has, and asking for that shape back is
// exactly what an edit should do. A refusal there is a picture nobody can edit
// without first learning which model draws which shapes.
//
// SO THE ROAD ASKS FOR THE NEAREST SHAPE THE MODEL TAKES AND CROPS BACK TO THE
// ONE ASKED FOR. Nearest is measured on the logarithm of width over height, so
// a frame twice as wide and one twice as tall are equally far from square. The
// result is centre-cropped to the asked shape and, when the call edits a
// picture of that very shape, scaled to that picture's own size, so an edit of
// a 1600×1000 photo comes back 1600×1000. The result says what happened in the
// same line that names the file, because "drew at 3:2 and cropped" is a fact
// about the picture a caller may care about.
//
// WHAT THIS DOES NOT DO: it never touches a shape the model publishes (an
// equal shape spelt differently, 16:10 for 8:5, is sent in the model's own
// spelling), a word that is not W:H ("auto"), or any call to a model whose
// shapes nobody published — those are passed through untouched, as they always
// were, because a guess about a model's shapes is a field it may refuse.
//
// It is the only one of the two ways the engine could have handled this:
// preferring a default that takes any shape would fix only the default, and a
// person's own choice of model would still refuse the photo.

// aspectFit is the shape a call sends and the shape its result must have.
type aspectFit struct {
	// asked is the caller's spelling; sent is what the request carries.
	asked, sent string
	// ratio is the asked width over height, set only when the result must be
	// cropped back to it (the model was sent another shape).
	ratio float64
}

// mapped says the model was sent a different shape from the one asked.
func (f aspectFit) mapped() bool { return f.ratio > 0 }

// aspectTolerance is how close two shapes must be to count as one: 0.5%, which
// treats 16:10 and 8:5 as equal and 3:2 and 8:5 (6%) as different.
const aspectTolerance = 0.005

// parseAspect reads "W:H" with positive decimal sides ("2.35:1", "19.5:9").
func parseAspect(text string) (float64, bool) {
	width, height, found := strings.Cut(strings.TrimSpace(text), ":")
	if !found {
		return 0, false
	}
	w, errW := strconv.ParseFloat(strings.TrimSpace(width), 64)
	h, errH := strconv.ParseFloat(strings.TrimSpace(height), 64)
	if errW != nil || errH != nil || w <= 0 || h <= 0 || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return 0, false
	}
	return w / h, true
}

// fitAspect chooses the shape to send for an asked one, given the shapes the
// model publishes (nil when it published none).
func fitAspect(asked string, published []string) aspectFit {
	asked = strings.TrimSpace(asked)
	fit := aspectFit{asked: asked, sent: asked}
	want, ok := parseAspect(asked)
	if !ok || len(published) == 0 {
		return fit
	}
	best, bestDistance := "", math.Inf(1)
	for _, shape := range published {
		ratio, ok := parseAspect(shape)
		if !ok {
			continue
		}
		distance := math.Abs(math.Log(ratio / want))
		if distance <= aspectTolerance {
			// The model takes this shape; send it in the model's spelling.
			fit.sent = strings.TrimSpace(shape)
			return fit
		}
		if distance < bestDistance {
			best, bestDistance = strings.TrimSpace(shape), distance
		}
	}
	if best == "" {
		// The model published only words ("auto"); nothing to measure against.
		return fit
	}
	fit.sent, fit.ratio = best, want
	return fit
}

// fitBack crops one returned picture to the asked shape and, when target is a
// size of that shape, scales it to exactly that size. It answers the PNG it
// made, or false when the picture could not be decoded — in which case the
// caller keeps the provider's bytes untouched and says so.
func (f aspectFit) fitBack(data []byte, targetW, targetH int) ([]byte, bool) {
	picture, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	bounds := picture.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, false
	}
	crop := bounds
	if float64(w)/float64(h) > f.ratio {
		keep := int(math.Round(float64(h) * f.ratio))
		left := bounds.Min.X + (w-keep)/2
		crop = stdimage.Rect(left, bounds.Min.Y, left+keep, bounds.Max.Y)
	} else {
		keep := int(math.Round(float64(w) / f.ratio))
		top := bounds.Min.Y + (h-keep)/2
		crop = stdimage.Rect(bounds.Min.X, top, bounds.Max.X, top+keep)
	}
	outW, outH := crop.Dx(), crop.Dy()
	if targetW > 0 && targetH > 0 {
		outW, outH = targetW, targetH
	}
	out := stdimage.NewRGBA(stdimage.Rect(0, 0, outW, outH))
	xdraw.CatmullRom.Scale(out, out.Bounds(), picture, crop, xdraw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, out); err != nil {
		return nil, false
	}
	return encoded.Bytes(), true
}

// editTarget is the size an edit's result is scaled to: the first reference's
// own size, when that picture has the asked shape. Zero when there is no such
// picture, which leaves the cropped result at the size the model drew it.
func (f aspectFit) editTarget(workspace string, references []string) (int, int) {
	if !f.mapped() || len(references) == 0 {
		return 0, 0
	}
	w, h, ok := imageSizeAt(workspace, references[0])
	if !ok || math.Abs(math.Log(float64(w)/float64(h)/f.ratio)) > aspectTolerance {
		return 0, 0
	}
	return w, h
}

// fitItems crops every returned picture of a mapped call in place, and answers
// the line the result closes with. A picture that cannot be decoded keeps its
// bytes, and the line says the shape was not restored.
func (f aspectFit) fitItems(items []provider.GeneratedImage, model string, targetW, targetH int) string {
	if !f.mapped() {
		return ""
	}
	restored := true
	for i := range items {
		data, err := base64.StdEncoding.DecodeString(items[i].Base64)
		if err != nil || len(data) == 0 {
			continue
		}
		fitted, ok := f.fitBack(data, targetW, targetH)
		if !ok {
			restored = false
			continue
		}
		items[i].Base64 = base64.StdEncoding.EncodeToString(fitted)
		items[i].MediaType = "image/png"
	}
	if !restored {
		return fmt.Sprintf("%s does not draw %s, so it was asked for %s; the result could not be read to crop it back, so it is %s.", model, f.asked, f.sent, f.sent)
	}
	if targetW > 0 {
		return fmt.Sprintf("%s does not draw %s, so it drew at %s and the result was cropped to %s and scaled to %d×%d, the size of the picture it edits.", model, f.asked, f.sent, f.asked, targetW, targetH)
	}
	return fmt.Sprintf("%s does not draw %s, so it drew at %s and the result was cropped to %s.", model, f.asked, f.sent, f.asked)
}
