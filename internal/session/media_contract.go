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

	// GenerateMusic is the composing lane, and it is a METHOD OF ITS OWN rather
	// than Speak with a music model in it. The two are not one endpoint wearing
	// two hats: speech posts to /audio/speech, music has no media endpoint at
	// all on the router and is composed through streaming chat completions
	// asking for an audio modality back (internal/provider/music.go). A caller
	// that sent a composition brief to Speak would get 404s from a lane that
	// looks like it should work, which is what it did before this method
	// existed.
	GenerateMusic(ctx context.Context, request provider.MusicRequest) (*provider.MusicResponse, error)

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

// The five words [Config.MediaModel] takes, and the ONLY five it takes. They
// are the same words the settings slots and internal/exec/media.go's leaf tools
// use, so one vocabulary serves the picker, the resolver, and the belt — a
// resolver asked for "images" or "tts" would answer "" and take a verb off the
// belt with nothing anywhere saying why.
const (
	modalityImage  = "image"
	modalitySpeech = "speech"
	// modalityMusic is the COMPOSING slot, and it is a word of its own rather
	// than a second reading of speech: the settings sheet has carried a
	// "composing" row and an AFORGE_MUSIC_MODEL since long before anything on
	// this belt read it, and a person who picked Lyria there meant Lyria to make
	// the music and gpt-4o-mini-tts to keep making the voiceovers. The two share
	// an ENDPOINT (/audio/speech — internal/exec/media.go's generateMusic has
	// always ridden it) and nothing else.
	modalityMusic = "music"
	modalityVideo = "video"
	// modalityVision is the LOOKING slot, which no generation verb here reads —
	// view_image does (the sight lane owns it), and it is named here so the five
	// words live in one place.
	modalityVision = "vision"
)
