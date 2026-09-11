package provider

// music.go is the one media modality that IS NOT A MEDIA ENDPOINT.
//
// Every other generation lane on this client posts to an endpoint named after
// what it makes: /images, /audio/speech, /videos. Music has no such endpoint —
// /music, /audio/music, /audio/generations and /songs are all 404 on the router
// — and the models that compose (Lyria) are reached through ORDINARY CHAT
// COMPLETIONS asking for an audio modality back. So this file speaks
// /chat/completions and still belongs to MediaClient: what decides where a
// request lives here is which account, key and base URL carry it, and a caller
// asking for a piece of music should not have to know that the router spells
// this one as a conversation.
//
// THREE THINGS ARE NOT NEGOTIABLE ON THE WIRE, each learned by being refused:
//
//   - stream MUST be true. A non-streamed request is refused outright with
//     400 "Audio output requires stream: true".
//   - max_tokens MUST NOT BE SENT. A low cap made the provider answer 5xx
//     rather than truncate; omitting it succeeds. Nothing here sets one, and
//     nothing should be added.
//   - THE AUDIO IS NOT THE TEXT. It arrives base64 in delta.audio.data as one
//     very large SSE event (megabytes), while delta.content carries placeholder
//     markers that are not lyrics and not a transcript. A reader that
//     accumulated content would return prose and drop the song.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// maxMusicResponseBytes bounds the decoded clip. Music arrives whole rather
// than in a stream a caller consumes as it plays, so the ceiling is the same
// order as the other media lanes' and exists for the same reason: a runaway
// response must not be able to take the process with it.
const maxMusicResponseBytes = 128 << 20

// MusicRequest is a composition brief. There is deliberately NO DURATION and no
// format: the endpoint takes neither, a call returns whatever length the model
// decides to write, and a field that was accepted and ignored would be a knob
// every caller reasoned from and nothing honoured.
type MusicRequest struct {
	Model  string
	Prompt string
}

// MusicResponse is the finished clip. Format is what the provider said it sent,
// lowercased, and empty when it said nothing — the caller decides what an
// unnamed format is called on disk.
type MusicResponse struct {
	Audio  []byte
	Format string
	Usage  *ai.Usage
}

// musicWire is the request body, built here rather than by a caller so the
// three non-negotiables above cannot be forgotten at a call site.
type musicWire struct {
	Model      string          `json:"model"`
	Messages   []musicMessage  `json:"messages"`
	Modalities []string        `json:"modalities"`
	Stream     bool            `json:"stream"`
	Audio      *musicAudioSpec `json:"audio,omitempty"`
}

type musicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// musicAudioSpec is the OpenAI-compatible audio-output block. It is a pointer
// and left nil: the verified request carries no audio block at all, and this
// exists so the shape is written down where a later format argument would go
// rather than invented at that point.
type musicAudioSpec struct {
	Format string `json:"format,omitempty"`
}

// musicChunk is the slice of a streaming chat chunk this lane reads. It is its
// own type and not [streamChunk] because the field that matters — delta.audio —
// exists on no other lane, and widening the shared chunk would put an audio
// buffer on every token of every ordinary conversation.
type musicChunk struct {
	Choices []struct {
		Delta struct {
			Audio *struct {
				Data   string `json:"data"`
				Format string `json:"format"`
			} `json:"audio"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *ai.Usage       `json:"usage,omitempty"`
	Error json.RawMessage `json:"error,omitempty"`
}

// GenerateMusic composes one clip and returns it whole.
//
// It is one blocking call, like every other generation call on this client:
// the stream is an artifact-delivery mechanism the provider insists on, not
// something a caller watches, and nothing here polls. What differs is who
// waits. A composition takes most of a minute, so the session's tool
// (internal/session/tools_music.go) runs this in a job's goroutine and lands
// the result as a note, the way it does a video render — which is why ctx
// must be honoured all the way down: `jobs kill` is that context being
// cancelled, and a call that ignored it would compose on into a job nobody
// owns. The harness belt (internal/exec) still calls this and waits.
func (c *MediaClient) GenerateMusic(ctx context.Context, request MusicRequest) (*MusicResponse, error) {
	if strings.TrimSpace(request.Model) == "" {
		return nil, fmt.Errorf("music generation needs a model")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, fmt.Errorf("music generation needs a prompt")
	}
	body, err := json.Marshal(musicWire{
		Model:      request.Model,
		Messages:   []musicMessage{{Role: "user", Content: request.Prompt}},
		Modalities: []string{"audio"},
		Stream:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal music request: %w", err)
	}

	response, err := c.do(ctx, "/chat/completions", body, request.Model)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, maxMediaResponseBytes))
		return nil, apiError(response.StatusCode, payload)
	}
	return readMusicStream(response.Body)
}

// readMusicStream drains the event stream and returns the assembled clip.
//
// It reuses [sseDecoder] — the same framing every other stream on this client
// is read with, quirks included — so music cannot start disagreeing with chat
// about what a message boundary is. Only the SHAPE the payload is unmarshalled
// into differs, which is the whole reason [musicChunk] exists.
//
// The base64 is accumulated across events and decoded ONCE at the end. In
// practice the provider sends the clip as a single event; decoding per event
// would still be wrong, because base64 is only decodable on 4-character
// boundaries and a split that happened to land elsewhere would corrupt the file
// in a way nothing here could detect.
func readMusicStream(stream io.Reader) (*MusicResponse, error) {
	decoder := newSSEDecoder(stream)
	var (
		encoded bytes.Buffer
		format  string
		usage   *ai.Usage
	)
	for {
		payload, err := decoder.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read music stream: %w", err)
		}
		var chunk musicChunk
		if json.Unmarshal(payload, &chunk) != nil {
			continue
		}
		// An error delivered INSIDE the stream is the failure mode a 200 status
		// hides: the request was accepted, the composition was refused, and a
		// reader that only checked the HTTP code would return an empty clip
		// with no reason attached.
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return nil, fmt.Errorf("music generation failed: %s", videoErrorDetail(chunk.Error))
		}
		if chunk.Usage != nil {
			// FOLDED IN, NEVER SWAPPED IN, by the one rule the chat stream reads
			// its own frames with (client.go's [mergeUsage]): USAGE ACCUMULATES
			// ACROSS THE FRAMES OF ONE CALL, and a frame carrying only the price
			// does not zero the counts the frame before it carried.
			usage = mergeUsage(usage, chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			audio := choice.Delta.Audio
			if audio == nil {
				continue
			}
			if format == "" {
				format = strings.ToLower(strings.TrimSpace(audio.Format))
			}
			if encoded.Len()+len(audio.Data) > maxMusicResponseBytes {
				return nil, fmt.Errorf("music generation returned more than %d bytes", maxMusicResponseBytes)
			}
			encoded.WriteString(audio.Data)
		}
	}
	if encoded.Len() == 0 {
		// Said plainly, because this is what a model that CANNOT compose looks
		// like from here: it answers the chat request perfectly well, in words,
		// and never sends an audio delta at all.
		return nil, fmt.Errorf("the model returned no audio — it may not be a music model")
	}
	clip, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || len(clip) == 0 {
		return nil, fmt.Errorf("music generation returned audio that could not be decoded")
	}
	return &MusicResponse{Audio: clip, Format: format, Usage: usage}, nil
}
