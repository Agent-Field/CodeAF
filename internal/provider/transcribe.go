package provider

// transcribe.go is the media client's fourth door: audio in, words out.
//
// The other three doors on this client MAKE things — a picture, a voice, a
// film. This one is the only one that PERCEIVES, and it exists because the
// belt above it needs a rung that turns a sound file into text without asking a
// chat model to think about it: transcription is a cheap, purpose-built
// endpoint, and a session that reaches for a general model first pays a
// multiple of the price for the same sentence.
//
// THE WIRE SHAPE IS internal/voice's, MIRRORED RATHER THAN IMPORTED.
// internal/voice/transcriber.go has been posting to /audio/transcriptions for
// the microphone since v1 and its request is the one this deployment answers:
// JSON, not multipart, with the bytes as a base64 `input_audio` object carrying
// a bare format word. That package is a surface's own client — its own key, its
// own timeout, its own Usage type — and importing it here would make the media
// client depend on a TUI's audio stack to reach an endpoint it already knows
// how to speak to. So the shape is copied and the source is cited, exactly as
// tools_pdf.go mirrors bare's truncation constants, and it travels through this
// client's own postJSON so it inherits the bearer key, the attribution headers,
// the response cap and the cost-header accounting every other media call has.

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// maxTranscriptionBytes is the input ceiling, pinned to internal/voice's
// MaxAudioBytes so the microphone and a file on disk are accepted or refused
// the same way. It is also the limit the endpoint itself publishes, so a larger
// payload buys nothing but a slower 413.
const maxTranscriptionBytes = 25 << 20

// TranscriptionRequest is one audio file on its way to /audio/transcriptions.
//
// Data is the bytes rather than a path because this package never opens a
// person's files — the caller that has the path also has the size limit it
// wants to enforce and the sentence it wants to say about it. MIME and Filename
// are two ways of answering the same question (which format word rides the
// wire) and either alone is enough; Language is the endpoint's optional hint
// and is omitted when empty.
type TranscriptionRequest struct {
	Model    string
	Data     []byte
	MIME     string
	Filename string
	Language string
}

// TranscriptionResponse is what came back: the words, and what they cost.
type TranscriptionResponse struct {
	Text  string
	Usage *ai.Usage
}

// transcriptionAudio is the endpoint's base64 envelope. Format is a BARE WORD
// ("mp3", not "audio/mpeg"), which is the one detail a media type cannot be
// pasted into.
type transcriptionAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type transcriptionWire struct {
	Model      string             `json:"model"`
	InputAudio transcriptionAudio `json:"input_audio"`
	Language   string             `json:"language,omitempty"`
}

// Transcribe posts one audio file and returns its transcript.
//
// Usage follows the rest of this client's law: the body's own usage object when
// the deployment sends one, the cost header when it does not, and nil for
// "unknown" rather than a zero that would read as free.
func (c *MediaClient) Transcribe(ctx context.Context, request TranscriptionRequest) (*TranscriptionResponse, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		return nil, fmt.Errorf("transcription model is required")
	}
	if len(request.Data) == 0 {
		return nil, fmt.Errorf("transcription audio is empty")
	}
	if len(request.Data) > maxTranscriptionBytes {
		return nil, fmt.Errorf("transcription audio is over the %dMB limit", maxTranscriptionBytes>>20)
	}
	format := transcriptionFormat(request.MIME, request.Filename)
	if format == "" {
		// Refused here rather than sent, for imageMediaType's reason: a format
		// word the endpoint does not know comes back as a 400 with nothing in it
		// that points at the file the caller passed.
		return nil, fmt.Errorf("transcription needs an audio format it knows: mp3, wav, m4a, ogg, flac or webm")
	}

	var decoded struct {
		Text  string    `json:"text"`
		Usage *ai.Usage `json:"usage,omitempty"`
	}
	headers, err := c.postJSON(ctx, "/audio/transcriptions", transcriptionWire{
		Model:      model,
		InputAudio: transcriptionAudio{Data: base64.StdEncoding.EncodeToString(request.Data), Format: format},
		Language:   strings.TrimSpace(request.Language),
	}, &decoded)
	if err != nil {
		return nil, err
	}
	if decoded.Usage == nil {
		decoded.Usage = usageFromHeaders(headers)
	}
	return &TranscriptionResponse{Text: strings.TrimSpace(decoded.Text), Usage: decoded.Usage}, nil
}

// transcriptionFormats is the media type → format word table, and it is the
// same six the endpoint publishes. audio/mp4 and audio/x-m4a are both spelled
// by real files carrying the same codec, which is why two rows land on "m4a".
var transcriptionFormats = map[string]string{
	"audio/mpeg":   "mp3",
	"audio/mp3":    "mp3",
	"audio/wav":    "wav",
	"audio/x-wav":  "wav",
	"audio/wave":   "wav",
	"audio/mp4":    "m4a",
	"audio/x-m4a":  "m4a",
	"audio/ogg":    "ogg",
	"audio/flac":   "flac",
	"audio/x-flac": "flac",
	"audio/webm":   "webm",
}

// transcriptionExtensions is the same answer read off a filename, for the
// caller that has a path and no media type.
var transcriptionExtensions = map[string]string{
	".mp3":  "mp3",
	".wav":  "wav",
	".m4a":  "m4a",
	".mp4":  "m4a",
	".ogg":  "ogg",
	".oga":  "ogg",
	".flac": "flac",
	".webm": "webm",
}

// transcriptionFormat answers the wire's format word from whichever of the two
// claims the caller made. The media type wins because it is the more specific
// of the two — a caller that sniffed the bytes put its answer there — and the
// extension is the fallback for a caller that only ever had a name.
func transcriptionFormat(mediaType, filename string) string {
	declared := strings.ToLower(strings.TrimSpace(mediaType))
	if semicolon := strings.IndexByte(declared, ';'); semicolon >= 0 {
		declared = strings.TrimSpace(declared[:semicolon])
	}
	if format := transcriptionFormats[declared]; format != "" {
		return format
	}
	return transcriptionExtensions[strings.ToLower(filepath.Ext(filename))]
}
