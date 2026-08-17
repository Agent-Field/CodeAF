// media_contract.go is the belt's slice of the provider media client — the
// interface [Config.Media] carries (docs/MULTIMODAL.md, the v3 revision).
//
// It is an interface for tools_image.go's reason: tests substitute a scripted
// generator, and the session package must not depend on which struct
// internal/provider hands over. provider.MediaClient satisfies it as it
// stands, so the wiring is one assignment with no adapter.
package session

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// MediaGenerator is every generation endpoint the belt reaches: still images
// (with references for image-to-image), speech, and the async video job. A
// nil MediaGenerator keeps every generation tool off the belt.
type MediaGenerator interface {
	GenerateImage(ctx context.Context, request provider.ImageRequest) (*provider.ImageResponse, error)
	Speak(ctx context.Context, request provider.SpeechRequest) (*provider.SpeechResponse, error)
	GenerateVideo(ctx context.Context, request provider.VideoRequest) (*provider.VideoResponse, error)
}

// The four words [Config.MediaModel] takes, and the ONLY four it takes. They
// are the same words the settings slots and internal/exec/media.go's leaf tools
// use, so one vocabulary serves the picker, the resolver, and the belt — a
// resolver asked for "images" or "tts" would answer "" and take a verb off the
// belt with nothing anywhere saying why.
const (
	modalityImage  = "image"
	modalitySpeech = "speech"
	modalityVideo  = "video"
	// modalityVision is the LOOKING slot, which no generation verb here reads —
	// view_image does (the sight lane owns it), and it is named here so the four
	// words live in one place.
	modalityVision = "vision"
)
