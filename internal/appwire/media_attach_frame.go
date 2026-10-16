package appwire

import (
	"encoding/base64"
	"strings"
)

// FeatureMediaAttachFrame is an optional capability: the engine keeps one
// captured frame of an exact recorded video as an owned image attachment.
const FeatureMediaAttachFrame = "media.attach_frame"

// MediaFrameMaxBytes bounds the PNG a caller may supply. A request line over 4 MiB
// is refused by the app transport and base64 adds a third, so the frame stays
// well inside one request. internal/session pins this equal to its own limit.
const MediaFrameMaxBytes = 3_000_000

// MediaFrameAttacher turns caller-supplied PNG bytes plus an authorized video
// source into an engine-owned attachment, which the ordinary agents.send consumes
// and attachments.remove abandons. No path is accepted: a surface can supply a
// picture but can never make the engine read a file.
type MediaFrameAttacher interface {
	AttachMediaFrame(agent, source, version string, atMS int64, png []byte) (Attachment, error)
}

func init() {
	methods["media.attach_frame"] = typed(func(e Engine, p struct {
		Agent   string `json:"agent"`
		Source  string `json:"source"`
		Version string `json:"version"`
		AtMS    int64  `json:"at_ms"`
		PNG     string `json:"png"`
	}) (*MediaAttachResult, error) {
		r, ok := e.(MediaFrameAttacher)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.attach_frame")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.Version) == "" || len(p.Source) > 128 || len(p.Version) > 256 || p.AtMS < 0 {
			return nil, Errorf(CodeBadParams, "media.attach_frame needs an agent, source, recorded version and a moment")
		}
		// The encoded size is checked before a byte is decoded.
		if p.PNG == "" || len(p.PNG) > base64.StdEncoding.EncodedLen(MediaFrameMaxBytes) {
			return nil, Errorf(CodeBadParams, "a frame is 1 to %d bytes", MediaFrameMaxBytes)
		}
		png, err := base64.StdEncoding.Strict().DecodeString(p.PNG)
		if err != nil {
			return nil, Errorf(CodeBadParams, "the frame is not base64")
		}
		attachment, err := r.AttachMediaFrame(p.Agent, p.Source, p.Version, p.AtMS, png)
		if err != nil {
			return nil, err
		}
		return &MediaAttachResult{Attachment: attachment}, nil
	})
}
