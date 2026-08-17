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

// MediaGenerator is every media endpoint the belt reaches: still images (with
// references for image-to-image), speech, the async video job — and, since the
// senses landed, transcription. A nil MediaGenerator keeps every generation
// tool off the belt.
//
// THE NAME SAYS "GENERATOR" AND THE FOURTH METHOD PERCEIVES, which is worth one
// sentence. This is one client and not four, because it is one account, one key
// and one base URL, and the belt would learn nothing from a second interface
// that resolved to the same struct. The alternative — a MediaPerceiver beside
// it — buys a truer noun and costs every caller a second nil check for a client
// that is present or absent as a unit.
type MediaGenerator interface {
	GenerateImage(ctx context.Context, request provider.ImageRequest) (*provider.ImageResponse, error)
	Speak(ctx context.Context, request provider.SpeechRequest) (*provider.SpeechResponse, error)
	GenerateVideo(ctx context.Context, request provider.VideoRequest) (*provider.VideoResponse, error)

	// Transcribe is the senses wave's addition (tools_sense.go): audio in,
	// words out, through /audio/transcriptions. It is ADDITIVE — every existing
	// implementation of this interface is provider.MediaClient, which grew the
	// method in the same change (internal/provider/transcribe.go), so nothing
	// that satisfied this interface before stopped satisfying it. A test's
	// scripted generator must now answer it; embedding MediaGenerator in the
	// fake is the cheap way to keep that true through later additions.
	Transcribe(ctx context.Context, request provider.TranscriptionRequest) (*provider.TranscriptionResponse, error)
}
