package appwire

import "strings"

// FeatureMediaAttachRange is an optional capability: the engine keeps an exact
// copy of a recorded audio or video as an owned FILE attachment whose source
// names the recording, its verified version and a span of it in milliseconds.
const FeatureMediaAttachRange = "media.attach_range"

// MediaRangeAttacher snapshots an authorized recorded audio or video. The span
// is provenance: the bytes are the whole recording, unclipped and unrendered, and
// no model is called. The returned attachment is an ordinary file the ordinary
// agents.send names in attachments (never in submission.media, which is for
// pictures); attachments.remove abandons it. No path is accepted.
type MediaRangeAttacher interface {
	AttachMediaRange(agent, source, version string, fromMS, toMS int64) (Attachment, error)
}

func init() {
	methods["media.attach_range"] = typed(func(e Engine, p struct {
		Agent   string `json:"agent"`
		Source  string `json:"source"`
		Version string `json:"version"`
		FromMS  int64  `json:"from_ms"`
		ToMS    int64  `json:"to_ms"`
	}) (*MediaAttachResult, error) {
		r, ok := e.(MediaRangeAttacher)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.attach_range")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.Version) == "" || len(p.Source) > 128 || len(p.Version) > 256 {
			return nil, Errorf(CodeBadParams, "media.attach_range needs an agent, source, recorded version and a range")
		}
		if err := CheckMediaRange(p.FromMS, p.ToMS); err != nil {
			return nil, err
		}
		attachment, err := r.AttachMediaRange(p.Agent, p.Source, p.Version, p.FromMS, p.ToMS)
		if err != nil {
			return nil, err
		}
		return &MediaAttachResult{Attachment: attachment}, nil
	})
}
