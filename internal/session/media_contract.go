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
