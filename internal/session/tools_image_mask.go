package session

import (
	"fmt"
	stdimage "image"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A region edit: generate_image's mask_path.
//
// A mask is a picture the size of the FIRST reference, white where the picture
// may change and black where it must stay. It means one thing to the person who
// painted it, and the road has two ways to deliver that meaning, chosen per
// model at the moment of the call:
//
//   - A model whose catalog row publishes a `mask` knob takes the mask as a real
//     mask ([provider.ImageRequest.Mask]) and edits the open area alone.
//   - Every other model is sent the mask as ONE MORE REFERENCE, last, with words
//     appended to the instruction saying what that picture is. That is a guide
//     and not a guarantee: the model reads it as it reads any picture, and the
//     edit can spill past the white area.
//
// THE RESULT SAYS WHICH ONE HAPPENED. A caller that asked for a masked edit and
// got a guided one must be told so in the same line that names the file,
// because "only the sky changed" is a claim about the picture, and the guided
// road cannot make it. Sending a `mask` field to a model that does not publish
// one is not a third option: the endpoint either refuses the whole call or
// drops the field without a word, and the second is the lie this file exists
// to prevent.

// maskGuideWords is appended to the instruction when the mask rides as a
// reference. It names the picture by position because position is the only
// thing every image model reads the same way.
const maskGuideWords = "\n\nThe last reference picture is a mask for the first one, not a picture to draw from: change only the area that is white in the mask, and keep everything under its black area exactly as it is in the first picture."

// maskGuidedNote and maskSentNote close the result, one per road.
const (
	maskGuidedNote = "This model takes no real mask, so the mask was sent as a guide picture beside the source: the change may reach past the white area. Look at the result before you build on it."
	maskSentNote   = "The mask was sent as a real mask: only its white area was open to change."
)

// imageMask is a mask read and checked, before anything is paid for.
type imageMask struct {
	reference provider.ImageReference
}

// readImageMask reads mask_path the way a reference is read and checks the two
// things a mask must be: beside a picture to mask, and that picture's size. The
// second string is a refusal in the model's own terms, empty on success; a nil
// mask with no refusal means no mask was asked for.
//
// THE SIZE IS CHECKED BEFORE THE REQUEST, because a mask the size of some other
// picture lines its white area up with the wrong part of this one, and the
// provider charges for that edit either way. A format the standard library
// cannot measure (webp) is not refused on that account: the check needs both
// sizes, and an unknown one is not a mismatch.
func readImageMask(workspace, maskPath string, references []string) (*imageMask, string) {
	if strings.TrimSpace(maskPath) == "" {
		return nil, ""
	}
	if len(references) == 0 {
		return nil, "mask_path needs the picture it masks, given as the first of reference_paths"
	}
	dataURL, refusal := mediaReferenceFrom(workspace, maskPath)
	if refusal != "" {
		return nil, "mask " + refusal
	}
	if mw, mh, ok := imageSizeAt(workspace, maskPath); ok {
		if sw, sh, ok := imageSizeAt(workspace, references[0]); ok && (mw != sw || mh != sh) {
			return nil, fmt.Sprintf("the mask is %d×%d and the picture it masks (%s) is %d×%d; a mask must be the same size as the first of reference_paths",
				mw, mh, filepath.ToSlash(strings.TrimSpace(references[0])), sw, sh)
		}
	}
	return &imageMask{reference: provider.NewImageReference(dataURL)}, ""
}

// imageSizeAt reads a picture's dimensions from its header alone. Any failure is
// "unknown", never an error: the reference reader has already said what is
// wrong with a file that cannot be used at all.
func imageSizeAt(workspace, path string) (int, int, bool) {
	file, err := os.Open(resolveInWorkspace(path, workspace))
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	config, _, err := stdimage.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// apply puts the mask on the request by the road the model can take, and says
// which: true when it rode as a real mask.
func (m *imageMask) apply(request *provider.ImageRequest, takesMask bool) bool {
	if takesMask {
		reference := m.reference
		request.Mask = &reference
		return true
	}
	request.InputReferences = append(request.InputReferences, m.reference)
	request.Prompt += maskGuideWords
	return false
}

// note is the line the result closes with.
func (m *imageMask) note(real bool) string {
	if real {
		return maskSentNote
	}
	return maskGuidedNote
}
