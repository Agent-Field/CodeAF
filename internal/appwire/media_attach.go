package appwire

import "strings"

// MediaAttacher is an optional source-to-attachment capability. The engine
// snapshots an authorized recorded image; a surface never supplies its path.
// The ordinary agents.send consumes the returned attachment ID as a picture,
// retaining its immutable source provenance. attachments.remove abandons it.
const FeatureMediaAttach = "media.attach"

type MediaAttacher interface {
	AttachMedia(agent, source, version string) (Attachment, error)
}
type MediaAttachResult struct {
	Attachment Attachment `json:"attachment"`
}

func init() {
	methods["media.attach"] = typed(func(e Engine, p struct {
		Agent   string `json:"agent"`
		Source  string `json:"source"`
		Version string `json:"version"`
	}) (*MediaAttachResult, error) {
		r, ok := e.(MediaAttacher)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.attach")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.Version) == "" || len(p.Source) > 128 || len(p.Version) > 256 {
			return nil, Errorf(CodeBadParams, "media.attach needs an agent, source, and recorded version")
		}
		attachment, err := r.AttachMedia(p.Agent, p.Source, p.Version)
		if err != nil {
			return nil, err
		}
		return &MediaAttachResult{Attachment: attachment}, nil
	})
}
